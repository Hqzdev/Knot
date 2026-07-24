package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Mode string

const (
	PasswordMode     Mode = "password"
	GuestMode        Mode = "guest"
	ImpersonatedMode Mode = "impersonated"
)

type Claims struct {
	UserID    string `json:"sub"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
	Mode      Mode   `json:"mode"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type Manager struct {
	secret []byte
	now    func() time.Time
}

func NewManager(secret []byte) (*Manager, error) {
	if len(secret) < 32 {
		return nil, errors.New("session secret must contain at least 32 bytes")
	}
	return &Manager{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (manager *Manager) Issue(userID string, username string, sessionID string, mode Mode) (string, error) {
	if userID == "" || username == "" || sessionID == "" || !mode.Valid() {
		return "", errors.New("invalid session identity")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := manager.now().UTC()
	payload, err := json.Marshal(Claims{
		UserID:    userID,
		Username:  username,
		SessionID: sessionID,
		Mode:      mode,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := header + "." + encodedPayload
	return unsigned + "." + manager.signature(unsigned), nil
}

func (manager *Manager) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || !hmac.Equal([]byte(parts[2]), []byte(manager.signature(parts[0]+"."+parts[1]))) {
		return Claims{}, errors.New("invalid session")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid session")
	}
	var claims Claims
	now := manager.now().UTC().Unix()
	if json.Unmarshal(payload, &claims) != nil || claims.UserID == "" || claims.Username == "" || claims.SessionID == "" || !claims.Mode.Valid() || claims.IssuedAt <= 0 || claims.ExpiresAt <= now || claims.IssuedAt > now+60 || claims.ExpiresAt-claims.IssuedAt > int64((16*time.Minute)/time.Second) {
		return Claims{}, errors.New("invalid session")
	}
	return claims, nil
}

func (manager *Manager) signature(value string) string {
	mac := hmac.New(sha256.New, manager.secret)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (mode Mode) Valid() bool {
	return mode == PasswordMode || mode == GuestMode || mode == ImpersonatedMode
}

func NewID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func NewRefreshToken() (string, []byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(value)
	return base64.RawURLEncoding.EncodeToString(value), append([]byte(nil), digest[:]...), nil
}

func RefreshTokenHash(value string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	digest := sha256.Sum256(decoded)
	return append([]byte(nil), digest[:]...), true
}

func Bearer(value string) string {
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}
