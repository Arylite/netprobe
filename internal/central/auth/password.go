package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordLength and MaxPasswordLength bound what is accepted: the lower
	// bound against guessing, the upper against a request that makes the
	// server hash megabytes.
	MinPasswordLength = 12
	MaxPasswordLength = 128

	saltLength = 16
	keyLength  = 32
	maxMemory  = 1 << 20 // KiB: refuse a stored hash that asks for more than 1 GiB
)

type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

// hashParams are the argon2id costs of new hashes. They are encoded in each
// hash, so raising them later does not break the existing ones.
var hashParams = argonParams{memory: 64 * 1024, time: 2, threads: 2}

// ValidatePassword reports why a password is refused.
func ValidatePassword(username, password string) error {
	switch {
	case len(password) < MinPasswordLength:
		return fmt.Errorf("the password must have at least %d characters", MinPasswordLength)
	case len(password) > MaxPasswordLength:
		return fmt.Errorf("the password must have at most %d characters", MaxPasswordLength)
	case strings.ContainsRune(password, 0):
		return errors.New("the password must not contain a null character")
	case strings.EqualFold(password, username):
		return errors.New("the password must differ from the username")
	}
	return nil
}

// HashPassword returns an encoded argon2id hash with a random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	p := hashParams
	key := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword compares a password with an encoded hash in constant time. An
// error means the hash itself is unusable, not that the password is wrong.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false, errors.New("malformed argon2 parameters")
	}
	if p.memory == 0 || p.memory > maxMemory || p.time == 0 || p.threads == 0 {
		return false, errors.New("argon2 parameters out of range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("malformed salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("malformed hash")
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// SpendHashingTime does the work of a verification that cannot succeed, so that
// answering for an unknown user takes as long as for a known one.
func SpendHashingTime(password string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("unused-placeholder") })
	_, _ = VerifyPassword(password, dummyHash)
}
