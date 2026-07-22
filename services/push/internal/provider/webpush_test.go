package provider

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

func TestWebPushProviderEncryptsMetadataNotification(t *testing.T) {
	provider, client := testWebPushProvider(t, http.StatusCreated)
	target := testWebSubscription(t)
	if err := provider.Send(context.Background(), target, NewMessageAvailableNotification()); err != nil {
		t.Fatal(err)
	}
	if client.request.Header.Get("Content-Encoding") != "aes128gcm" || client.request.Header.Get("Topic") != MessageAvailable {
		t.Fatalf("unexpected Web Push headers: %#v", client.request.Header)
	}
	if bytes.Contains(client.payload, []byte(MessageAvailable)) {
		t.Fatalf("Web Push payload was not encrypted: %q", client.payload)
	}
	if !strings.HasPrefix(client.request.Header.Get("Authorization"), "vapid ") {
		t.Fatalf("missing VAPID authorization: %s", client.request.Header.Get("Authorization"))
	}
}

func TestWebPushProviderClassifiesExpiredSubscription(t *testing.T) {
	provider, _ := testWebPushProvider(t, http.StatusGone)
	err := provider.Send(context.Background(), testWebSubscription(t), NewMessageAvailableNotification())
	if !errors.Is(err, ErrSubscriptionInvalid) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublicIPClassification(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8":              true,
		"1.1.1.1":              true,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"169.254.1.1":          false,
		"::1":                  false,
		"2001:4860:4860::8888": true,
	}
	for value, expected := range tests {
		if actual := publicIP(net.ParseIP(value)); actual != expected {
			t.Fatalf("unexpected classification for %s: %t", value, actual)
		}
	}
}

func testWebPushProvider(t *testing.T, status int) (*WebPushProvider, *recordingHTTPClient) {
	t.Helper()
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingHTTPClient{response: response(status, "")}
	provider, err := newWebPushProvider(WebPushConfig{
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		Subject:         "mailto:push@example.test",
		Timeout:         5 * time.Second,
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	return provider, client
}

func testWebSubscription(t *testing.T) subscription.Subscription {
	t.Helper()
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return subscription.Subscription{
		Channel:     subscription.ChannelWeb,
		WebEndpoint: "https://push.example.test/subscription",
		WebKeys: subscription.WebKeys{
			P256DH: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)),
			Auth:   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
		},
	}
}
