package auth

import (
	"errors"

	"github.com/yaroslavfairfieldd/knot/services/shared/session"
)

var ErrUnauthorized = errors.New("unauthorized")

type Identity struct {
	UserID    string
	SessionID string
	Mode      session.Mode
}

type Verifier struct {
	sessions *session.Manager
}

func NewVerifier(secret []byte) (*Verifier, error) {
	manager, err := session.NewManager(secret)
	if err != nil {
		return nil, err
	}
	return &Verifier{sessions: manager}, nil
}

func (verifier *Verifier) VerifyAuthorization(value string) (Identity, error) {
	claims, err := verifier.sessions.Verify(session.Bearer(value))
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.UserID, SessionID: claims.SessionID, Mode: claims.Mode}, nil
}
