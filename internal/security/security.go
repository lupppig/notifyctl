package security

import (
	"crypto/sha256"
	"fmt"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const (
	KeyPrefix = "nc_"
	Alphabet  = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// GenerateKey creates a secure random API key using NanoID with a prefix.
func GenerateKey() (string, error) {
	id, err := gonanoid.Generate(Alphabet, 32)
	if err != nil {
		return "", err
	}
	return KeyPrefix + id, nil
}

// HashKey returns a SHA-256 hash of the provided key.
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash)
}
