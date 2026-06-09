package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

// AdminHandler handles admin endpoints.
type AdminHandler struct {
	users       *repository.UserRepo
	invites     *repository.InviteCodeRepo
	serverDests *repository.ServerDestRepo
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(users *repository.UserRepo, invites *repository.InviteCodeRepo, serverDests *repository.ServerDestRepo) *AdminHandler {
	return &AdminHandler{users: users, invites: invites, serverDests: serverDests}
}

// ListUsers handles GET /api/v1/admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if users == nil {
		users = []models.User{}
	}
	JSON(w, http.StatusOK, users)
}

// CreateUser handles POST /api/v1/admin/users
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" || req.Role == "" {
		Error(w, http.StatusBadRequest, "username, password, and role are required")
		return
	}
	if len(req.Password) < 8 {
		Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.Role != string(models.RoleAdmin) && req.Role != string(models.RoleMod) && req.Role != string(models.RoleUser) {
		Error(w, http.StatusBadRequest, "role must be 'admin', 'mod', or 'user'")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.users.Create(r.Context(), req.Username, passwordHash, models.Role(req.Role))
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			Error(w, http.StatusConflict, "username already exists")
			return
		}
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusCreated, user)
}

// UpdateUser handles PATCH /api/v1/admin/users/{id}
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	// Prevent self-demotion
	currentUser := middleware.UserFromContext(r.Context())
	if currentUser.ID == id {
		Error(w, http.StatusBadRequest, "cannot modify your own account via this endpoint")
		return
	}

	var req struct {
		Role         *string `json:"role"`
		CanBatchAdd  *bool   `json:"can_batch_add"`
		Disabled     *bool   `json:"disabled"`
		NotesBlocked *bool   `json:"notes_blocked"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var role *models.Role
	if req.Role != nil {
		if *req.Role != string(models.RoleAdmin) && *req.Role != string(models.RoleMod) && *req.Role != string(models.RoleUser) {
			Error(w, http.StatusBadRequest, "role must be 'admin', 'mod', or 'user'")
			return
		}
		r := models.Role(*req.Role)
		role = &r
	}

	if err := h.users.Update(r.Context(), id, role, req.CanBatchAdd, req.Disabled, req.NotesBlocked); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, user)
}

// GenerateInvite handles POST /api/v1/admin/invite-codes
func (h *AdminHandler) GenerateInvite(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	var req struct {
		ExpiresInHours *int `json:"expires_in_hours"`
	}
	// Body is optional; an empty body (io.EOF) means "use defaults". Any other
	// decode error is a genuine bad request.
	if err := Decode(r, &req); err != nil && !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code, err := auth.GenerateInviteCode()
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresInHours != nil && *req.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}

	invite, err := h.invites.Create(r.Context(), code, user.ID, expiresAt)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusCreated, invite)
}

// ListInvites handles GET /api/v1/admin/invite-codes
func (h *AdminHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	codes, err := h.invites.List(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if codes == nil {
		codes = []models.InviteCode{}
	}
	JSON(w, http.StatusOK, codes)
}

// ListServerDestinations handles GET /api/v1/server-destinations
func (h *AdminHandler) ListServerDestinations(w http.ResponseWriter, r *http.Request) {
	dests, err := h.serverDests.List(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dests == nil {
		dests = []models.ServerDestination{}
	}
	JSON(w, http.StatusOK, dests)
}

// CreateServerDestination handles POST /api/v1/server-destinations
func (h *AdminHandler) CreateServerDestination(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		Error(w, http.StatusBadRequest, "name is required")
		return
	}

	dest, err := h.serverDests.Create(r.Context(), req.Name, user.ID)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			Error(w, http.StatusConflict, "server destination name already exists")
			return
		}
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusCreated, dest)
}

// DeleteServerDestination handles DELETE /api/v1/server-destinations/{id}
func (h *AdminHandler) DeleteServerDestination(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid server destination ID")
		return
	}

	if err := h.serverDests.Delete(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}
