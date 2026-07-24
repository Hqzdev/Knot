package security

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type resolver map[string][]netip.Addr

func (values resolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	addresses, exists := values[host]
	if !exists {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func TestGuardAllowsOnlyPublicHTTPAddresses(t *testing.T) {
	guard := NewGuard(resolver{
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
		"private.example": {netip.MustParseAddr("10.0.0.2")},
		"mixed.example":   {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")},
	})
	if _, err := guard.Validate(context.Background(), "https://public.example/path"); err != nil {
		t.Fatalf("expected public URL, got %v", err)
	}
	blocked := []string{
		"file:///etc/passwd",
		"http://localhost/admin",
		"http://127.0.0.1/admin",
		"http://[::1]/admin",
		"http://private.example/admin",
		"http://mixed.example/admin",
		"http://169.254.169.254/latest/meta-data",
		"https://user:password@public.example/",
	}
	for _, value := range blocked {
		if _, err := guard.Validate(context.Background(), value); !errors.Is(err, ErrUnsafeAddress) {
			t.Fatalf("expected %q to be blocked, got %v", value, err)
		}
	}
}
