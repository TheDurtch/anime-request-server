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

// UserRepo handles user database operations.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// Create inserts a new user.
func (r *UserRepo) Create(ctx context.Context, username, passwordHash string, role models.Role) (*models.User, error) {
	user := &models.User{
		ID:       uuid.New(),
		Username: username,
		Role:     role,
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, role)
		VALUES ($1, $2, $3, $4)
	`, user.ID, username, passwordHash, string(role))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, fmt.Errorf("username already exists")
		}
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return r.GetByID(ctx, user.ID)
}

// GetByID fetches a user by ID.
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	u := &models.User{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, totp_secret, totp_enabled,
		       role, can_batch_add, disabled, created_at, updated_at
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.TOTPSecret,
		&u.TOTPEnabled, &u.Role, &u.CanBatchAdd, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting user by ID: %w", err)
	}
	return u, nil
}

// GetByUsername fetches a user by username.
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	u := &models.User{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, totp_secret, totp_enabled,
		       role, can_batch_add, disabled, created_at, updated_at
		FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.TOTPSecret,
		&u.TOTPEnabled, &u.Role, &u.CanBatchAdd, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting user by username: %w", err)
	}
	return u, nil
}

// List returns all users.
func (r *UserRepo) List(ctx context.Context) ([]models.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, username, email, totp_enabled, role, can_batch_add, disabled, created_at, updated_at
		FROM users ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.TOTPEnabled,
			&u.Role, &u.CanBatchAdd, &u.Disabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

// Update modifies user fields. Only non-nil fields are updated.
func (r *UserRepo) Update(ctx context.Context, id uuid.UUID, role *models.Role, canBatchAdd *bool, disabled *bool) error {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if role != nil {
		sets = append(sets, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, string(*role))
		argIdx++
	}
	if canBatchAdd != nil {
		sets = append(sets, fmt.Sprintf("can_batch_add = $%d", argIdx))
		args = append(args, *canBatchAdd)
		argIdx++
	}
	if disabled != nil {
		sets = append(sets, fmt.Sprintf("disabled = $%d", argIdx))
		args = append(args, *disabled)
		argIdx++
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	return nil
}

// UpdateTOTP sets or clears a user's TOTP secret.
func (r *UserRepo) UpdateTOTP(ctx context.Context, id uuid.UUID, secret *string, enabled bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET totp_secret = $1, totp_enabled = $2, updated_at = NOW() WHERE id = $3
	`, secret, enabled, id)
	if err != nil {
		return fmt.Errorf("updating TOTP: %w", err)
	}
	return nil
}

// SessionRepo handles session database operations.
type SessionRepo struct {
	pool *pgxpool.Pool
}

// NewSessionRepo creates a new SessionRepo.
func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// Create inserts a new session.
func (r *SessionRepo) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*models.Session, error) {
	s := &models.Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, s.ID, s.UserID, s.TokenHash, s.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	return s, nil
}

// GetByTokenHash retrieves a session by its token hash, including user data.
func (r *SessionRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*models.Session, *models.User, error) {
	s := &models.Session{}
	u := &models.User{}
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.token_hash, s.expires_at, s.created_at,
		       u.id, u.username, u.email, u.totp_enabled, u.role, u.can_batch_add, u.disabled, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > NOW()
	`, tokenHash).Scan(
		&s.ID, &s.UserID, &s.TokenHash, &s.ExpiresAt, &s.CreatedAt,
		&u.ID, &u.Username, &u.Email, &u.TOTPEnabled, &u.Role, &u.CanBatchAdd, &u.Disabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("getting session: %w", err)
	}
	return s, u, nil
}

// Delete removes a session.
func (r *SessionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// DeleteExpired removes all expired sessions.
func (r *SessionRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("deleting expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
