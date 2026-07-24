package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/argon2"
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + "." + base64.RawStdEncoding.EncodeToString(hash), nil
}

func verifyPassword(encoded string, password string) bool {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return false
	}
	salt, saltError := base64.RawStdEncoding.DecodeString(parts[0])
	expected, hashError := base64.RawStdEncoding.DecodeString(parts[1])
	if saltError != nil || hashError != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
