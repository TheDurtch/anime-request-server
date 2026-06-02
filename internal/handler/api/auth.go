package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/ratelimit"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

const sessionDuration = 24 * time.Hour

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	users       *repository.UserRepo
	sessions    *repository.SessionRepo
	invites     *repository.InviteCodeRepo
	loginLimiter *ratelimit.LoginLimiter
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(users *repository.UserRepo, sessions *repository.SessionRepo, invites *repository.InviteCodeRepo, loginLimiter *ratelimit.LoginLimiter) *AuthHandler {
	return &AuthHandler{users: users, sessions: sessions, invites: invites, loginLimiter: loginLimiter}
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	clientIP := ratelimit.GetClientIP(r)

	// Check if IP is banned
	banned, notified := h.loginLimiter.IsBanned(clientIP)
	if banned {
		if notified {
			// Already notified, just return 418 I'm a teapot
			w.WriteHeader(http.StatusTeapot)
			return
		}
		// First ban message
		h.loginLimiter.MarkNotified(clientIP)
		Error(w, http.StatusTooManyRequests, "too many failed login attempts - temporarily banned")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code,omitempty"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		Error(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !auth.CheckPassword(user.PasswordHash, req.Password) {
		// Record failed attempt
		nowBanned := h.loginLimiter.RecordFailedAttempt(clientIP)
		if nowBanned {
			h.loginLimiter.MarkNotified(clientIP)
			Error(w, http.StatusTooManyRequests, "too many failed login attempts - temporarily banned")
			return
		}
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if user.Disabled {
		// Don't record as failed attempt if account is disabled
		Error(w, http.StatusForbidden, "account is disabled")
		return
	}

	// TOTP check (if enabled for user)
	if user.TOTPEnabled {
		if req.TOTPCode == "" {
			Error(w, http.StatusUnauthorized, "TOTP code required")
			return
		}
		// TODO: validate TOTP code against user.TOTPSecret
		// For now, this is a placeholder for TOTP validation
	}

	token, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	expiresAt := time.Now().Add(sessionDuration)
	_, err = h.sessions.Create(r.Context(), user.ID, tokenHash, expiresAt)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
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

	JSON(w, http.StatusOK, map[string]any{
		"user":  user,
		"token": token,
	})
}

// Logout handles POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session := middleware.SessionFromContext(r.Context())
	if session == nil {
		Error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	if err := h.sessions.Delete(r.Context(), session.ID); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	JSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// RedeemInvite handles POST /api/v1/auth/redeem-invite
func (h *AuthHandler) RedeemInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" || req.Username == "" || req.Password == "" {
		Error(w, http.StatusBadRequest, "code, username, and password are required")
		return
	}
	if len(req.Password) < 8 {
		Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	invite, err := h.invites.GetByCode(r.Context(), req.Code)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if invite == nil {
		Error(w, http.StatusBadRequest, "invalid invite code")
		return
	}
	if invite.UsedBy != nil {
		Error(w, http.StatusBadRequest, "invite code already used")
		return
	}
	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
		Error(w, http.StatusBadRequest, "invite code expired")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.users.Create(r.Context(), req.Username, passwordHash, models.RoleUser)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			Error(w, http.StatusConflict, "username already exists")
			return
		}
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := h.invites.MarkUsed(r.Context(), invite.ID, user.ID); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Auto-login
	token, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	expiresAt := time.Now().Add(sessionDuration)
	if _, err := h.sessions.Create(r.Context(), user.ID, tokenHash, expiresAt); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
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

	JSON(w, http.StatusCreated, map[string]any{
		"user":  user,
		"token": token,
	})
}
