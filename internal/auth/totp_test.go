package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

// rfc4226Secret is the ASCII secret "12345678901234567890" used by the test
// vectors in RFC 4226 Appendix D and RFC 6238 Appendix B.
var rfc4226Secret = []byte("12345678901234567890")

func TestGenerateHOTP_RFC4226Vectors(t *testing.T) {
	// RFC 4226 Appendix D: 6-digit HOTP values for counters 0..9.
	want := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}
	for counter, exp := range want {
		got := generateHOTP(rfc4226Secret, uint64(counter), 6)
		if got != exp {
			t.Errorf("generateHOTP counter=%d = %q, want %q", counter, got, exp)
		}
	}
}

func TestValidateTOTP(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rfc4226Secret)

	// At unix time 59s the 30s counter is 1, whose 6-digit HOTP is 287082.
	at := time.Unix(59, 0)

	if !validateTOTPAt(secret, "287082", at) {
		t.Error("expected current-period code to validate")
	}
	// Lowercase + padded secret should still decode.
	if !validateTOTPAt(base32.StdEncoding.EncodeToString(rfc4226Secret), "287082", at) {
		t.Error("expected padded secret to validate")
	}
	// Previous period (counter 0 -> 755224) and next period (counter 2 ->
	// 359152) are within the ±1 skew window.
	if !validateTOTPAt(secret, "755224", at) {
		t.Error("expected previous-period code to validate within skew window")
	}
	if !validateTOTPAt(secret, "359152", at) {
		t.Error("expected next-period code to validate within skew window")
	}
	// A code three periods away (counter 3 -> 969429) is outside the window.
	if validateTOTPAt(secret, "969429", at) {
		t.Error("code outside skew window should not validate")
	}
	// Wrong code, wrong length, and empty secret all fail closed.
	if validateTOTPAt(secret, "000000", at) {
		t.Error("wrong code validated")
	}
	if validateTOTPAt(secret, "12345", at) {
		t.Error("wrong-length code validated")
	}
	if validateTOTPAt("", "287082", at) {
		t.Error("empty secret validated")
	}
	if validateTOTPAt("not-base32!!", "287082", at) {
		t.Error("malformed secret validated")
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	s, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if _, err := decodeBase32Secret(s); err != nil {
		t.Errorf("generated secret does not decode: %v", err)
	}
	// A freshly generated secret should produce a code that validates now.
	code := generateHOTP(mustDecode(t, s), uint64(time.Now().Unix())/uint64(totpPeriod.Seconds()), totpDigits)
	if !ValidateTOTP(s, code) {
		t.Error("code from generated secret did not validate")
	}
}

func mustDecode(t *testing.T, secret string) []byte {
	t.Helper()
	b, err := decodeBase32Secret(secret)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b
}
