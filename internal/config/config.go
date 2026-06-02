package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the server.
type Config struct {
	DatabaseURL   string
	ServerHost    string
	ServerPort    int
	SessionSecret string
	WebUIEnabled  bool
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	host := os.Getenv("SERVER_HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	port := 8080
	if p := os.Getenv("SERVER_PORT"); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid SERVER_PORT: %w", err)
		}
	}

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}

	webUI := true
	if v := os.Getenv("WEBUI_ENABLED"); v != "" {
		var err error
		webUI, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEBUI_ENABLED: %w", err)
		}
	}

	return &Config{
		DatabaseURL:   dbURL,
		ServerHost:    host,
		ServerPort:    port,
		SessionSecret: secret,
		WebUIEnabled:  webUI,
	}, nil
}

// ListenAddr returns the host:port string for the HTTP server.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}
