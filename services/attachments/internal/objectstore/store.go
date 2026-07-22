package objectstore

import (
	"context"
	"errors"
	"time"
)

var ErrObjectNotFound = errors.New("object not found")

type SignedRequest struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Size             int64
	CiphertextSHA256 string
}

type Store interface {
	PresignUpload(context.Context, string, int64, string, time.Duration) (SignedRequest, error)
	PresignDownload(context.Context, string, time.Duration) (SignedRequest, error)
	Head(context.Context, string) (ObjectInfo, error)
	Delete(context.Context, string) error
	Ping(context.Context) error
}
