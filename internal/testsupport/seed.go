package testsupport

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

// SeedUser inserts a user with the given role and a usable bcrypt password hash.
func SeedUser(ctx context.Context, pool *pgxpool.Pool, username string, role models.Role) (*models.User, error) {
	hash, err := auth.HashPassword("test-password")
	if err != nil {
		return nil, err
	}
	return repository.NewUserRepo(pool).Create(ctx, username, hash, role)
}

// SeedSession creates a one-hour session for userID and returns the plaintext
// bearer token a client would send (the DB stores only its SHA-256 hash).
func SeedSession(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (string, error) {
	token, hash, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	if _, err := repository.NewSessionRepo(pool).Create(ctx, userID, hash, time.Now().Add(time.Hour)); err != nil {
		return "", err
	}
	return token, nil
}

// SeedDestination inserts a server destination created by createdBy.
func SeedDestination(ctx context.Context, pool *pgxpool.Pool, name string, createdBy uuid.UUID) (*models.ServerDestination, error) {
	return repository.NewServerDestRepo(pool).Create(ctx, name, createdBy)
}
