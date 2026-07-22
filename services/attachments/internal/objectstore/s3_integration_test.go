package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestS3CompatibleLifecycle(t *testing.T) {
	endpoint := os.Getenv("KNOT_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("KNOT_TEST_S3_ENDPOINT is not set")
	}
	store, err := NewS3Store(S3Config{
		Endpoint:        endpoint,
		Region:          valueOrFallback("KNOT_TEST_S3_REGION", "us-east-1"),
		Bucket:          os.Getenv("KNOT_TEST_S3_BUCKET"),
		AccessKeyID:     os.Getenv("KNOT_TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("KNOT_TEST_S3_SECRET_ACCESS_KEY"),
		PathStyle:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{73}, 257)
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	objectKey := "attachments/integration-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	upload, err := store.PresignUpload(context.Background(), objectKey, int64(len(payload)), hash, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = int64(len(payload))
	for name, value := range upload.Headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("upload returned status %d", response.StatusCode)
	}
	info, err := store.Head(context.Background(), objectKey)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(payload)) || info.CiphertextSHA256 != hash {
		t.Fatalf("unexpected object info: %#v", info)
	}
	download, err := store.PresignDownload(context.Background(), objectKey, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	downloadResponse, err := http.Get(download.URL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(io.LimitReader(downloadResponse.Body, int64(len(payload))+1))
	downloadResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if downloadResponse.StatusCode != http.StatusOK || !bytes.Equal(downloaded, payload) {
		t.Fatalf("unexpected download status=%d size=%d", downloadResponse.StatusCode, len(downloaded))
	}
	if err := store.Delete(context.Background(), objectKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Head(context.Background(), objectKey); err != ErrObjectNotFound {
		t.Fatalf("expected deleted object, got %v", err)
	}
}

func valueOrFallback(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
