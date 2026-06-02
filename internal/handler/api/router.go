package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"time"

	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

// NewRouter creates the API router with all endpoints.
func NewRouter(
	users *repository.UserRepo,
	sessions *repository.SessionRepo,
	requests *repository.RequestRepo,
	invites *repository.InviteCodeRepo,
	serverDests *repository.ServerDestRepo,
) chi.Router {
	r := chi.NewRouter()

	authHandler := NewAuthHandler(users, sessions, invites)
	requestHandler := NewRequestHandler(requests)
	adminHandler := NewAdminHandler(users, invites, serverDests)

	// Auth routes (public)
	r.Route("/auth", func(r chi.Router) {
		r.Use(httprate.LimitByIP(10, 1*time.Minute))
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
