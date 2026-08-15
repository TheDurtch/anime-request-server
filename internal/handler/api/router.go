package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"

	"github.com/Kagejitsu/anime-request-server/internal/middleware"
	"github.com/Kagejitsu/anime-request-server/internal/models"
	"github.com/Kagejitsu/anime-request-server/internal/ratelimit"
	"github.com/Kagejitsu/anime-request-server/internal/repository"
)

// NewRouter creates the API router with all endpoints.
func NewRouter(
	users *repository.UserRepo,
	sessions *repository.SessionRepo,
	requests *repository.RequestRepo,
	invites *repository.InviteCodeRepo,
	serverDests *repository.ServerDestRepo,
	loginLimiter *ratelimit.LoginLimiter,
	cookieSecure bool,
) chi.Router {
	r := chi.NewRouter()

	authHandler := NewAuthHandler(users, sessions, invites, loginLimiter, cookieSecure)
	requestHandler := NewRequestHandler(requests)
	adminHandler := NewAdminHandler(users, invites, serverDests)

	// Auth routes (public). Key the limiter on the trusted client IP (the same
	// identity the LoginLimiter uses) rather than the raw RemoteAddr.
	r.Route("/auth", func(r chi.Router) {
		r.Use(httprate.Limit(10, 1*time.Minute, httprate.WithKeyFuncs(func(req *http.Request) (string, error) {
			return loginLimiter.ClientIP(req), nil
		})))
		r.Post("/login", authHandler.Login)
		r.Post("/redeem-invite", authHandler.RedeemInvite)
	})

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Post("/auth/logout", authHandler.Logout)

		// Requests
		r.Route("/requests", func(r chi.Router) {
			r.Get("/", requestHandler.List)
			r.Post("/", requestHandler.Create)
			r.Post("/batch", requestHandler.BatchCreate)
			r.Get("/{id}", requestHandler.Get)

			// Admin/mod only
			r.With(middleware.RequireRole(models.RoleAdmin, models.RoleMod)).
				Patch("/{id}", requestHandler.Update)
			r.With(middleware.RequireRole(models.RoleAdmin, models.RoleMod)).
				Delete("/{id}", requestHandler.Delete)

			// Destination management (admin/mod only)
			r.With(middleware.RequireRole(models.RoleAdmin, models.RoleMod)).
				Post("/{id}/destinations", requestHandler.AddDestination)
			r.With(middleware.RequireRole(models.RoleAdmin, models.RoleMod)).
				Delete("/{id}/destinations/{dest_id}", requestHandler.RemoveDestination)

			// Notes: any authenticated user can read and post (unless blocked);
			// only admin/mod can delete.
			r.Get("/{id}/notes", requestHandler.ListNotes)
			r.Post("/{id}/notes", requestHandler.AddNote)
			r.With(middleware.RequireRole(models.RoleAdmin, models.RoleMod)).
				Delete("/{id}/notes/{note_id}", requestHandler.DeleteNote)
		})

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Use(middleware.RequireRole(models.RoleAdmin))

			r.Get("/users", adminHandler.ListUsers)
			r.Post("/users", adminHandler.CreateUser)
			r.Patch("/users/{id}", adminHandler.UpdateUser)

			r.Get("/invite-codes", adminHandler.ListInvites)
			r.Post("/invite-codes", adminHandler.GenerateInvite)
		})

		// Server destinations (admin/mod)
		r.Route("/server-destinations", func(r chi.Router) {
			r.Use(middleware.RequireRole(models.RoleAdmin, models.RoleMod))

			r.Get("/", adminHandler.ListServerDestinations)
			r.Post("/", adminHandler.CreateServerDestination)
			r.Delete("/{id}", adminHandler.DeleteServerDestination)
		})
	})

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}
