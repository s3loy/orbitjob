package account

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Parameters are stored with each hash so future upgrades can retain compatibility.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 15 || len(password) > 1024 {
		return "", fmt.Errorf("%w: password must contain at least 15 characters and at most 1024 bytes", ErrInvalid)
	}
	if weakPassword(password) {
		return "", fmt.Errorf("%w: choose a less common password", ErrInvalid)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(password, encoded string) error {
	if len(password) > 1024 {
		return ErrUnauthenticated
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return ErrUnauthenticated
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || memory < 8192 || memory > 65536 || iterations < 1 || iterations > 5 || parallelism < 1 || parallelism > 4 {
		return ErrUnauthenticated
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return ErrUnauthenticated
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return ErrUnauthenticated
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, 32)
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrUnauthenticated
	}
	return nil
}

// The local blocklist avoids transmitting passwords to a third party. It is
// intentionally bounded and supplements length rather than imposing character
// classes on passphrases.
func weakPassword(password string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(password), " "))
	switch normalized {
	case "passwordpassword", "passwordpasswordpassword", "password123456789", "password1234567890", "123456789012345", "1234567890123456", "12345678901234567890", "123456789abcdef", "qwertyuiopasdfgh", "qwertyuiopasdfghjkl", "letmeinletmeinletmein", "iloveyouiloveyou", "correct horse battery staple", "correcthorsebatterystaple", "changemechangeme", "administrator123", "welcome123456789":
		return true
	}
	runes := []rune(normalized)
	if len(runes) == 0 {
		return true
	}
	for period := 1; period <= 4 && period < len(runes); period++ {
		same := true
		for i := period; i < len(runes); i++ {
			if runes[i] != runes[i%period] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}
