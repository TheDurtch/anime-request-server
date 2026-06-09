package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/handler/web"
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
		fmt.Fprintf(os.Stderr, "skipping web DB tests: %v\n", err)
		os.Exit(0)
	}
	testDB = db
	code := m.Run()
	_ = db.Close(ctx)
	os.Exit(code)
}

// newWebServer wires the web handler behind the real auth middleware, exactly as
// main.go does, so template execution and RequireAuth run for real.
func newWebServer(t *testing.T) http.Handler {
	t.Helper()
	pool := testDB.Pool
	users := repository.NewUserRepo(pool)
	sessions := repository.NewSessionRepo(pool)
	requests := repository.NewRequestRepo(pool)
	invites := repository.NewInviteCodeRepo(pool)
	dests := repository.NewServerDestRepo(pool)
	limiter := ratelimit.NewLoginLimiter(5, 5*time.Minute, 15*time.Minute, "")

	h, err := web.NewHandler(users, sessions, requests, invites, dests, limiter, false)
	if err != nil {
		t.Fatalf("new web handler: %v", err)
	}
	r := chi.NewRouter()
	r.Use(middleware.Auth(sessions))
	r.Mount("/", h.Routes(sessions))
	return r
}

func TestWeb_NewRequestPage_ModFields(t *testing.T) {
	resetDB(t)
	srv := newWebServer(t)
	ctx := context.Background()

	// A mod sees the status, server-destination, and AniDB URL fields, plus the
	// seeded destination name — confirming the template renders without error.
	mod := mustUser(t, "mod", models.RoleMod)
	if _, err := testsupport.SeedDestination(ctx, testDB.Pool, "Plex", mod.ID); err != nil {
		t.Fatalf("seed dest: %v", err)
	}
	modBody := get(t, srv, "/requests/new", mustSession(t, mod.ID))
	for _, want := range []string{"Server Destinations", "AniDB URL", `name="status"`, "Plex"} {
		if !strings.Contains(modBody, want) {
			t.Errorf("mod new-request page missing %q", want)
		}
	}

	// A regular user sees none of the mod-only fields.
	user := mustUser(t, "joe", models.RoleUser)
	userBody := get(t, srv, "/requests/new", mustSession(t, user.ID))
	for _, notWant := range []string{"Server Destinations", "AniDB URL", `name="status"`} {
		if strings.Contains(userBody, notWant) {
			t.Errorf("user new-request page unexpectedly contains %q", notWant)
		}
	}
}

func TestWeb_NewRequestSubmit_BadDestination(t *testing.T) {
	resetDB(t)
	srv := newWebServer(t)

	mod := mustUser(t, "mod", models.RoleMod)
	tok := mustSession(t, mod.ID)

	form := url.Values{
		"name":                   {"Ghosty"},
		"category":               {"current_future"},
		"server_destination_ids": {"not-a-uuid"},
	}
	rec := postForm(t, srv, "/requests/new", tok, form)

	// The form is re-rendered with an error (200), not redirected or 500'd.
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (form re-render); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Invalid server destination") {
		t.Errorf("expected a validation error in the re-rendered form")
	}

	// And no request was created.
	requests := repository.NewRequestRepo(testDB.Pool)
	if dup, err := requests.CheckDuplicate(context.Background(), "Ghosty"); err != nil {
		t.Fatalf("check duplicate: %v", err)
	} else if dup {
		t.Error("a request was created despite the invalid destination")
	}
}

func postForm(t *testing.T, h http.Handler, path, token string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, path, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200; body=%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
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
