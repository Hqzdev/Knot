package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestVerifierAcceptsBoundUnexpiredToken(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_700_000_000, 0)
	verifier, err := newVerifier(secret, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token := signToken(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user-1","device_id":"device-1","exp":1700000060}`)
	identity, err := verifier.VerifyAuthorization("Bearer " + token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "user-1" || identity.DeviceID != "device-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestVerifierRejectsMalformedTokens(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_700_000_000, 0)
	verifier, err := newVerifier(secret, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	tests := []string{
		"",
		"bearer token",
		"Bearer token",
		"Bearer  token",
		"Bearer " + signToken(secret, `{"alg":"none","typ":"JWT"}`, `{"sub":"user-1","device_id":"device-1","exp":1700000060}`),
		"Bearer " + signToken(secret, `{"alg":"HS256","typ":"JWT","kid":"unexpected"}`, `{"sub":"user-1","device_id":"device-1","exp":1700000060}`),
		"Bearer " + signToken(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user-1","device_id":"device-1","exp":1699999999}`),
		"Bearer " + signToken(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user-1","device_id":"","exp":1700000060}`),
		"Bearer " + signToken([]byte("abcdef0123456789abcdef0123456789"), `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user-1","device_id":"device-1","exp":1700000060}`),
	}
	for _, value := range tests {
		if _, err := verifier.VerifyAuthorization(value); err == nil {
			t.Fatalf("expected rejection for %q", value)
		}
	}
}

func TestVerifierRequiresStrongSecret(t *testing.T) {
	if _, err := NewVerifier([]byte("short")); err == nil {
		t.Fatal("expected configuration rejection")
	}
}

func signToken(secret []byte, header string, payload string) string {
	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	unsigned := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
