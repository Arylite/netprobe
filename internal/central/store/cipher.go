package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	sealedPrefix = "enc1:"
	keySize      = 32
)

// ErrKeyMissing is returned for a value that is encrypted when no key was given.
var ErrKeyMissing = errors.New("the value is encrypted and no key was given")

// Cipher encrypts what the central must read back: the address and the signing
// secret of a channel. A hash would not do, they are sent. AES-256-GCM, with the
// name of the row as additional data, so a value cannot be moved to another row.
type Cipher struct{ aead cipher.AEAD }

// NewCipher builds a cipher from a 32 byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("the key must be %d bytes, not %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// GenerateKey returns a new random key, as base64.
func GenerateKey() (string, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// ParseKey reads a key written as base64 or as hexadecimal.
func ParseKey(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if key, err := base64.StdEncoding.DecodeString(text); err == nil && len(key) == keySize {
		return key, nil
	}
	if key, err := hex.DecodeString(text); err == nil && len(key) == keySize {
		return key, nil
	}
	return nil, fmt.Errorf("the key must be %d bytes in base64 or in hexadecimal: 'netprobe-central secret-key' makes one", keySize)
}

func (c *Cipher) seal(plain, context string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(context))
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) open(value, context string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, sealedPrefix))
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", errors.New("the encrypted value is malformed")
	}
	nonce, body := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, body, []byte(context))
	if err != nil {
		return "", errors.New("the encrypted value does not open: wrong key, or the value was moved")
	}
	return string(plain), nil
}

// sealValue encrypts a value when there is a key. Empty stays empty.
func (s *Store) sealValue(value, context string) (string, error) {
	if s.cipher == nil || value == "" {
		return value, nil
	}
	return s.cipher.seal(value, context)
}

// openValue reads a value back. One written before there was a key is not
// encrypted, and is returned as it is.
func (s *Store) openValue(value, context string) (string, error) {
	if !strings.HasPrefix(value, sealedPrefix) {
		return value, nil
	}
	if s.cipher == nil {
		return "", ErrKeyMissing
	}
	return s.cipher.open(value, context)
}

func channelContext(name, field string) string { return "netprobe/channel/" + name + "/" + field }
