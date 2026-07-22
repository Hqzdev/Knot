package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTokenBytes    = 4096
	maxIdentityBytes = 128
)

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
	if len(secret) < 32 || now == nil {
		return nil, errors.New("invalid verifier configuration")
	}
	return &Verifier{secret: append([]byte(nil), secret...), now: now}, nil
}

func (verifier *Verifier) VerifyAuthorization(value string) (Identity, error) {
	if !strings.HasPrefix(value, "Bearer ") || strings.Count(value, " ") != 1 {
		return Identity{}, ErrUnauthorized
	}
	token := strings.TrimPrefix(value, "Bearer ")
	if token == "" || len(token) > maxTokenBytes {
		return Identity{}, ErrUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrUnauthorized
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(providedSignature) != sha256.Size {
		return Identity{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, verifier.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return Identity{}, ErrUnauthorized
	}
	var header tokenHeader
	if err := decodePart(parts[0], &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return Identity{}, ErrUnauthorized
	}
	var claims tokenClaims
	if err := decodePart(parts[1], &claims); err != nil || !validIdentity(claims.Subject) || !validIdentity(claims.DeviceID) || claims.Expiry <= verifier.now().Unix() {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.Subject, DeviceID: claims.DeviceID}, nil
}

func decodePart(encoded string, destination any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid token JSON")
	}
	return nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value
}
