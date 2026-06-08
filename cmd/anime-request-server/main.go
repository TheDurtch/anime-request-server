package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/TheDurtch/anime-request-server/internal/auth"
	"github.com/TheDurtch/anime-request-server/internal/config"
	"github.com/TheDurtch/anime-request-server/internal/database"
	"github.com/TheDurtch/anime-request-server/internal/handler/api"
	"github.com/TheDurtch/anime-request-server/internal/handler/web"
	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/ratelimit"
	"github.com/TheDurtch/anime-request-server/internal/repository"
)

var rootCmd = &cobra.Command{
	Use:   "anime-request-server",
	Short: "Anime request board server",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the database and create the admin user",
	RunE:  runInit,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE:  runServe,
}

var createUserCmd = &cobra.Command{
	Use:   "create-user",
	Short: "Create a new user",
	RunE:  runCreateUser,
}

var generateInviteCmd = &cobra.Command{
	Use:   "generate-invite",
	Short: "Generate a new invite code",
	RunE:  runGenerateInvite,
}

func init() {
	createUserCmd.Flags().String("username", "", "Username (required)")
	createUserCmd.Flags().String("password", "", "Password (required, min 8 chars)")
	createUserCmd.Flags().String("role", "user", "Role: admin, mod, or user")

	generateInviteCmd.Flags().Int("expires-in-hours", 0, "Expiry in hours (0 = never)")

	rootCmd.AddCommand(initCmd, serveCmd, createUserCmd, generateInviteCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	fmt.Println("Running database migrations...")
	if err := database.RunMigrations(databaseURL); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println("✓ Migrations applied successfully.")

	ctx := context.Background()
	pool, err := database.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	userRepo := repository.NewUserRepo(pool)

	// Check if admin exists
	users, err := userRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("checking users: %w", err)
	}

	hasAdmin := false
	for _, u := range users {
		if u.Role == models.RoleAdmin {
			hasAdmin = true
			break
		}
	}

	if hasAdmin {
		fmt.Println("✓ Admin user already exists.")
		fmt.Println("\nRun 'anime-request-server serve' to start the server.")
		return nil
	}

	fmt.Println("\nNo admin user found. Let's create one.")
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Admin username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	fmt.Print("Admin password (min 8 chars): ")
	passwordRaw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}
	password := strings.TrimSpace(string(passwordRaw))
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	fmt.Print("Confirm admin password: ")
	confirmRaw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("reading password confirmation: %w", err)
	}
	if password != strings.TrimSpace(string(confirmRaw)) {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	_, err = userRepo.Create(ctx, username, hash, models.RoleAdmin)
	if err != nil {
		return fmt.Errorf("creating admin user: %w", err)
	}

	fmt.Printf("✓ Admin user '%s' created.\n", username)
	fmt.Println("\nRun 'anime-request-server serve' to start the server.")
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Check if initialized
	initialized, err := database.IsInitialized(ctx, pool)
	if err != nil {
		return fmt.Errorf("checking initialization: %w", err)
	}
	if !initialized {
		return fmt.Errorf("database is not initialized: run 'anime-request-server init' to set up the database and create an admin user")
	}

	// Repositories
	userRepo := repository.NewUserRepo(pool)
	sessionRepo := repository.NewSessionRepo(pool)
	requestRepo := repository.NewRequestRepo(pool)
	inviteRepo := repository.NewInviteCodeRepo(pool)
	serverDestRepo := repository.NewServerDestRepo(pool)

	// Create shared login limiter: 5 attempts per 5 minutes, 15 minute ban.
	// The limiter keys on the trusted client IP (see config.RealIPHeader).
	loginLimiter := ratelimit.NewLoginLimiter(5, 5*time.Minute, 15*time.Minute, cfg.RealIPHeader)

	// Router. We deliberately do not use chimiddleware.RealIP: it rewrites
	// RemoteAddr from spoofable client headers with a fixed precedence. Client
	// IP is instead derived from the explicitly configured trusted header via
	// loginLimiter.ClientIP / ratelimit.GetClientIP.
	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Auth(sessionRepo))

	// API routes
	r.Mount("/api/v1", api.NewRouter(userRepo, sessionRepo, requestRepo, inviteRepo, serverDestRepo, loginLimiter, cfg.CookieSecure))

	// Web UI
	if cfg.WebUIEnabled {
		webHandler, err := web.NewHandler(userRepo, sessionRepo, requestRepo, inviteRepo, serverDestRepo, loginLimiter, cfg.CookieSecure)
		if err != nil {
			return fmt.Errorf("initializing web UI: %w", err)
		}
		r.Mount("/", webHandler.Routes(sessionRepo))
		slog.Info("Web UI enabled")
	} else {
		slog.Info("Web UI disabled (API-only mode)")
	}

	// Session cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleaned, err := sessionRepo.DeleteExpired(context.Background())
			if err != nil {
				slog.Error("session cleanup failed", "error", err)
			} else if cleaned > 0 {
				slog.Info("cleaned expired sessions", "count", cleaned)
			}
		}
	}()

	// HTTP server with graceful shutdown
	srv := &http.Server{
		Addr:         cfg.ListenAddr(),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", cfg.ListenAddr())
		errCh <- srv.ListenAndServe()
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}

func runCreateUser(cmd *cobra.Command, args []string) error {
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	role, _ := cmd.Flags().GetString("role")

	if username == "" || password == "" {
		return fmt.Errorf("--username and --password are required")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if role != string(models.RoleAdmin) && role != string(models.RoleMod) && role != string(models.RoleUser) {
		return fmt.Errorf("role must be 'admin', 'mod', or 'user'")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	userRepo := repository.NewUserRepo(pool)
	user, err := userRepo.Create(ctx, username, hash, models.Role(role))
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	fmt.Printf("✓ User '%s' created with role '%s' (ID: %s)\n", user.Username, user.Role, user.ID)
	return nil
}

func runGenerateInvite(cmd *cobra.Command, args []string) error {
	expiresInHours, _ := cmd.Flags().GetInt("expires-in-hours")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Need an admin user to attribute the invite to
	userRepo := repository.NewUserRepo(pool)
	users, err := userRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	var adminID *models.User
	for _, u := range users {
		if u.Role == models.RoleAdmin {
			adminID = &u
			break
		}
	}
	if adminID == nil {
		return fmt.Errorf("no admin user found — run 'anime-request-server init' first")
	}

	code, err := auth.GenerateInviteCode()
	if err != nil {
		return fmt.Errorf("generating code: %w", err)
	}

	inviteRepo := repository.NewInviteCodeRepo(pool)

	var expiresAt *time.Time
	if expiresInHours > 0 {
		t := time.Now().Add(time.Duration(expiresInHours) * time.Hour)
		expiresAt = &t
	}

	_, err = inviteRepo.Create(ctx, code, adminID.ID, expiresAt)
	if err != nil {
		return fmt.Errorf("creating invite: %w", err)
	}

	fmt.Printf("✓ Invite code: %s\n", code)
	if expiresAt != nil {
		fmt.Printf("  Expires: %s\n", expiresAt.Format(time.RFC3339))
	} else {
		fmt.Println("  Expires: never")
	}
	return nil
}
