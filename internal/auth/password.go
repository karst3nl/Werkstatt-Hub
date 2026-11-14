// Package auth handles local password credentials without exposing their hashes
// through domain users or audit events.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const iterations = 600000

var ErrPassword = errors.New("Passwort muss 12 bis 128 Zeichen lang sein")

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) > 512 || utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 128 || strings.ContainsRune(password, 0) {
		return ErrPassword
	}
	return nil
}
func GeneratePassword() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$" + strconv.Itoa(iterations) + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func VerifyPassword(encoded, password string) bool {
	// Bound both the untrusted input and stored work factor before deriving a key.
	if !utf8.ValidString(password) || len(password) > 512 || len(encoded) > 256 {
		return false
	}
	fields := strings.Split(encoded, "$")
	if len(fields) != 4 || fields[0] != "pbkdf2-sha256" {
		return false
	}
	cost, err := strconv.Atoi(fields[1])
	if err != nil || cost < iterations || cost > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(fields[2])
	if err != nil || len(salt) < 16 || len(salt) > 32 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(fields[3])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, cost, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
