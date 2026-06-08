package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// HashPassword hashes a password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a password against a bcrypt hash using constant-time comparison.
func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// dummyPasswordHash is a precomputed bcrypt hash used to equalize response time
// when authenticating a username that does not exist, mitigating username
// enumeration via timing. Generated once at startup at the same cost as real
// hashes; we fail fast if bcrypt is unusable so the mitigation can't silently
// degrade to a fast nil-hash comparison.
var dummyPasswordHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("password-timing-equalizer"), bcryptCost)
	if err != nil {
		panic("auth: generating dummy password hash: " + err.Error())
	}
	dummyPasswordHash = h
}

// CheckPasswordDummy performs a bcrypt comparison against a fixed dummy hash and
// discards the result. Call it on the no-such-user path so that login timing
// matches the path where a real user's password is verified.
func CheckPasswordDummy(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
}

// GenerateSessionToken creates a cryptographically random session token.
func GenerateSessionToken() (token string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating random bytes: %w", err)
	}
	token = hex.EncodeToString(b)
	hash = HashToken(token)
	return token, hash, nil
}

// HashToken creates a SHA-256 hash of a session token for storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// GenerateInviteCode creates a random invite code.
func GenerateInviteCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating invite code: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ConstantTimeCompare performs a constant-time string comparison.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
