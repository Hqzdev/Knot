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
	IssuedAt int64  `json:"iat"`
	Expiry   int64  `json:"exp"`
	JWTID    string `json:"jti"`
}

func NewVerifier(secret []byte) (*Verifier, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must contain at least 32 bytes")
	}
	return &Verifier{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (verifier *Verifier) VerifyAuthorization(value string) (Identity, error) {
	if !strings.HasPrefix(value, "Bearer ") || strings.Count(value, " ") != 1 {
		return Identity{}, ErrUnauthorized
	}
	return verifier.VerifyToken(strings.TrimPrefix(value, "Bearer "))
}

func (verifier *Verifier) VerifyToken(token string) (Identity, error) {
	if token == "" || len(token) > maxTokenBytes {
		return Identity{}, ErrUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrUnauthorized
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(provided) != sha256.Size {
		return Identity{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, verifier.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return Identity{}, ErrUnauthorized
	}
	var header tokenHeader
	if decodePart(parts[0], &header) != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return Identity{}, ErrUnauthorized
	}
	var claims tokenClaims
	now := verifier.now().UTC().Unix()
	if decodePart(parts[1], &claims) != nil || !validIdentity(claims.Subject) || !validIdentity(claims.DeviceID) || !validIdentity(claims.JWTID) || claims.IssuedAt <= 0 || claims.IssuedAt > now+60 || claims.Expiry <= now || claims.Expiry-claims.IssuedAt > int64((16*time.Minute)/time.Second) {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.Subject, DeviceID: claims.DeviceID}, nil
}

func decodePart(value string, destination any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrUnauthorized
	}
	return nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityBytes && utf8.ValidString(value) && strings.TrimSpace(value) == value
}
