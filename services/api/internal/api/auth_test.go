package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestTokenManagerRequiresHS256JWTHeader(t *testing.T) {
	secret := []byte("12345678901234567890123456789012")
	manager := newTokenManager(secret)
	token, err := manager.issue("user", "device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.verify(token); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[0] = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	parts[2] = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := manager.verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("expected non-HS256 token rejection")
	}
}
