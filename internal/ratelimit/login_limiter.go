package ratelimit

import (
	"net/http"
	"sync"
	"time"
)

// LoginLimiter tracks failed login attempts and bans IPs that exceed limits.
type LoginLimiter struct {
	mu       sync.RWMutex
	attempts map[string]*ipAttempts
	// Ban after this many failed attempts
	maxAttempts int
	// Window for counting attempts
	window time.Duration
	// Ban duration
	banDuration time.Duration
}

type ipAttempts struct {
	count         int
	firstTry      time.Time
	banned        bool
	bannedAt      time.Time
	notifiedOfBan bool // Track if we've sent the first ban message
}

// NewLoginLimiter creates a new login rate limiter with temporary bans.
// maxAttempts: number of failed attempts before ban (e.g., 5)
// window: time window for counting attempts (e.g., 5 minutes)
// banDuration: how long to ban the IP (e.g., 15 minutes)
func NewLoginLimiter(maxAttempts int, window, banDuration time.Duration) *LoginLimiter {
	limiter := &LoginLimiter{
		attempts:    make(map[string]*ipAttempts),
		maxAttempts: maxAttempts,
		window:      window,
		banDuration: banDuration,
	}
	// Start cleanup goroutine
	go limiter.cleanup()
	return limiter
}

// RecordFailedAttempt records a failed login attempt for the given IP.
// Returns true if the IP is now banned.
func (l *LoginLimiter) RecordFailedAttempt(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, exists := l.attempts[ip]

	if !exists {
		l.attempts[ip] = &ipAttempts{
			count:    1,
			firstTry: now,
		}
		return false
	}

	// If already banned, check if ban expired
	if entry.banned {
		if now.Sub(entry.bannedAt) < l.banDuration {
			return true // Still banned
		}
		// Ban expired, reset
		entry.banned = false
		entry.notifiedOfBan = false
		entry.count = 1
		entry.firstTry = now
		return false
	}

	// Check if window expired, reset if so
	if now.Sub(entry.firstTry) > l.window {
		entry.count = 1
		entry.firstTry = now
		return false
	}

	// Increment attempt count
	entry.count++

	// Ban if exceeded max attempts
	if entry.count >= l.maxAttempts {
		entry.banned = true
		entry.bannedAt = now
		return true
	}

	return false
}

// IsBanned checks if an IP is currently banned.
// Returns (isBanned, hasBeenNotified).
func (l *LoginLimiter) IsBanned(ip string) (bool, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	entry, exists := l.attempts[ip]
	if !exists || !entry.banned {
		return false, false
	}

	// Check if ban expired
	if time.Since(entry.bannedAt) >= l.banDuration {
		return false, false
	}

	return true, entry.notifiedOfBan
}

// MarkNotified marks that the IP has been notified of the ban.
// After this, subsequent requests should get 418 "I'm a teapot".
func (l *LoginLimiter) MarkNotified(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, exists := l.attempts[ip]; exists && entry.banned {
		entry.notifiedOfBan = true
	}
}

// RecordSuccess clears failed attempts for an IP on successful login.
func (l *LoginLimiter) RecordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, ip)
}

// cleanup periodically removes expired entries to prevent memory leaks.
func (l *LoginLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for ip, entry := range l.attempts {
			// Remove if:
			// - Not banned and window expired
			// - Banned and ban expired + some grace period
			if !entry.banned && now.Sub(entry.firstTry) > l.window*2 {
				delete(l.attempts, ip)
			} else if entry.banned && now.Sub(entry.bannedAt) > l.banDuration*2 {
				delete(l.attempts, ip)
			}
		}
		l.mu.Unlock()
	}
}

// GetClientIP extracts the client IP from the request.
// Checks X-Forwarded-For and X-Real-IP headers, falls back to RemoteAddr.
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For (most common with reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		for i, c := range xff {
			if c == ',' || c == ' ' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fallback to RemoteAddr (strip port)
	ip := r.RemoteAddr
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == ':' {
			return ip[:i]
		}
	}
	return ip
}
