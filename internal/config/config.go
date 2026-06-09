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
	// RealIPHeader is the request header trusted to carry the originating
	// client IP, set by the reverse proxy in front of this server (e.g.
	// "CF-Connecting-IP" behind Cloudflare, "X-Forwarded-For" behind
	// Caddy/Pangolin/nginx). When empty, no forwarding headers are trusted
	// and the TCP peer address is used instead.
	RealIPHeader string
	// CookieSecure controls the Secure flag on session cookies. Defaults to
	// true; set COOKIE_SECURE=false for local development over plain HTTP.
	CookieSecure bool
	// LogRequestIPs, when true, logs every IP-bearing header plus the derived
	// client IP for each request. Off by default because it records client IPs
	// (PII) on every request; set LOG_REQUEST_IPS=true to diagnose REAL_IP_HEADER
	// behavior behind a reverse proxy.
	LogRequestIPs bool
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

	// SESSION_SECRET is currently unused (sessions are random tokens stored as
	// SHA-256 hashes, not signed cookies). It is read but optional, reserved
	// for future cookie signing.
	secret := os.Getenv("SESSION_SECRET")

	webUI := true
	if v := os.Getenv("WEBUI_ENABLED"); v != "" {
		var err error
		webUI, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEBUI_ENABLED: %w", err)
		}
	}

	cookieSecure := true
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		var err error
		cookieSecure, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid COOKIE_SECURE: %w", err)
		}
	}

	logRequestIPs := false
	if v := os.Getenv("LOG_REQUEST_IPS"); v != "" {
		var err error
		logRequestIPs, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid LOG_REQUEST_IPS: %w", err)
		}
	}

	return &Config{
		DatabaseURL:   dbURL,
		ServerHost:    host,
		ServerPort:    port,
		SessionSecret: secret,
		WebUIEnabled:  webUI,
		RealIPHeader:  os.Getenv("REAL_IP_HEADER"),
		CookieSecure:  cookieSecure,
		LogRequestIPs: logRequestIPs,
	}, nil
}

// ListenAddr returns the host:port string for the HTTP server.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}
