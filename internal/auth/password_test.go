package auth

import (
	"strings"
	"testing"
)

func TestSaltedPasswordCodecPreservesExactPassphrase(t *testing.T) {
	generated, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) < 22 {
		t.Fatal("generated initial password is too short")
	}
	password := " " + generated + " "
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("equal passwords reused the same salt")
	}
	if strings.Contains(first, password) {
		t.Fatal("encoded credential contains plaintext")
	}
	if !VerifyPassword(first, password) {
		t.Fatal("exact password did not round trip")
	}
	if VerifyPassword(first, strings.TrimSpace(password)) {
		t.Fatal("password whitespace was normalized")
	}
}
func TestPasswordValidationAndBoundedStoredFormats(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", 129), string([]byte{0xff}) + strings.Repeat("x", 20), strings.Repeat("x", 12) + "\x00"} {
		if err := ValidatePassword(password); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, hash := range []string{"", "other$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "pbkdf2-sha256$1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "pbkdf2-sha256$1000001$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "pbkdf2-sha256$600000$invalid$invalid", strings.Repeat("a", 257)} {
		if VerifyPassword(hash, "a deliberately invalid password") {
			t.Fatal("invalid stored credential accepted")
		}
	}
	if err := ValidatePassword(strings.Repeat("ä", 128)); err != nil {
		t.Fatal("valid Unicode passphrase rejected")
	}
}
