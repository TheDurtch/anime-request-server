package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/handler/api"
	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/ratelimit"
	"github.com/TheDurtch/anime-request-server/internal/repository"
	"github.com/TheDurtch/anime-request-server/internal/testsupport"
)

var testDB *testsupport.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	db, err := testsupport.StartPostgres(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skipping api DB tests: %v\n", err)
		os.Exit(0)
	}
	testDB = db
	code := m.Run()
	_ = db.Close(ctx)
	os.Exit(code)
}

// newTestServer wires the API router behind the real auth middleware, mounted at
// /api/v1 exactly as main.go does, and returns the handler plus a request repo
// for seeding/asserting.
func newTestServer(t *testing.T) (http.Handler, *repository.RequestRepo) {
	t.Helper()
	pool := testDB.Pool
	users := repository.NewUserRepo(pool)
	sessions := repository.NewSessionRepo(pool)
	requests := repository.NewRequestRepo(pool)
	invites := repository.NewInviteCodeRepo(pool)
	dests := repository.NewServerDestRepo(pool)
	limiter := ratelimit.NewLoginLimiter(5, 5*time.Minute, 15*time.Minute, "")

	r := chi.NewRouter()
	r.Use(middleware.Auth(sessions))
	r.Mount("/api/v1", api.NewRouter(users, sessions, requests, invites, dests, limiter, false))
	return r, requests
}

func TestAPI_DeleteRequest_RBACAndNotFound(t *testing.T) {
	resetDB(t)
	srv, requests := newTestServer(t)
	ctx := context.Background()

	mod := mustUser(t, "mod", models.RoleMod)
	modTok := mustSession(t, mod.ID)
	plain := mustUser(t, "joe", models.RoleUser)
	userTok := mustSession(t, plain.ID)

	req, err := requests.Create(ctx, "Steins;Gate", models.CategoryCurrentFuture, mod.ID)
	if err != nil {
		t.Fatalf("seed request: %v", err)
	}
	path := "/api/v1/requests/" + req.ID.String()

	// A regular user is forbidden...
	if rec := do(t, srv, http.MethodDelete, path, userTok, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("user delete: status %d, want 403", rec.Code)
	}
	// ...and the request must still exist.
	if got, _ := requests.GetByID(ctx, req.ID); got == nil {
		t.Fatal("request was deleted by a non-mod")
	}

	// A mod can delete.
	if rec := do(t, srv, http.MethodDelete, path, modTok, ""); rec.Code != http.StatusOK {
		t.Fatalf("mod delete: status %d, want 200", rec.Code)
	}
	// Deleting it again is a 404.
	if rec := do(t, srv, http.MethodDelete, path, modTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("repeat delete: status %d, want 404", rec.Code)
	}
}

func TestAPI_UpdateRequest_Name(t *testing.T) {
	resetDB(t)
	srv, requests := newTestServer(t)
	ctx := context.Background()

	mod := mustUser(t, "mod", models.RoleMod)
	tok := mustSession(t, mod.ID)

	a, err := requests.Create(ctx, "Old", models.CategoryCurrentFuture, mod.ID)
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if _, err := requests.Create(ctx, "Taken", models.CategoryCurrentFuture, mod.ID); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	path := "/api/v1/requests/" + a.ID.String()

	// Blank name -> 400.
	if rec := do(t, srv, http.MethodPatch, path, tok, `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name: status %d, want 400", rec.Code)
	}
	// Duplicate name (case-insensitive) -> 409.
	if rec := do(t, srv, http.MethodPatch, path, tok, `{"name":"taken"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate name: status %d, want 409", rec.Code)
	}
	// Valid rename -> 200, and the response body reflects the new name.
	rec := do(t, srv, http.MethodPatch, path, tok, `{"name":"Brand New"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: status %d, want 200", rec.Code)
	}
	var got models.AnimeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Name != "Brand New" {
		t.Fatalf("returned name = %q, want %q", got.Name, "Brand New")
	}
}

func do(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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

func mustSession(t *testing.T, id uuid.UUID) string {
	t.Helper()
	tok, err := testsupport.SeedSession(context.Background(), testDB.Pool, id)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return tok
}
