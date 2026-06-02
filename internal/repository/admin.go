package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TheDurtch/anime-request-server/internal/models"
)

// InviteCodeRepo handles invite code database operations.
type InviteCodeRepo struct {
	pool *pgxpool.Pool
}

// NewInviteCodeRepo creates a new InviteCodeRepo.
func NewInviteCodeRepo(pool *pgxpool.Pool) *InviteCodeRepo {
	return &InviteCodeRepo{pool: pool}
}

// Create inserts a new invite code.
func (r *InviteCodeRepo) Create(ctx context.Context, code string, createdBy uuid.UUID, expiresAt *time.Time) (*models.InviteCode, error) {
	ic := &models.InviteCode{
		ID:        uuid.New(),
		Code:      code,
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO invite_codes (id, code, created_by, expires_at)
		VALUES ($1, $2, $3, $4)
	`, ic.ID, ic.Code, ic.CreatedBy, ic.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("creating invite code: %w", err)
	}
	return ic, nil
}

// GetByCode retrieves an invite code by its code string.
func (r *InviteCodeRepo) GetByCode(ctx context.Context, code string) (*models.InviteCode, error) {
	ic := &models.InviteCode{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, created_by, used_by, expires_at, created_at
		FROM invite_codes WHERE code = $1
	`, code).Scan(&ic.ID, &ic.Code, &ic.CreatedBy, &ic.UsedBy, &ic.ExpiresAt, &ic.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting invite code: %w", err)
	}
	return ic, nil
}

// MarkUsed marks an invite code as used by a user.
func (r *InviteCodeRepo) MarkUsed(ctx context.Context, codeID, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE invite_codes SET used_by = $1 WHERE id = $2`, userID, codeID)
	if err != nil {
		return fmt.Errorf("marking invite code used: %w", err)
	}
	return nil
}

// List returns all invite codes.
func (r *InviteCodeRepo) List(ctx context.Context) ([]models.InviteCode, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, created_by, used_by, expires_at, created_at
		FROM invite_codes ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing invite codes: %w", err)
	}
	defer rows.Close()

	var codes []models.InviteCode
	for rows.Next() {
		var ic models.InviteCode
		if err := rows.Scan(&ic.ID, &ic.Code, &ic.CreatedBy, &ic.UsedBy, &ic.ExpiresAt, &ic.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning invite code: %w", err)
		}
		codes = append(codes, ic)
	}
	return codes, nil
}

// ServerDestRepo handles server destination database operations.
type ServerDestRepo struct {
	pool *pgxpool.Pool
}

// NewServerDestRepo creates a new ServerDestRepo.
func NewServerDestRepo(pool *pgxpool.Pool) *ServerDestRepo {
	return &ServerDestRepo{pool: pool}
}

// Create inserts a new server destination.
func (r *ServerDestRepo) Create(ctx context.Context, name string, createdBy uuid.UUID) (*models.ServerDestination, error) {
	sd := &models.ServerDestination{
		ID:        uuid.New(),
		Name:      name,
		CreatedBy: createdBy,
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO server_destinations (id, name, created_by) VALUES ($1, $2, $3)
	`, sd.ID, sd.Name, sd.CreatedBy)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, fmt.Errorf("server destination name already exists")
		}
		return nil, fmt.Errorf("creating server destination: %w", err)
	}
	return sd, nil
}

// List returns all server destinations.
func (r *ServerDestRepo) List(ctx context.Context) ([]models.ServerDestination, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, created_by, created_at FROM server_destinations ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing server destinations: %w", err)
	}
	defer rows.Close()

	var dests []models.ServerDestination
	for rows.Next() {
		var sd models.ServerDestination
		if err := rows.Scan(&sd.ID, &sd.Name, &sd.CreatedBy, &sd.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning server destination: %w", err)
		}
		dests = append(dests, sd)
	}
	return dests, nil
}

// Delete removes a server destination by ID.
func (r *ServerDestRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM server_destinations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting server destination: %w", err)
	}
	return nil
}
