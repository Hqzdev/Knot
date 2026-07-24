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
	Size   int64
	SHA256 string
}

type MediaObject struct {
	Data      []byte
	MediaType string
}

type Store interface {
	PresignUpload(context.Context, string, int64, string, time.Duration) (SignedRequest, error)
	PresignDownload(context.Context, string, time.Duration) (SignedRequest, error)
	Head(context.Context, string) (ObjectInfo, error)
	Put(context.Context, string, string, []byte) error
	Get(context.Context, string) (MediaObject, error)
	Delete(context.Context, string) error
	Ping(context.Context) error
}
