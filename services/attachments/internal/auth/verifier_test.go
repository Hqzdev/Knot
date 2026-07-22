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

func TestVerifierRequiresSignedDeviceIdentity(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	verifier, err := newVerifier([]byte("secret"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	valid := signToken(t, []byte("secret"), tokenClaims{Subject: "user", DeviceID: "device", Expiry: now.Add(time.Minute).Unix()})
	identity, err := verifier.VerifyAuthorization("Bearer " + valid)
	if err != nil {
		t.Fatal(err)
	}
	if identity != (Identity{UserID: "user", DeviceID: "device"}) {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	invalid := []string{
		"",
		"Bearer " + signToken(t, []byte("secret"), tokenClaims{Subject: "user", Expiry: now.Add(time.Minute).Unix()}),
		"Bearer " + signToken(t, []byte("secret"), tokenClaims{Subject: "user", DeviceID: "device", Expiry: now.Unix()}),
		"Bearer " + signToken(t, []byte("wrong"), tokenClaims{Subject: "user", DeviceID: "device", Expiry: now.Add(time.Minute).Unix()}),
	}
	for _, value := range invalid {
		if _, err := verifier.VerifyAuthorization(value); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("expected unauthorized for %q, got %v", value, err)
		}
	}
}

func signToken(t *testing.T, secret []byte, claims tokenClaims) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
