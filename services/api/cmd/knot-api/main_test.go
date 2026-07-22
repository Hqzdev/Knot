package main

import "testing"

func TestParseAllowedOriginsRejectsWildcardsAndPaths(t *testing.T) {
	origins, err := parseAllowedOrigins("https://app.example,http://localhost:5173")
	if err != nil || len(origins) != 2 {
		t.Fatalf("unexpected origins: %#v %v", origins, err)
	}
	for _, value := range []string{"*", "https://app.example/path", "file://local"} {
		if _, err := parseAllowedOrigins(value); err == nil {
			t.Fatalf("expected invalid origin rejection for %q", value)
		}
	}
}
