package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// Create inserts a new anime request.
func (r *RequestRepo) Create(ctx context.Context, name string, category models.Category, requestedBy uuid.UUID) (*models.AnimeRequest, error) {
	id := uuid.New()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO anime_requests (id, name, category, requested_by)
		VALUES ($1, $2, $3, $4)
	`, id, string(category), requestedBy)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	return r.GetByID(ctx, id)
}

// CreateBatch inserts multiple anime requests at once.
func (r *RequestRepo) CreateBatch(ctx context.Context, names []string, requestedBy uuid.UUID) (int, error) {
	batch := &pgx.Batch{}
	for _, name := range names {
		batch.Queue(`
			INSERT INTO anime_requests (id, name, category, requested_by)
			VALUES ($1, $2, 'batch_add', $3)
		`, uuid.New(), name, requestedBy)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	count := 0
	for range names {
		if _, err := br.Exec(); err != nil {
			return count, fmt.Errorf("batch insert item %d: %w", count, err)
		}
		count++
	}
	return count, nil
}

// GetByID fetches a request by ID with joined data.
func (r *RequestRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.AnimeRequest, error) {
	req := &models.AnimeRequest{}
	err := r.pool.QueryRow(ctx, `
		SELECT ar.id, ar.name, ar.category, ar.status, ar.requested_by, u.username,
		       ar.server_destination_id, sd.name, ar.anidb_url, ar.created_at, ar.updated_at
		FROM anime_requests ar
		JOIN users u ON u.id = ar.requested_by
		LEFT JOIN server_destinations sd ON sd.id = ar.server_destination_id
		WHERE ar.id = $1
	`, id).Scan(&req.ID, &req.Name, &req.Category, &req.Status, &req.RequestedBy,
		&req.RequestedByUsername, &req.ServerDestinationID, &req.ServerDestinationName,
		&req.AnidbURL, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting request: %w", err)
	}
	return req, nil
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
		       ar.server_destination_id, sd.name, ar.anidb_url, ar.created_at, ar.updated_at
		FROM anime_requests ar
		JOIN users u ON u.id = ar.requested_by
		LEFT JOIN server_destinations sd ON sd.id = ar.server_destination_id
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
			&req.RequestedByUsername, &req.ServerDestinationID, &req.ServerDestinationName,
			&req.AnidbURL, &req.CreatedAt, &req.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	return requests, total, nil
}

// Update modifies a request (admin/mod only fields).
func (r *RequestRepo) Update(ctx context.Context, id uuid.UUID, status *models.Status, category *models.Category, serverDestID *uuid.UUID, anidbURL *string) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

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
	if serverDestID != nil {
		sets = append(sets, fmt.Sprintf("server_destination_id = $%d", argIdx))
		args = append(args, *serverDestID)
		argIdx++
	}
	if anidbURL != nil {
		sets = append(sets, fmt.Sprintf("anidb_url = $%d", argIdx))
		args = append(args, *anidbURL)
		argIdx++
	}

	if len(args) == 0 {
		return nil
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE anime_requests SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating request: %w", err)
	}
	return nil
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
