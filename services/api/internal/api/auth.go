package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var errUnauthorized = errors.New("unauthorized")

var unavailableUserPasswordHash = encodePasswordHash(
	[]byte("unavailable-account-password"),
	[]byte("knot-auth-dummy!"),
)

const (
	refreshTokenBytes    = 32
	refreshTokenLifetime = 30 * 24 * time.Hour
)

type claims struct {
	Subject  string `json:"sub"`
	DeviceID string `json:"device_id"`
	IssuedAt int64  `json:"iat"`
	Expiry   int64  `json:"exp"`
	JWTID    string `json:"jti"`
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type tokenManager struct {
	secret []byte
}

type refreshCredential struct {
	value     string
	hash      []byte
	expiresAt time.Time
}

func newTokenManager(secret []byte) tokenManager {
	return tokenManager{secret: append([]byte(nil), secret...)}
}

func (manager tokenManager) issue(userID string, deviceID string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(claims{
		Subject:  userID,
		DeviceID: deviceID,
		IssuedAt: now.Unix(),
		Expiry:   now.Add(15 * time.Minute).Unix(),
		JWTID:    base64.RawURLEncoding.EncodeToString(identifier),
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := header + "." + encodedPayload
	mac := hmac.New(sha256.New, manager.secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (manager tokenManager) verify(token string) (claims, error) {
	if len(token) == 0 || len(token) > 4096 {
		return claims{}, errUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims{}, errUnauthorized
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(providedSignature) != sha256.Size {
		return claims{}, errUnauthorized
	}
	headerValue, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims{}, errUnauthorized
	}
	var header tokenHeader
	if err := json.Unmarshal(headerValue, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return claims{}, errUnauthorized
	}
	mac := hmac.New(sha256.New, manager.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return claims{}, errUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, errUnauthorized
	}
	var parsed claims
	now := time.Now().UTC().Unix()
	if err := json.Unmarshal(payload, &parsed); err != nil || parsed.Subject == "" || parsed.DeviceID == "" || parsed.JWTID == "" || parsed.IssuedAt <= 0 || parsed.IssuedAt > now+60 || parsed.Expiry <= now || parsed.Expiry-parsed.IssuedAt > int64((16*time.Minute)/time.Second) {
		return claims{}, errUnauthorized
	}
	return parsed, nil
}

func newRefreshCredential() (refreshCredential, error) {
	value, hash, err := newOpaqueCredential()
	if err != nil {
		return refreshCredential{}, err
	}
	return refreshCredential{
		value:     value,
		hash:      hash,
		expiresAt: time.Now().UTC().Add(refreshTokenLifetime),
	}, nil
}

func newOpaqueCredential() (string, []byte, error) {
	value := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(value)
	return base64.RawURLEncoding.EncodeToString(value), append([]byte(nil), digest[:]...), nil
}

func refreshTokenHash(value string) ([]byte, bool) {
	return opaqueCredentialHash(value)
}

func opaqueCredentialHash(value string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != refreshTokenBytes {
		return nil, false
	}
	digest := sha256.Sum256(decoded)
	return append([]byte(nil), digest[:]...), true
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return encodePasswordHash([]byte(password), salt), nil
}

func verifyPassword(encoded string, password string) bool {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return false
	}
	salt, saltError := base64.RawStdEncoding.DecodeString(parts[0])
	expectedHash, hashError := base64.RawStdEncoding.DecodeString(parts[1])
	if saltError != nil || hashError != nil || len(salt) != 16 || len(expectedHash) != 32 {
		return false
	}
	actualHash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
}

func encodePasswordHash(password []byte, salt []byte) string {
	hash := argon2.IDKey(password, salt, 3, 64*1024, 4, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + "." + base64.RawStdEncoding.EncodeToString(hash)
}
