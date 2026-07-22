package provider

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

type recordingHTTPClient struct {
	request  *http.Request
	payload  []byte
	response *http.Response
	err      error
}

func (client *recordingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.request = request
	if request.Body != nil {
		client.payload, _ = io.ReadAll(request.Body)
	}
	return client.response, client.err
}

func TestAPNSProviderSendsBackgroundMetadataOnlyNotification(t *testing.T) {
	privateKey, encodedKey := testAPNSPrivateKey(t)
	client := &recordingHTTPClient{response: response(http.StatusOK, "")}
	now := time.Unix(1_700_000_000, 0)
	provider, err := newAPNSProvider(APNSConfig{
		KeyID:      "A1B2C3D4E5",
		TeamID:     "F6G7H8J9K0",
		Topic:      "com.knot.messenger",
		PrivateKey: encodedKey,
		Timeout:    5 * time.Second,
	}, "https://api.push.test", client, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	target := subscription.Subscription{Channel: subscription.ChannelAPNS, APNSToken: strings.Repeat("a", 64)}
	if err := provider.Send(context.Background(), target, NewMessageAvailableNotification()); err != nil {
		t.Fatal(err)
	}
	if client.request.URL.String() != "https://api.push.test/3/device/"+target.APNSToken {
		t.Fatalf("unexpected request URL: %s", client.request.URL)
	}
	if client.request.Header.Get("apns-push-type") != "background" || client.request.Header.Get("apns-priority") != "5" {
		t.Fatalf("unexpected APNs headers: %#v", client.request.Header)
	}
	expectedPayload := `{"aps":{"content-available":1},"type":"message_available"}`
	if string(client.payload) != expectedPayload {
		t.Fatalf("unexpected payload: %s", client.payload)
	}
	if bytes.Contains(client.payload, []byte("ciphertext")) || bytes.Contains(client.payload, []byte("key")) {
		t.Fatalf("sensitive data leaked into payload: %s", client.payload)
	}
	verifyAPNSToken(t, strings.TrimPrefix(client.request.Header.Get("Authorization"), "bearer "), privateKey, now)
}

func TestAPNSProviderClassifiesInvalidAndTransientResponses(t *testing.T) {
	_, encodedKey := testAPNSPrivateKey(t)
	tests := []struct {
		status  int
		body    string
		invalid bool
	}{
		{status: http.StatusGone, body: `{"reason":"Unregistered"}`, invalid: true},
		{status: http.StatusBadRequest, body: `{"reason":"BadDeviceToken"}`, invalid: true},
		{status: http.StatusTooManyRequests, body: `{"reason":"TooManyRequests"}`, invalid: false},
	}
	for _, test := range tests {
		client := &recordingHTTPClient{response: response(test.status, test.body)}
		provider, err := newAPNSProvider(APNSConfig{
			KeyID:      "A1B2C3D4E5",
			TeamID:     "F6G7H8J9K0",
			Topic:      "com.knot.messenger",
			PrivateKey: encodedKey,
			Timeout:    5 * time.Second,
		}, "https://api.push.test", client, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		target := subscription.Subscription{Channel: subscription.ChannelAPNS, APNSToken: strings.Repeat("a", 64)}
		err = provider.Send(context.Background(), target, NewMessageAvailableNotification())
		if errors.Is(err, ErrSubscriptionInvalid) != test.invalid {
			t.Fatalf("unexpected classification for %d: %v", test.status, err)
		}
		if err == nil {
			t.Fatalf("expected rejection for %d", test.status)
		}
	}
}

func testAPNSPrivateKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func verifyAPNSToken(t *testing.T, token string, privateKey *ecdsa.PrivateKey, now time.Time) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid APNs token: %s", token)
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]string
	var claims map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "ES256" || header["kid"] != "A1B2C3D4E5" || claims["iss"] != "F6G7H8J9K0" || int64(claims["iat"].(float64)) != now.Unix() {
		t.Fatalf("unexpected APNs JWT: %#v %#v", header, claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("invalid APNs signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&privateKey.PublicKey, digest[:], newBigInt(signature[:32]), newBigInt(signature[32:])) {
		t.Fatal("APNs signature verification failed")
	}
}

func newBigInt(value []byte) *big.Int {
	return new(big.Int).SetBytes(value)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
