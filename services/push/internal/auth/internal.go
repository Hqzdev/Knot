package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
)

type InternalVerifier struct {
	digest [sha256.Size]byte
}

func NewInternalVerifier(token []byte) (*InternalVerifier, error) {
	if len(token) < 32 {
		return nil, errors.New("invalid internal token configuration")
	}
	return &InternalVerifier{digest: sha256.Sum256(token)}, nil
}

func (verifier *InternalVerifier) Verify(value string) bool {
	provided := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(provided[:], verifier.digest[:]) == 1
}
