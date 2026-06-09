package repository_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
	"github.com/TheDurtch/anime-request-server/internal/testsupport"
)

var testDB *testsupport.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	db, err := testsupport.StartPostgres(ctx)
	if err != nil {
		// No container runtime (or image pull failed): skip the package rather
		// than fail, so `go test ./...` stays green without Docker/Podman.
		fmt.Fprintf(os.Stderr, "skipping repository DB tests: %v\n", err)
		os.Exit(0)
	}
	testDB = db
	code := m.Run()
	_ = db.Close(ctx)
	os.Exit(code)
}

func TestRequestRepo_Update_Rename(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	repo := repository.NewRequestRepo(testDB.Pool)
	u := mustUser(t, "alice", models.RoleUser)

	req, err := repo.Create(ctx, "Old Name", models.CategoryCurrentFuture, u.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newName := "New Name"
	if err := repo.Update(ctx, req.ID, &newName, nil, nil, nil); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got, err := repo.GetByID(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != newName {
		t.Fatalf("name = %q, want %q", got.Name, newName)
	}
}

func TestRequestRepo_Update_RenameConflict(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	repo := repository.NewRequestRepo(testDB.Pool)
	u := mustUser(t, "alice", models.RoleUser)

	a, err := repo.Create(ctx, "Naruto", models.CategoryCurrentFuture, u.ID)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := repo.Create(ctx, "Bleach", models.CategoryCurrentFuture, u.ID); err != nil {
		t.Fatalf("create b: %v", err)
	}

	// Renaming "Naruto" -> "bleach" must collide on the LOWER(name) unique index.
	clash := "bleach"
	err = repo.Update(ctx, a.ID, &clash, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("rename conflict: got %v, want an 'already exists' error", err)
	}

	// Renaming a row to its own current name is a no-op, not a conflict.
	same := "Naruto"
	if err := repo.Update(ctx, a.ID, &same, nil, nil, nil); err != nil {
		t.Fatalf("rename-to-self: %v", err)
	}
}

func TestRequestRepo_Delete(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	repo := repository.NewRequestRepo(testDB.Pool)
	u := mustUser(t, "mod", models.RoleMod)

	req, err := repo.Create(ctx, "Frieren", models.CategoryCurrentFuture, u.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	dest, err := testsupport.SeedDestination(ctx, testDB.Pool, "Plex", u.ID)
	if err != nil {
		t.Fatalf("seed dest: %v", err)
	}
	if err := repo.AddDestination(ctx, req.ID, dest.ID); err != nil {
		t.Fatalf("add dest: %v", err)
	}

	deleted, err := repo.Delete(ctx, req.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete reported no row removed, want true")
	}

	// The junction row is removed automatically via ON DELETE CASCADE.
	n, err := repo.GetDestinationCount(ctx, req.ID)
	if err != nil {
		t.Fatalf("dest count: %v", err)
	}
	if n != 0 {
		t.Fatalf("destination count = %d, want 0 after cascade", n)
	}

	// Deleting a missing request returns (false, nil), not an error.
	deleted, err = repo.Delete(ctx, req.ID)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if deleted {
		t.Fatal("second delete reported a row removed for a missing request")
	}
}

func TestRequestRepo_CreateWithDetails(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	repo := repository.NewRequestRepo(testDB.Pool)
	u := mustUser(t, "mod", models.RoleMod)

	d1, err := testsupport.SeedDestination(ctx, testDB.Pool, "Plex", u.ID)
	if err != nil {
		t.Fatalf("seed dest 1: %v", err)
	}
	d2, err := testsupport.SeedDestination(ctx, testDB.Pool, "Jellyfin", u.ID)
	if err != nil {
		t.Fatalf("seed dest 2: %v", err)
	}

	status := models.StatusNeedToGet
	url := "https://anidb.net/anime/1"
	got, err := repo.CreateWithDetails(ctx, "Frieren", models.CategoryCurrentFuture, u.ID,
		&status, &url, []uuid.UUID{d1.ID, d2.ID})
	if err != nil {
		t.Fatalf("create with details: %v", err)
	}

	if got.Status != models.StatusNeedToGet {
		t.Errorf("status = %q, want %q", got.Status, models.StatusNeedToGet)
	}
	if got.AnidbURL == nil || *got.AnidbURL != url {
		t.Errorf("anidb_url = %v, want %q", got.AnidbURL, url)
	}
	if len(got.ServerDestinations) != 2 {
		t.Errorf("destinations = %d, want 2", len(got.ServerDestinations))
	}

	// A duplicate name still conflicts, and the transaction rolls back so no
	// orphaned destination links are left behind.
	_, err = repo.CreateWithDetails(ctx, "frieren", models.CategoryCurrentFuture, u.ID, nil, nil, []uuid.UUID{d1.ID})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create: got %v, want an 'already exists' error", err)
	}
}

func resetDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		t.Skip("no test database available")
	}
	if err := testDB.TruncateAll(context.Background()); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func mustUser(t *testing.T, name string, role models.Role) *models.User {
	t.Helper()
	u, err := testsupport.SeedUser(context.Background(), testDB.Pool, name, role)
	if err != nil {
		t.Fatalf("seed user %q: %v", name, err)
	}
	return u
}
