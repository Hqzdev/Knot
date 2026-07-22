package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestVerifierAcceptsDeviceBoundAccessToken(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	verifier, err := newVerifier([]byte("test-secret"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token := signedToken(t, []byte("test-secret"), tokenHeader{Algorithm: "HS256", Type: "JWT"}, tokenClaims{
		Subject:  "user-1",
		DeviceID: "device-1",
		Expiry:   now.Add(time.Minute).Unix(),
	})
	identity, err := verifier.VerifyAuthorization("Bearer " + token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "user-1" || identity.DeviceID != "device-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	identity, err = verifier.VerifyToken(token)
	if err != nil || identity.UserID != "user-1" || identity.DeviceID != "device-1" {
		t.Fatalf("unexpected raw token identity: %#v %v", identity, err)
	}
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	verifier, err := newVerifier([]byte("test-secret"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"missing bearer": "",
		"missing device": "Bearer " + signedToken(t, []byte("test-secret"), tokenHeader{Algorithm: "HS256", Type: "JWT"}, tokenClaims{Subject: "user-1", Expiry: now.Add(time.Minute).Unix()}),
		"expired":        "Bearer " + signedToken(t, []byte("test-secret"), tokenHeader{Algorithm: "HS256", Type: "JWT"}, tokenClaims{Subject: "user-1", DeviceID: "device-1", Expiry: now.Unix()}),
		"wrong algorithm": "Bearer " + signedToken(t, []byte("test-secret"), tokenHeader{Algorithm: "none", Type: "JWT"}, tokenClaims{
			Subject: "user-1", DeviceID: "device-1", Expiry: now.Add(time.Minute).Unix(),
		}),
		"wrong signature": "Bearer " + signedToken(t, []byte("other-secret"), tokenHeader{Algorithm: "HS256", Type: "JWT"}, tokenClaims{
			Subject: "user-1", DeviceID: "device-1", Expiry: now.Add(time.Minute).Unix(),
		}),
	}
	for name, authorization := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.VerifyAuthorization(authorization); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("expected unauthorized, got %v", err)
			}
		})
	}
}

func signedToken(t *testing.T, secret []byte, header tokenHeader, claims tokenClaims) string {
	t.Helper()
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
