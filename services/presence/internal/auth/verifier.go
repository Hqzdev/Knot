package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const maxTokenBytes = 4096

var ErrUnauthorized = errors.New("unauthorized")

type Identity struct {
	UserID   string
	DeviceID string
}

type Verifier struct {
	secret []byte
	now    func() time.Time
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type tokenClaims struct {
	Subject  string `json:"sub"`
	DeviceID string `json:"device_id"`
	Expiry   int64  `json:"exp"`
}

func NewVerifier(secret []byte) (*Verifier, error) {
	return newVerifier(secret, time.Now)
}

func newVerifier(secret []byte, now func() time.Time) (*Verifier, error) {
	if len(secret) == 0 || now == nil {
		return nil, errors.New("invalid verifier configuration")
	}
	return &Verifier{secret: append([]byte(nil), secret...), now: now}, nil
}

func (verifier *Verifier) VerifyAuthorization(value string) (Identity, error) {
	if !strings.HasPrefix(value, "Bearer ") {
		return Identity{}, ErrUnauthorized
	}
	return verifier.VerifyToken(strings.TrimPrefix(value, "Bearer "))
}

func (verifier *Verifier) VerifyToken(token string) (Identity, error) {
	if token == "" || len(token) > maxTokenBytes {
		return Identity{}, ErrUnauthorized
	}
	return verifier.verify(token)
}

func (verifier *Verifier) verify(token string) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrUnauthorized
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	var header tokenHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return Identity{}, ErrUnauthorized
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, verifier.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return Identity{}, ErrUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Subject == "" || claims.DeviceID == "" || claims.Expiry <= verifier.now().Unix() {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.Subject, DeviceID: claims.DeviceID}, nil
}
