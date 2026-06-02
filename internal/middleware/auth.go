package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
)

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(userContextKey).(*models.User)
	return u
}

// SessionFromContext extracts the session from the request context.
func SessionFromContext(ctx context.Context) *models.Session {
	s, _ := ctx.Value(sessionContextKey).(*models.Session)
	return s
}

// Auth returns middleware that validates session tokens and populates context.
func Auth(sessions *repository.SessionRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			tokenHash := auth.HashToken(token)
			session, user, err := sessions.GetByTokenHash(r.Context(), tokenHash)
			if err != nil || session == nil || user == nil {
				next.ServeHTTP(w, r)
				return
			}

			if user.Disabled {
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, sessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects unauthenticated requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects requests from users without the specified roles.
func RequireRole(roles ...models.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"authentication required"}`))
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			for _, role := range roles {
				if user.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"insufficient permissions"}`))
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

// extractToken gets the session token from the cookie or Authorization header.
func extractToken(r *http.Request) string {
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	if header := r.Header.Get("Authorization"); header != "" {
		if strings.HasPrefix(header, "Bearer ") {
			return strings.TrimPrefix(header, "Bearer ")
		}
	}

	return ""
}
