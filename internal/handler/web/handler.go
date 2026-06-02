package web

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/ratelimit"
	"github.com/TheDurtch/anime-request-server/internal/repository"
	webstatic "github.com/TheDurtch/anime-request-server/web"
)

const sessionDuration = 24 * time.Hour

// templateFuncMap returns the shared template function map.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"add":       func(a, b int) int { return a + b },
		"subtract":  func(a, b int) int { return a - b },
		"eq":        func(a, b any) bool { return a == b },
		"deref":     func(s *string) string { if s != nil { return *s }; return "" },
		"derefUUID": func(u *uuid.UUID) uuid.UUID { if u != nil { return *u }; return uuid.Nil },
		"derefTime": func(t *time.Time) time.Time { if t != nil { return *t }; return time.Time{} },
		"expired": func(t *time.Time) bool {
			if t == nil {
				return false
			}
			return t.Before(time.Now())
		},
	}
}

// Handler serves the web UI.
type Handler struct {
	users        *repository.UserRepo
	sessions     *repository.SessionRepo
	requests     *repository.RequestRepo
	invites      *repository.InviteCodeRepo
	serverDests  *repository.ServerDestRepo
	loginLimiter *ratelimit.LoginLimiter
	templates    *template.Template
}

// NewHandler creates a new web UI handler.
func NewHandler(
	users *repository.UserRepo,
	sessions *repository.SessionRepo,
	requests *repository.RequestRepo,
	invites *repository.InviteCodeRepo,
	serverDests *repository.ServerDestRepo,
	loginLimiter *ratelimit.LoginLimiter,
) (*Handler, error) {
	tmplFS, err := fs.Sub(webstatic.TemplatesFS, "templates")
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("").Funcs(templateFuncMap()).ParseFS(tmplFS, "*.html")
	if err != nil {
		return nil, err
	}

	return &Handler{
		users:        users,
		sessions:     sessions,
		requests:     requests,
		invites:      invites,
		serverDests:  serverDests,
		loginLimiter: loginLimiter,
		templates:    tmpl,
	}, nil
}

// Routes returns the web UI router.
func (h *Handler) Routes(sessionRepo *repository.SessionRepo) chi.Router {
	r := chi.NewRouter()

	// Static files
	staticSub, _ := fs.Sub(webstatic.StaticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Public routes
	r.Get("/login", h.loginPage)
	r.Post("/login", h.loginSubmit)
	r.Get("/redeem-invite", h.redeemInvitePage)
	r.Post("/redeem-invite", h.redeemInviteSubmit)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Post("/logout", h.logout)
		r.Get("/", h.requestsList)
		r.Get("/requests/new", h.requestNewPage)
		r.Post("/requests/new", h.requestNewSubmit)
		r.Get("/requests/batch", h.batchAddPage)
		r.Post("/requests/batch", h.batchAddSubmit)
		r.Get("/requests/{id}", h.requestDetail)
		r.Post("/requests/{id}/edit", h.requestEditSubmit)

		// Admin routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireRole(models.RoleAdmin))

			r.Get("/manage/users", h.usersPage)
			r.Get("/manage/users/{id}", h.userEditPage)
			r.Post("/manage/users/{id}", h.userEditSubmit)
			r.Get("/manage/invites", h.invitesPage)
			r.Post("/manage/invites/generate", h.inviteGenerate)
		})

		// Admin/mod routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireRole(models.RoleAdmin, models.RoleMod))

			r.Get("/manage/server-destinations", h.serverDestsPage)
			r.Post("/manage/server-destinations", h.serverDestCreate)
			r.Post("/manage/server-destinations/{id}/delete", h.serverDestDelete)
		})
	})

	return r
}

func (h *Handler) render(w http.ResponseWriter, name string, data map[string]any) {
	// Parse base + specific template
	err := h.templates.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) renderPage(w http.ResponseWriter, page string, data map[string]any) {
	tmplFS, _ := fs.Sub(webstatic.TemplatesFS, "templates")
	tmpl, err := template.New("").Funcs(templateFuncMap()).ParseFS(tmplFS, "base.html", page+".html")
	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// --- Auth pages ---

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, "login", map[string]any{})
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	clientIP := ratelimit.GetClientIP(r)

	// Check if IP is banned
	banned, _ := h.loginLimiter.IsBanned(clientIP)
	if banned {
		// For web UI, show error page (not 418)
		h.renderPage(w, "login", map[string]any{"Error": "Too many failed login attempts - temporarily banned"})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	totpCode := r.FormValue("totp_code")

	user, err := h.users.GetByUsername(r.Context(), username)
	if err != nil || user == nil || !auth.CheckPassword(user.PasswordHash, password) {
		// Record failed attempt
		nowBanned := h.loginLimiter.RecordFailedAttempt(clientIP)
		if nowBanned {
			h.renderPage(w, "login", map[string]any{"Error": "Too many failed login attempts - temporarily banned"})
			return
		}
		h.renderPage(w, "login", map[string]any{"Error": "Invalid credentials"})
		return
	}
	if user.Disabled {
		// Don't record as failed attempt if account is disabled
		h.renderPage(w, "login", map[string]any{"Error": "Account is disabled"})
		return
	}
	if user.TOTPEnabled && totpCode == "" {
		h.renderPage(w, "login", map[string]any{"Error": "2FA code required"})
		return
	}

	token, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = h.sessions.Create(r.Context(), user.ID, tokenHash, time.Now().Add(sessionDuration))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	// Record successful login (clears failed attempts)
	h.loginLimiter.RecordSuccess(clientIP)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	session := middleware.SessionFromContext(r.Context())
	if session != nil {
		h.sessions.Delete(r.Context(), session.ID)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) redeemInvitePage(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, "redeem_invite", map[string]any{})
}

func (h *Handler) redeemInviteSubmit(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.FormValue("code"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if code == "" || username == "" || password == "" {
		h.renderPage(w, "redeem_invite", map[string]any{"Error": "All fields are required"})
		return
	}
	if len(password) < 8 {
		h.renderPage(w, "redeem_invite", map[string]any{"Error": "Password must be at least 8 characters"})
		return
	}

	invite, err := h.invites.GetByCode(r.Context(), code)
	if err != nil || invite == nil || invite.UsedBy != nil {
		h.renderPage(w, "redeem_invite", map[string]any{"Error": "Invalid or used invite code"})
		return
	}
	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
		h.renderPage(w, "redeem_invite", map[string]any{"Error": "Invite code expired"})
		return
	}

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.users.Create(r.Context(), username, passwordHash, models.RoleUser)
	if err != nil {
		h.renderPage(w, "redeem_invite", map[string]any{"Error": "Username already exists"})
		return
	}

	h.invites.MarkUsed(r.Context(), invite.ID, user.ID)

	// Auto-login
	token, tokenHash, _ := auth.GenerateSessionToken()
	h.sessions.Create(r.Context(), user.ID, tokenHash, time.Now().Add(sessionDuration))
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- Request pages ---

func (h *Handler) requestsList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	filter := repository.RequestFilter{
		Page:    intParam(r.URL.Query().Get("page"), 1),
		PerPage: 25,
	}

	category := r.URL.Query().Get("category")
	if category != "" {
		filter.Category = &category
	}
	status := r.URL.Query().Get("status")
	if status != "" {
		filter.Status = &status
	}

	requests, total, err := h.requests.List(r.Context(), filter)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	totalPages := (total + filter.PerPage - 1) / filter.PerPage
	if totalPages < 1 {
		totalPages = 1
	}

	h.renderPage(w, "requests", map[string]any{
		"User":           user,
		"Requests":       requests,
		"Total":          total,
		"Page":           filter.Page,
		"TotalPages":     totalPages,
		"ActiveCategory": category,
		"ActiveStatus":   status,
	})
}

func (h *Handler) requestNewPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	h.renderPage(w, "request_new", map[string]any{
		"User":     user,
		"Name":     "",
		"Category": "current_future",
	})
}

func (h *Handler) requestNewSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	category := r.FormValue("category")

	if name == "" {
		h.renderPage(w, "request_new", map[string]any{"User": user, "Error": "Name is required", "Name": name, "Category": category})
		return
	}
	if category != string(models.CategoryCurrentFuture) && category != string(models.CategoryFinishedAiring) {
		h.renderPage(w, "request_new", map[string]any{"User": user, "Error": "Invalid category", "Name": name, "Category": category})
		return
	}

	dup, err := h.requests.CheckDuplicate(r.Context(), name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if dup {
		h.renderPage(w, "request_new", map[string]any{"User": user, "Error": "A request with this name already exists", "Name": name, "Category": category})
		return
	}

	_, err = h.requests.Create(r.Context(), name, models.Category(category), user.ID)
	if err != nil {
		h.renderPage(w, "request_new", map[string]any{"User": user, "Error": "Failed to create request", "Name": name, "Category": category})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) batchAddPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if !user.CanBatchAdd {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.renderPage(w, "batch_add", map[string]any{"User": user})
}

func (h *Handler) batchAddSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if !user.CanBatchAdd {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	namesRaw := r.FormValue("names")
	var names []string
	for _, line := range strings.Split(namesRaw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}

	if len(names) == 0 {
		h.renderPage(w, "batch_add", map[string]any{"User": user, "Error": "At least one name is required", "Names": namesRaw})
		return
	}

	count, err := h.requests.CreateBatch(r.Context(), names, user.ID)
	if err != nil {
		h.renderPage(w, "batch_add", map[string]any{"User": user, "Error": "Failed to add entries", "Names": namesRaw})
		return
	}

	h.renderPage(w, "batch_add", map[string]any{
		"User":    user,
		"Success": strings.Replace("Added X entries via batch add.", "X", strings.TrimSpace(intToStr(count)), 1),
	})
}

func (h *Handler) requestDetail(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}

	req, err := h.requests.GetByID(r.Context(), id)
	if err != nil || req == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	dests, _ := h.serverDests.List(r.Context())

	h.renderPage(w, "request_detail", map[string]any{
		"User":               user,
		"Request":            req,
		"Statuses":           models.ValidStatuses(),
		"Categories":         models.ValidCategories(),
		"ServerDestinations": dests,
	})
}

func (h *Handler) requestEditSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user.Role != models.RoleAdmin && user.Role != models.RoleMod {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}

	statusStr := r.FormValue("status")
	categoryStr := r.FormValue("category")
	// TODO: Handle multiple server_destination_id[] checkboxes
	anidbURL := r.FormValue("anidb_url")

	var status *models.Status
	if statusStr != "" && models.IsValidStatus(statusStr) {
		s := models.Status(statusStr)
		status = &s
	}

	var category *models.Category
	if categoryStr != "" && models.IsValidCategory(categoryStr) {
		c := models.Category(categoryStr)
		category = &c
	}

	var anidbPtr *string
	if anidbURL != "" {
		anidbPtr = &anidbURL
	}

	if err := h.requests.Update(r.Context(), id, status, category, anidbPtr); err != nil {
		http.Error(w, "failed to update request", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/requests/"+id.String(), http.StatusSeeOther)
}

// --- Admin pages ---

func (h *Handler) usersPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	users, err := h.users.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderPage(w, "users", map[string]any{"User": user, "Users": users})
}

func (h *Handler) userEditPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}

	editUser, err := h.users.GetByID(r.Context(), id)
	if err != nil || editUser == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	h.renderPage(w, "user_edit", map[string]any{"User": user, "EditUser": editUser})
}

func (h *Handler) userEditSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}

	if user.ID == id {
		http.Redirect(w, r, "/manage/users", http.StatusSeeOther)
		return
	}

	roleStr := r.FormValue("role")
	if roleStr != string(models.RoleAdmin) && roleStr != string(models.RoleMod) && roleStr != string(models.RoleUser) {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	role := models.Role(roleStr)
	canBatchAdd := r.FormValue("can_batch_add") == "true"
	disabled := r.FormValue("disabled") == "true"

	if err := h.users.Update(r.Context(), id, &role, &canBatchAdd, &disabled); err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/manage/users", http.StatusSeeOther)
}

func (h *Handler) invitesPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	codes, _ := h.invites.List(r.Context())

	data := map[string]any{"User": user, "Codes": codes}
	h.renderPage(w, "invites", data)
}

func (h *Handler) inviteGenerate(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	code, err := auth.GenerateInviteCode()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.invites.Create(r.Context(), code, user.ID, nil)

	codes, _ := h.invites.List(r.Context())
	h.renderPage(w, "invites", map[string]any{"User": user, "Codes": codes, "NewCode": code})
}

// --- Server destination pages ---

func (h *Handler) serverDestsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	dests, _ := h.serverDests.List(r.Context())
	h.renderPage(w, "server_destinations", map[string]any{"User": user, "Destinations": dests})
}

func (h *Handler) serverDestCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		dests, _ := h.serverDests.List(r.Context())
		h.renderPage(w, "server_destinations", map[string]any{"User": user, "Destinations": dests, "Error": "Name is required"})
		return
	}

	_, err := h.serverDests.Create(r.Context(), name, user.ID)
	if err != nil {
		dests, _ := h.serverDests.List(r.Context())
		h.renderPage(w, "server_destinations", map[string]any{"User": user, "Destinations": dests, "Error": err.Error()})
		return
	}

	http.Redirect(w, r, "/manage/server-destinations", http.StatusSeeOther)
}

func (h *Handler) serverDestDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid ID", http.StatusBadRequest)
		return
	}
	h.serverDests.Delete(r.Context(), id)
	http.Redirect(w, r, "/manage/server-destinations", http.StatusSeeOther)
}

// --- Helpers ---

func intParam(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		return def
	}
	return n
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
