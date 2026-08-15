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

	"github.com/Kagejitsu/anime-request-server/internal/handler/api"
	"github.com/Kagejitsu/anime-request-server/internal/middleware"
	"github.com/Kagejitsu/anime-request-server/internal/models"
	"github.com/Kagejitsu/anime-request-server/internal/ratelimit"
	"github.com/Kagejitsu/anime-request-server/internal/repository"
	"github.com/Kagejitsu/anime-request-server/internal/testsupport"
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

func TestAPI_CreateRequest_ModFields(t *testing.T) {
	resetDB(t)
	srv, _ := newTestServer(t)
	ctx := context.Background()

	mod := mustUser(t, "mod", models.RoleMod)
	modTok := mustSession(t, mod.ID)
	dest, err := testsupport.SeedDestination(ctx, testDB.Pool, "Plex", mod.ID)
	if err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	// A mod can set status, anidb_url, and destinations at creation.
	body := fmt.Sprintf(`{"name":"Frieren","category":"current_future","status":"need_to_get","anidb_url":"https://anidb.net/anime/1","server_destination_ids":["%s"]}`, dest.ID)
	rec := do(t, srv, http.MethodPost, "/api/v1/requests/", modTok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mod create: status %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got models.AnimeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != models.StatusNeedToGet {
		t.Errorf("status = %q, want need_to_get", got.Status)
	}
	if got.AnidbURL == nil || *got.AnidbURL != "https://anidb.net/anime/1" {
		t.Errorf("anidb_url = %v, want the submitted URL", got.AnidbURL)
	}
	if len(got.ServerDestinations) != 1 {
		t.Errorf("destinations = %d, want 1", len(got.ServerDestinations))
	}

	// A regular user's mod-only fields are ignored: defaults apply.
	plain := mustUser(t, "joe", models.RoleUser)
	userTok := mustSession(t, plain.ID)
	body = fmt.Sprintf(`{"name":"Bocchi","category":"current_future","status":"done","server_destination_ids":["%s"]}`, dest.ID)
	rec = do(t, srv, http.MethodPost, "/api/v1/requests/", userTok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("user create: status %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// Fresh struct: server_destinations is omitempty, so reusing the previous
	// value would leave a stale slice when the field is absent.
	var userGot models.AnimeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &userGot); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if userGot.Status != models.StatusNew {
		t.Errorf("user status = %q, want new (mod fields must be ignored)", userGot.Status)
	}
	if len(userGot.ServerDestinations) != 0 {
		t.Errorf("user destinations = %d, want 0 (mod fields must be ignored)", len(userGot.ServerDestinations))
	}
}

func TestAPI_CreateRequest_AltName(t *testing.T) {
	resetDB(t)
	srv, _ := newTestServer(t)

	mod := mustUser(t, "mod", models.RoleMod)
	modTok := mustSession(t, mod.ID)

	// A mod sets an alt name; it round-trips.
	rec := do(t, srv, http.MethodPost, "/api/v1/requests/", modTok,
		`{"name":"Kusuriya no Hitorigoto","category":"current_future","alt_name":"The Apothecary Diaries"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mod create: status %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got models.AnimeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AltName == nil || *got.AltName != "The Apothecary Diaries" {
		t.Errorf("alt_name = %v, want the submitted alt", got.AltName)
	}

	// A new request whose name matches the existing alt is a 409.
	rec = do(t, srv, http.MethodPost, "/api/v1/requests/", modTok,
		`{"name":"the apothecary diaries","category":"current_future"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("name == existing alt: status %d, want 409", rec.Code)
	}

	// A regular user's alt_name is ignored.
	user := mustUser(t, "joe", models.RoleUser)
	rec = do(t, srv, http.MethodPost, "/api/v1/requests/", mustSession(t, user.ID),
		`{"name":"Bocchi the Rock","category":"current_future","alt_name":"Bocchi"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("user create: status %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var userGot models.AnimeRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &userGot); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if userGot.AltName != nil {
		t.Errorf("user alt_name = %v, want nil (mod-only field ignored)", userGot.AltName)
	}
}

func TestAPI_CreateRequest_BadDestination(t *testing.T) {
	resetDB(t)
	srv, _ := newTestServer(t)

	mod := mustUser(t, "mod", models.RoleMod)
	tok := mustSession(t, mod.ID)

	// A malformed UUID is rejected up front.
	rec := do(t, srv, http.MethodPost, "/api/v1/requests/", tok,
		`{"name":"A","category":"current_future","server_destination_ids":["not-a-uuid"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed dest: status %d, want 400", rec.Code)
	}

	// A well-formed but non-existent destination is a 400, not a 500.
	body := fmt.Sprintf(`{"name":"B","category":"current_future","server_destination_ids":["%s"]}`, uuid.New())
	rec = do(t, srv, http.MethodPost, "/api/v1/requests/", tok, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-existent dest: status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPI_Notes(t *testing.T) {
	resetDB(t)
	srv, requests := newTestServer(t)
	ctx := context.Background()

	mod := mustUser(t, "mod", models.RoleMod)
	modTok := mustSession(t, mod.ID)
	user := mustUser(t, "joe", models.RoleUser)
	userTok := mustSession(t, user.ID)

	req, err := requests.Create(ctx, "Steins;Gate", models.CategoryCurrentFuture, mod.ID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	base := "/api/v1/requests/" + req.ID.String() + "/notes"

	// A regular user can post a note, attributed to them.
	rec := do(t, srv, http.MethodPost, base, userTok, `{"body":"a user note"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("user post: status %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var note models.Note
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if note.AuthorUsername != "joe" || note.Body != "a user note" {
		t.Errorf("note = %+v", note)
	}

	// Empty body -> 400.
	if rec := do(t, srv, http.MethodPost, base, userTok, `{"body":"   "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body: status %d, want 400", rec.Code)
	}

	// Anyone authenticated can list notes.
	if rec := do(t, srv, http.MethodGet, base, userTok, ""); rec.Code != http.StatusOK {
		t.Fatalf("list: status %d, want 200", rec.Code)
	}

	// Delete: forbidden for a regular user, allowed for a mod, 404 once gone.
	delPath := base + "/" + note.ID.String()
	if rec := do(t, srv, http.MethodDelete, delPath, userTok, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("user delete: status %d, want 403", rec.Code)
	}
	// A note can't be deleted via a mismatched request URL.
	other, err := requests.Create(ctx, "Other Show", models.CategoryCurrentFuture, mod.ID)
	if err != nil {
		t.Fatalf("seed other: %v", err)
	}
	mismatch := "/api/v1/requests/" + other.ID.String() + "/notes/" + note.ID.String()
	if rec := do(t, srv, http.MethodDelete, mismatch, modTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("mismatched delete: status %d, want 404", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, delPath, modTok, ""); rec.Code != http.StatusOK {
		t.Fatalf("mod delete: status %d, want 200", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, delPath, modTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("repeat delete: status %d, want 404", rec.Code)
	}
}

func TestAPI_Notes_BlockedUser(t *testing.T) {
	resetDB(t)
	srv, requests := newTestServer(t)
	ctx := context.Background()

	admin := mustUser(t, "admin", models.RoleAdmin)
	adminTok := mustSession(t, admin.ID)
	blocked := mustUser(t, "spammer", models.RoleUser)
	blockedTok := mustSession(t, blocked.ID)

	req, err := requests.Create(ctx, "Bocchi", models.CategoryCurrentFuture, admin.ID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	base := "/api/v1/requests/" + req.ID.String() + "/notes"

	// Admin blocks the user from posting notes.
	if rec := do(t, srv, http.MethodPatch, "/api/v1/admin/users/"+blocked.ID.String(), adminTok, `{"notes_blocked":true}`); rec.Code != http.StatusOK {
		t.Fatalf("block user: status %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The block takes effect immediately (auth re-reads the user each request).
	if rec := do(t, srv, http.MethodPost, base, blockedTok, `{"body":"spam"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("blocked post: status %d, want 403", rec.Code)
	}
	// A blocked user can still read notes.
	if rec := do(t, srv, http.MethodGet, base, blockedTok, ""); rec.Code != http.StatusOK {
		t.Fatalf("blocked read: status %d, want 200", rec.Code)
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
