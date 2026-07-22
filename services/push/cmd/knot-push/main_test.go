package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/provider"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

func TestProviderDuration(t *testing.T) {
	duration, err := providerDuration("")
	if err != nil || duration != 8*time.Second {
		t.Fatalf("unexpected default duration: %s %v", duration, err)
	}
	if _, err := providerDuration("500ms"); err == nil {
		t.Fatal("expected lower bound rejection")
	}
	if _, err := providerDuration("21s"); err == nil {
		t.Fatal("expected upper bound rejection")
	}
}

func TestDisabledProvidersAreExplicitlyUnavailable(t *testing.T) {
	apns, web, err := newProviders(configuration{providerMode: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sender := range []provider.Sender{apns, web} {
		if err := sender.Send(context.Background(), subscription.Subscription{}, provider.NewMessageAvailableNotification()); !errors.Is(err, provider.ErrProviderUnavailable) {
			t.Fatalf("expected unavailable provider, got %v", err)
		}
	}
}

func TestDisabledProviderConfigurationDoesNotRequireExternalCredentials(t *testing.T) {
	t.Setenv("KNOT_DATABASE_URL", "postgres://database.example/knot")
	t.Setenv("KNOT_JWT_SECRET", "test-jwt-secret-with-at-least-thirty-two-bytes")
	t.Setenv("KNOT_PUSH_INTERNAL_TOKEN", "test-push-token-with-at-least-thirty-two-bytes")
	t.Setenv("KNOT_PUSH_PROVIDER_MODE", "disabled")
	t.Setenv("KNOT_APNS_PRIVATE_KEY_FILE", "")
	t.Setenv("KNOT_WEB_PUSH_VAPID_PUBLIC_KEY", "")
	t.Setenv("KNOT_WEB_PUSH_VAPID_PRIVATE_KEY", "")
	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.providerMode != "disabled" || len(config.apnsPrivateKey) != 0 {
		t.Fatalf("unexpected disabled provider configuration: %#v", config)
	}
}

func TestProviderConfigurationRejectsUnknownMode(t *testing.T) {
	t.Setenv("KNOT_PUSH_PROVIDER_MODE", "unknown")
	if _, err := loadConfiguration(); err == nil {
		t.Fatal("expected provider mode rejection")
	}
}

func TestReadBoundedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.p8")
	if err := os.WriteFile(path, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readBoundedFile(path, 2)
	if err == nil || value != nil {
		t.Fatalf("expected bounded read failure: %q %v", value, err)
	}
	value, err = readBoundedFile(path, 3)
	if err != nil || string(value) != "key" {
		t.Fatalf("unexpected bounded read: %q %v", value, err)
	}
}
