package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TheDurtch/anime-request-server/internal/models"
)

// RequestRepo handles anime request database operations.
type RequestRepo struct {
	pool *pgxpool.Pool
}

// NewRequestRepo creates a new RequestRepo.
func NewRequestRepo(pool *pgxpool.Pool) *RequestRepo {
	return &RequestRepo{pool: pool}
}

// RequestFilter holds optional filters for listing requests.
type RequestFilter struct {
	Category    *string
	Status      *string
	RequestedBy *uuid.UUID
	Page        int
	PerPage     int
}

// Create inserts a new anime request with default status and no destinations.
func (r *RequestRepo) Create(ctx context.Context, name string, category models.Category, requestedBy uuid.UUID) (*models.AnimeRequest, error) {
	return r.CreateWithDetails(ctx, name, category, requestedBy, nil, nil, nil)
}

// CreateWithDetails inserts a new anime request and, in a single transaction,
// applies the optional mod/admin fields: a non-nil status overrides the DB
// default ('new'), a non-nil anidbURL is stored, and each ID in destIDs is
// linked as a server destination. Callers must validate status/anidbURL before
// passing them; values are bound, not interpolated.
func (r *RequestRepo) CreateWithDetails(ctx context.Context, name string, category models.Category, requestedBy uuid.UUID, status *models.Status, anidbURL *string, destIDs []uuid.UUID) (*models.AnimeRequest, error) {
	id := uuid.New()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Build the insert with only the columns we're setting; status/anidb_url
	// fall back to their schema defaults when omitted.
	cols := []string{"id", "name", "category", "requested_by"}
	placeholders := []string{"$1", "$2", "$3", "$4"}
	args := []any{id, name, string(category), requestedBy}
	next := 5
	if status != nil {
		cols = append(cols, "status")
		placeholders = append(placeholders, fmt.Sprintf("$%d", next))
		args = append(args, string(*status))
		next++
	}
	if anidbURL != nil {
		cols = append(cols, "anidb_url")
		placeholders = append(placeholders, fmt.Sprintf("$%d", next))
		args = append(args, *anidbURL)
		next++
	}

	query := fmt.Sprintf("INSERT INTO anime_requests (%s) VALUES (%s)",
		strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		// Unique index on LOWER(name) (migration 007) — surfaces a race that
		// slipped past the app-level duplicate check. Match the specific
		// unique-violation (23505) on that index rather than the error string.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_anime_requests_name_lower" {
			return nil, fmt.Errorf("a request with this name already exists")
		}
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for _, destID := range destIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO request_server_destinations (request_id, server_destination_id)
			VALUES ($1, $2)
			ON CONFLICT (request_id, server_destination_id) DO NOTHING
		`, id, destID); err != nil {
			return nil, fmt.Errorf("linking destination: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}

	return r.GetByID(ctx, id)
}

// CreateBatch inserts multiple anime requests at once.
func (r *RequestRepo) CreateBatch(ctx context.Context, names []string, requestedBy uuid.UUID) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, name := range names {
		// Skip names that collide (case-insensitively) with an existing row or
		// an earlier item in this batch, rather than failing the whole batch.
		batch.Queue(`INSERT INTO anime_requests (id, name, category, requested_by) VALUES ($1, $2, 'batch_add', $3) ON CONFLICT (LOWER(name)) DO NOTHING`, uuid.New(), name, requestedBy)
	}

	br := tx.SendBatch(ctx, batch)

	count := 0
	var execErr error
	for range names {
		tag, err := br.Exec()
		if err != nil {
			execErr = fmt.Errorf("batch insert: %w", err)
			break
		}
		// RowsAffected is 0 for a skipped conflict, 1 for an actual insert.
		count += int(tag.RowsAffected())
	}

	// Close the batch exactly once, before committing — the batch holds the
	// connection until closed. The deferred tx.Rollback covers the error paths.
	if cerr := br.Close(); cerr != nil && execErr == nil {
		execErr = fmt.Errorf("closing batch: %w", cerr)
	}
	if execErr != nil {
		return count, execErr
	}

	if err := tx.Commit(ctx); err != nil {
		return count, fmt.Errorf("commit batch insert: %w", err)
	}
	return count, nil
}

// GetByID fetches a request by ID with joined data.
func (r *RequestRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.AnimeRequest, error) {
	req := &models.AnimeRequest{}
	err := r.pool.QueryRow(ctx, `
		SELECT ar.id, ar.name, ar.category, ar.status, ar.requested_by, u.username,
		       ar.anidb_url, ar.created_at, ar.updated_at
		FROM anime_requests ar
		JOIN users u ON u.id = ar.requested_by
		WHERE ar.id = $1
	`, id).Scan(&req.ID, &req.Name, &req.Category, &req.Status, &req.RequestedBy,
		&req.RequestedByUsername, &req.AnidbURL, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting request: %w", err)
	}

	// Load server destinations
	if err := r.loadDestinations(ctx, req); err != nil {
		return nil, err
	}

	return req, nil
}

// loadDestinations loads server destinations for a single request.
func (r *RequestRepo) loadDestinations(ctx context.Context, req *models.AnimeRequest) error {
	rows, err := r.pool.Query(ctx, `
		SELECT sd.id, sd.name, rsd.added_at
		FROM request_server_destinations rsd
		JOIN server_destinations sd ON sd.id = rsd.server_destination_id
		WHERE rsd.request_id = $1
		ORDER BY rsd.added_at ASC
	`, req.ID)
	if err != nil {
		return fmt.Errorf("loading destinations: %w", err)
	}
	defer rows.Close()

	req.ServerDestinations = []models.ServerDestinationMapping{}
	for rows.Next() {
		var dest models.ServerDestinationMapping
		if err := rows.Scan(&dest.ID, &dest.Name, &dest.AddedAt); err != nil {
			return fmt.Errorf("scanning destination: %w", err)
		}
		req.ServerDestinations = append(req.ServerDestinations, dest)
	}
	return rows.Err()
}

// loadDestinationsForMultiple loads destinations for multiple requests efficiently.
func (r *RequestRepo) loadDestinationsForMultiple(ctx context.Context, requests []models.AnimeRequest) error {
	if len(requests) == 0 {
		return nil
	}

	// Build list of request IDs
	requestIDs := make([]uuid.UUID, len(requests))
	requestMap := make(map[uuid.UUID]*models.AnimeRequest)
	for i := range requests {
		requestIDs[i] = requests[i].ID
		requestMap[requests[i].ID] = &requests[i]
		requests[i].ServerDestinations = []models.ServerDestinationMapping{}
	}

	// Fetch all destinations for these requests in one query
	rows, err := r.pool.Query(ctx, `
		SELECT rsd.request_id, sd.id, sd.name, rsd.added_at
		FROM request_server_destinations rsd
		JOIN server_destinations sd ON sd.id = rsd.server_destination_id
		WHERE rsd.request_id = ANY($1)
		ORDER BY rsd.request_id, rsd.added_at ASC
	`, requestIDs)
	if err != nil {
		return fmt.Errorf("loading destinations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var requestID uuid.UUID
		var dest models.ServerDestinationMapping
		if err := rows.Scan(&requestID, &dest.ID, &dest.Name, &dest.AddedAt); err != nil {
			return fmt.Errorf("scanning destination: %w", err)
		}
		if req, ok := requestMap[requestID]; ok {
			req.ServerDestinations = append(req.ServerDestinations, dest)
		}
	}
	return rows.Err()
}

// List returns filtered and paginated anime requests.
func (r *RequestRepo) List(ctx context.Context, filter RequestFilter) ([]models.AnimeRequest, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 || filter.PerPage > 100 {
		filter.PerPage = 25
	}

	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.Category != nil {
		where = append(where, fmt.Sprintf("ar.category = $%d", argIdx))
		args = append(args, *filter.Category)
		argIdx++
	}
	if filter.Status != nil {
		where = append(where, fmt.Sprintf("ar.status = $%d", argIdx))
		args = append(args, *filter.Status)
		argIdx++
	}
	if filter.RequestedBy != nil {
		where = append(where, fmt.Sprintf("ar.requested_by = $%d", argIdx))
		args = append(args, *filter.RequestedBy)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM anime_requests ar WHERE %s", whereClause)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting requests: %w", err)
	}

	// Fetch page
	offset := (filter.Page - 1) * filter.PerPage
	args = append(args, filter.PerPage, offset)
	query := fmt.Sprintf(`
		SELECT ar.id, ar.name, ar.category, ar.status, ar.requested_by, u.username,
		       ar.anidb_url, ar.created_at, ar.updated_at
		FROM anime_requests ar
		JOIN users u ON u.id = ar.requested_by
		WHERE %s
		ORDER BY ar.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var requests []models.AnimeRequest
	for rows.Next() {
		var req models.AnimeRequest
		if err := rows.Scan(&req.ID, &req.Name, &req.Category, &req.Status, &req.RequestedBy,
			&req.RequestedByUsername,
			&req.AnidbURL, &req.CreatedAt, &req.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	// Load destinations for all requests
	if err := r.loadDestinationsForMultiple(ctx, requests); err != nil {
		return nil, 0, err
	}

	return requests, total, nil
}

// Update modifies a request (admin/mod only fields). A non-nil name renames the
// entry; renaming to a name another request already uses (case-insensitively)
// returns an "already exists" error via the LOWER(name) unique index.
// Note: serverDestIDs are now managed via AddDestination/RemoveDestination methods.
func (r *RequestRepo) Update(ctx context.Context, id uuid.UUID, name *string, status *models.Status, category *models.Category, anidbURL *string) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}
	if status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*status))
		argIdx++
	}
	if category != nil {
		sets = append(sets, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, string(*category))
		argIdx++
	}
	if anidbURL != nil {
		// Don't allow clearing once set - validate before calling
		sets = append(sets, fmt.Sprintf("anidb_url = $%d", argIdx))
		args = append(args, *anidbURL)
		argIdx++
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE anime_requests SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		// A rename can collide with the LOWER(name) unique index (migration 007).
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_anime_requests_name_lower" {
			return fmt.Errorf("a request with this name already exists")
		}
		return err
	}
	return nil
}

// Delete removes a request. The request_server_destinations rows are removed
// automatically via ON DELETE CASCADE (migration 006). Returns true if a row
// was deleted, false if no request had the given ID.
func (r *RequestRepo) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM anime_requests WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("deleting request: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// AddDestination adds a server destination to a request.
func (r *RequestRepo) AddDestination(ctx context.Context, requestID, destinationID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO request_server_destinations (request_id, server_destination_id)
		VALUES ($1, $2)
		ON CONFLICT (request_id, server_destination_id) DO NOTHING
	`, requestID, destinationID)
	return err
}

// RemoveDestination removes a server destination from a request.
func (r *RequestRepo) RemoveDestination(ctx context.Context, requestID, destinationID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM request_server_destinations
		WHERE request_id = $1 AND server_destination_id = $2
	`, requestID, destinationID)
	return err
}

// GetDestinationCount returns the number of destinations for a request.
func (r *RequestRepo) GetDestinationCount(ctx context.Context, requestID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM request_server_destinations WHERE request_id = $1
	`, requestID).Scan(&count)
	return count, err
}

// CheckDuplicate checks if a request with the same name already exists (case-insensitive).
func (r *RequestRepo) CheckDuplicate(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM anime_requests WHERE LOWER(name) = LOWER($1))
	`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking duplicate: %w", err)
	}
	return exists, nil
}
