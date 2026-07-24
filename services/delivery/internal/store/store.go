package store

import (
	"context"
	"errors"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

var (
	ErrMessageNotFound = errors.New("message not found")
	ErrForbidden       = errors.New("message action forbidden")
	ErrInvalidEvent    = errors.New("invalid message event")
	ErrLegacySchema    = errors.New("legacy encrypted delivery schema detected; reset the delivery database")
)

type WiretapFilter struct {
	AfterSequence uint64
	Limit         int
	Author        string
	Participant   string
	Conversation  string
	SessionMode   knotv1.SessionMode
	EventKind     knotv1.MessageEventKind
}

type Store interface {
	Append(context.Context, *knotv1.Message) (*knotv1.Message, bool, error)
	ApplyEvent(context.Context, *knotv1.ApplyEventRequest) (*knotv1.Message, bool, error)
	History(context.Context, string, string, uint64, int) ([]*knotv1.Message, uint64, error)
	Wiretap(context.Context, WiretapFilter) ([]*knotv1.WiretapRecord, uint64, error)
	Ping(context.Context) error
}
