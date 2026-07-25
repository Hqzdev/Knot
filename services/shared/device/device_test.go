package device

import "testing"

func TestParse(t *testing.T) {
	value := Parse("device-1", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/137.0 Safari/537.36")
	if value.ID != "device-1" || value.Browser != "Chrome" || value.OS != "macOS" || value.FormFactor != "desktop" {
		t.Fatalf("unexpected descriptor: %#v", value)
	}
}

func TestParseMobile(t *testing.T) {
	value := Parse("device-2", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1")
	if value.Browser != "Safari" || value.OS != "iOS" || value.FormFactor != "mobile" {
		t.Fatalf("unexpected descriptor: %#v", value)
	}
}
