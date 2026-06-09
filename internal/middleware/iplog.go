package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/TheDurtch/anime-request-server/internal/ratelimit"
)

// ipHeaders are the request headers commonly used by reverse proxies and CDNs
// to carry the originating client IP. All are logged for visibility; only the
// one named by REAL_IP_HEADER is actually trusted (see ratelimit.GetClientIP).
var ipHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
	"CF-Connecting-IP",
	"True-Client-IP",
	"X-Client-IP",
	"Forwarded",
}

// LogRequestIPs returns middleware that logs, for every request, the TCP peer
// address (RemoteAddr), each known IP-bearing header, and the client IP the app
// actually derives via ratelimit.GetClientIP using realIPHeader. This makes it
// possible to verify the trusted-header configuration behind a reverse proxy —
// e.g. why a request appears to come from 127.0.0.1 (the proxy) versus the real
// client. It logs client IPs (PII) on every request, so callers should gate it
// behind configuration (LOG_REQUEST_IPS).
func LogRequestIPs(realIPHeader string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"real_ip", ratelimit.GetClientIP(r, realIPHeader),
				"real_ip_header", realIPHeader,
			}
			for _, h := range ipHeaders {
				// Values (not Get) so repeated headers are captured in full.
				attrs = append(attrs, headerKey(h), strings.Join(r.Header.Values(h), ", "))
			}
			slog.Info("request ip", attrs...)
			next.ServeHTTP(w, r)
		})
	}
}

// headerKey converts a header name to a snake_case log key, e.g.
// "X-Forwarded-For" -> "x_forwarded_for".
func headerKey(h string) string {
	return strings.ReplaceAll(strings.ToLower(h), "-", "_")
}
