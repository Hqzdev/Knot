package objectstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPresignedUploadIsDeterministicAndBindsMetadata(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	store, err := newS3Store(S3Config{
		Endpoint:        "https://objects.example.com",
		PublicEndpoint:  "https://downloads.example.com",
		Region:          "us-east-1",
		Bucket:          "knot-attachments",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		SessionToken:    "session-token",
		PathStyle:       true,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PresignUpload(context.Background(), "attachments/id", 42, testSHA256, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PresignUpload(context.Background(), "attachments/id", 42, testSHA256, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.URL != second.URL {
		t.Fatalf("presigning is not deterministic: %q != %q", first.URL, second.URL)
	}
	parsed, err := url.Parse(first.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "downloads.example.com" || parsed.Path != "/knot-attachments/attachments/id" || query.Get("X-Amz-Algorithm") != algorithm || query.Get("X-Amz-Expires") != "900" || query.Get("X-Amz-Security-Token") != "session-token" || len(query.Get("X-Amz-Signature")) != 64 {
		t.Fatalf("unexpected presigned URL: %s", first.URL)
	}
	if query.Get("X-Amz-SignedHeaders") != "content-disposition;content-length;host;x-amz-meta-sha256" {
		t.Fatalf("unexpected signed headers: %s", query.Get("X-Amz-SignedHeaders"))
	}
	if first.Headers["Content-Disposition"] != "attachment" || first.Headers["Content-Length"] != "42" || first.Headers["X-Amz-Meta-Sha256"] != testSHA256 || !first.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("unexpected upload request: %#v", first)
	}
}

func TestS3HeadDeleteAndReadinessAreSigned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.Header.Get("Authorization"), algorithm+" Credential=access-key/") || request.Header.Get("X-Amz-Content-Sha256") != emptyPayloadHash {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodHead && request.URL.Path == "/bucket/attachments/id":
			writer.Header().Set("Content-Length", "42")
			writer.Header().Set("X-Amz-Meta-Sha256", testSHA256)
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodDelete && request.URL.Path == "/bucket/attachments/id":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodHead && request.URL.Path == "/bucket":
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	store, err := NewS3Store(S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "bucket", AccessKeyID: "access-key", SecretAccessKey: "secret-key", PathStyle: true, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Head(context.Background(), "attachments/id")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 42 || info.SHA256 != testSHA256 {
		t.Fatalf("unexpected object info: %#v", info)
	}
	if err := store.Delete(context.Background(), "attachments/id"); err != nil {
		t.Fatal(err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestS3ConfigurationAndInputsAreStrict(t *testing.T) {
	if _, err := NewS3Store(S3Config{Endpoint: "ftp://example.com", Region: "region", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret"}); err == nil {
		t.Fatal("expected invalid endpoint rejection")
	}
	if _, err := NewS3Store(S3Config{Endpoint: "https://example.com", PublicEndpoint: "file:///tmp/objects", Region: "region", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret"}); err == nil {
		t.Fatal("expected invalid public endpoint rejection")
	}
	store, err := NewS3Store(S3Config{Endpoint: "https://example.com", Region: "region", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PresignUpload(context.Background(), "attachments/id", 0, testSHA256, time.Minute); err == nil {
		t.Fatal("expected invalid size rejection")
	}
	if _, err := store.PresignDownload(context.Background(), "/absolute", time.Minute); err == nil {
		t.Fatal("expected invalid object key rejection")
	}
}
