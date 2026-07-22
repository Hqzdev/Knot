package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestVerifierAcceptsStrictDeviceBoundJWT(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	verifier.now = func() time.Time { return now }
	token := signedJWT(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user","device_id":"device","iat":1700000000,"exp":1700000060,"jti":"token-id"}`)
	identity, err := verifier.VerifyAuthorization("Bearer " + token)
	if err != nil || identity.UserID != "user" || identity.DeviceID != "device" {
		t.Fatalf("unexpected identity: %#v %v", identity, err)
	}
	invalid := []string{
		signedJWT(secret, `{"alg":"none","typ":"JWT"}`, `{"sub":"user","device_id":"device","iat":1700000000,"exp":1700000060,"jti":"token-id"}`),
		signedJWT(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user","device_id":"device","iat":1699999940,"exp":1700000000,"jti":"token-id"}`),
		signedJWT(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user","device_id":"device","iat":1700000000,"exp":1700000060,"jti":"token-id","role":"admin"}`),
		signedJWT(secret, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"user","iat":1700000000,"exp":1700000060,"jti":"token-id"}`),
	}
	for _, value := range invalid {
		if _, err := verifier.VerifyToken(value); err == nil {
			t.Fatalf("invalid token accepted: %s", value)
		}
	}
	if _, err := verifier.VerifyAuthorization("bearer " + token); err == nil {
		t.Fatal("non-canonical authorization accepted")
	}
}

func signedJWT(secret []byte, header string, claims string) string {
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + base64.RawURLEncoding.EncodeToString([]byte(claims))
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
