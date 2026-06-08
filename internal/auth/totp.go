package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters. These match the defaults used by common authenticator apps
// (Google Authenticator, Authy): SHA-1, 6 digits, 30-second period.
const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSkew allows the code from one period on either side of "now" to
	// account for clock drift between server and client.
	totpSkew = 1
)

// ValidateTOTP reports whether code is a valid time-based one-time password
// (RFC 6238) for the given base32-encoded secret at the current time. It checks
// the current period plus one period on either side to tolerate clock drift,
// and uses a constant-time comparison.
//
// It fails closed: an empty secret, malformed base32, or wrong-length code all
// return false. This is the verification half of TOTP; secret generation and
// the enrollment flow (GenerateTOTPSecret / TOTPProvisioningURI) are provided
// as a base but are not yet wired into any handler.
func ValidateTOTP(secret, code string) bool {
	return validateTOTPAt(secret, code, time.Now())
}

// validateTOTPAt is ValidateTOTP with an injectable time, for testing.
func validateTOTPAt(secret, code string, t time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	key, err := decodeBase32Secret(secret)
	if err != nil || len(key) == 0 {
		return false
	}

	period := uint64(totpPeriod.Seconds())
	counter := uint64(t.Unix()) / period

	for skew := -totpSkew; skew <= totpSkew; skew++ {
		c := counter
		if skew < 0 {
			c -= uint64(-skew)
		} else {
			c += uint64(skew)
		}
		expected := generateHOTP(key, c, totpDigits)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// generateHOTP computes the HOTP value (RFC 4226) for the given key and counter,
// zero-padded to digits.
func generateHOTP(key []byte, counter uint64, digits int) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3).
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod)
}

// decodeBase32Secret decodes a base32 secret, tolerating lowercase, spaces, and
// missing padding (all common in QR-provisioned secrets).
func decodeBase32Secret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.TrimSpace(secret))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimRight(s, "=")
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
}

// GenerateTOTPSecret returns a new random base32-encoded TOTP secret (160 bits),
// suitable for provisioning an authenticator app. Provided as a base for a
// future enrollment flow; not yet wired into any handler.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// TOTPProvisioningURI builds an otpauth:// URI for QR-code enrollment in an
// authenticator app. Provided as a base for a future enrollment flow.
func TOTPProvisioningURI(secret, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
