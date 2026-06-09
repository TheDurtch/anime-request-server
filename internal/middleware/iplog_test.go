package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogRequestIPs(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(old)

	called := false
	h := LogRequestIPs("X-Forwarded-For")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/requests/new", nil)
	req.RemoteAddr = "127.0.0.1:60144"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 70.41.3.18")
	req.Header.Set("CF-Connecting-IP", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("middleware did not call the next handler")
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v; raw=%s", err, buf.String())
	}

	checks := map[string]string{
		"remote_addr":      "127.0.0.1:60144",
		"real_ip":          "203.0.113.7", // first X-Forwarded-For entry (the trusted header)
		"real_ip_header":   "X-Forwarded-For",
		"x_forwarded_for":  "203.0.113.7, 70.41.3.18",
		"cf_connecting_ip": "203.0.113.7",
		"x_real_ip":        "", // absent headers are logged as empty
	}
	for key, want := range checks {
		if got, _ := rec[key].(string); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLogRequestIPs_NoTrustedHeader(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(old)

	// Empty realIPHeader: forwarding headers are ignored and real_ip falls back
	// to the RemoteAddr host (no port).
	h := LogRequestIPs("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:60144"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v; raw=%s", err, buf.String())
	}
	if got, _ := rec["real_ip"].(string); got != "127.0.0.1" {
		t.Errorf("real_ip = %q, want 127.0.0.1 (RemoteAddr host, headers untrusted)", got)
	}
}
