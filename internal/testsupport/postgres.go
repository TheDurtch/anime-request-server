// Package testsupport provides throwaway PostgreSQL instances for tests, backed
// by testcontainers-go. It is imported only from _test.go files, so it is never
// compiled into the server binary. A running Docker/Podman daemon is required;
// callers should skip their tests when StartPostgres returns an error.
package testsupport

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/TheDurtch/anime-request-server/internal/database"
)

// DB is a disposable PostgreSQL instance and its connection pool.
type DB struct {
	Pool      *pgxpool.Pool
	container *postgres.PostgresContainer
}

// StartPostgres boots a disposable PostgreSQL container, applies all migrations,
// and returns a ready connection pool. It requires a running Docker/Podman
// daemon; on any failure the caller should skip rather than fail, so the suite
// stays green on machines without a container runtime.
func StartPostgres(ctx context.Context) (*DB, error) {
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("anime_requests_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			// The official image logs the "ready" line twice during init.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("starting postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("connection string: %w", err)
	}

	if err := database.RunMigrations(dsn); err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	pool, err := database.Connect(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("connecting pool: %w", err)
	}

	return &DB{Pool: pool, container: container}, nil
}

// Close closes the pool and terminates the container.
func (d *DB) Close(ctx context.Context) error {
	d.Pool.Close()
	return d.container.Terminate(ctx)
}

// TruncateAll clears every application table. Call it between tests so each one
// starts from a clean slate.
func (d *DB) TruncateAll(ctx context.Context) error {
	_, err := d.Pool.Exec(ctx, `
		TRUNCATE request_server_destinations, anime_requests, invite_codes,
		         server_destinations, sessions, users RESTART IDENTITY CASCADE
	`)
	return err
}
