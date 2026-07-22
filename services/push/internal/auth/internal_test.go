package auth

import "testing"

func TestInternalVerifier(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	verifier, err := NewInternalVerifier([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.Verify(token) {
		t.Fatal("valid token rejected")
	}
	if verifier.Verify("0123456789abcdef0123456789abcdeg") || verifier.Verify("") {
		t.Fatal("invalid token accepted")
	}
}

func TestInternalVerifierRequiresStrongToken(t *testing.T) {
	if _, err := NewInternalVerifier([]byte("short")); err == nil {
		t.Fatal("expected configuration rejection")
	}
}
