package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
)

const testSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var apiTestSecret = []byte("attachments-test-secret")

type fakeObjectStore struct {
	now          time.Time
	info         objectstore.ObjectInfo
	headError    error
	deleteError  error
	deleted      []string
	uploadKey    string
	uploadSize   int64
	uploadSHA256 string
	downloadKey  string
	downloadTTL  time.Duration
	pingError    error
}

func (store *fakeObjectStore) PresignUpload(_ context.Context, key string, size int64, hash string, ttl time.Duration) (objectstore.SignedRequest, error) {
	store.uploadKey = key
	store.uploadSize = size
	store.uploadSHA256 = hash
	return objectstore.SignedRequest{
		URL:       "https://objects.example/upload",
		Headers:   map[string]string{"Content-Length": "42", "X-Amz-Meta-Sha256": hash},
		ExpiresAt: store.now.Add(ttl),
	}, nil
}

func (store *fakeObjectStore) PresignDownload(_ context.Context, key string, ttl time.Duration) (objectstore.SignedRequest, error) {
	store.downloadKey = key
	store.downloadTTL = ttl
	return objectstore.SignedRequest{URL: "https://objects.example/download", ExpiresAt: store.now.Add(ttl)}, nil
}

func (store *fakeObjectStore) Head(context.Context, string) (objectstore.ObjectInfo, error) {
	return store.info, store.headError
}

func (store *fakeObjectStore) Delete(_ context.Context, key string) error {
	store.deleted = append(store.deleted, key)
	return store.deleteError
}

func (store *fakeObjectStore) Ping(context.Context) error {
	return store.pingError
}

func TestAttachmentLifecycleUsesOpaqueCiphertextMetadata(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	server, metadataStore, objects, id := newTestServer(t, now)
	ownerToken := accessToken(t, "owner", "owner-phone")
	readerToken := accessToken(t, "reader", "reader-web")
	create := authorizedJSONRequest(t, http.MethodPost, "/v1/attachments", ownerToken, createRequest{CiphertextSize: 42, CiphertextSHA256: testSHA256})
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d: %s", http.StatusCreated, createRecorder.Code, createRecorder.Body.String())
	}
	var created createResponse
	decodeResponse(t, createRecorder, &created)
	if created.AttachmentID != id || created.UploadURL == "" || created.RequiredHeaders["X-Amz-Meta-Sha256"] != testSHA256 || objects.uploadKey != "attachments/"+id || objects.uploadSize != 42 || objects.uploadSHA256 != testSHA256 {
		t.Fatalf("unexpected create response: %#v", created)
	}
	objects.info = objectstore.ObjectInfo{Size: 42, CiphertextSHA256: testSHA256}
	complete := authorizedJSONRequest(t, http.MethodPost, "/v1/attachments/"+id+"/complete", ownerToken, nil)
	completeRecorder := httptest.NewRecorder()
	server.ServeHTTP(completeRecorder, complete)
	if completeRecorder.Code != http.StatusOK {
		t.Fatalf("expected complete status %d, got %d: %s", http.StatusOK, completeRecorder.Code, completeRecorder.Body.String())
	}
	var ready readyResponse
	decodeResponse(t, completeRecorder, &ready)
	if ready.Status != "ready" || !ready.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("unexpected ready response: %#v", ready)
	}
	download := authorizedJSONRequest(t, http.MethodGet, "/v1/attachments/"+id, readerToken, nil)
	downloadRecorder := httptest.NewRecorder()
	server.ServeHTTP(downloadRecorder, download)
	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("expected download status %d, got %d: %s", http.StatusOK, downloadRecorder.Code, downloadRecorder.Body.String())
	}
	var downloadResult downloadResponse
	decodeResponse(t, downloadRecorder, &downloadResult)
	if downloadResult.DownloadURL == "" || objects.downloadKey != "attachments/"+id || objects.downloadTTL != 5*time.Minute {
		t.Fatalf("unexpected download response: %#v", downloadResult)
	}
	wrongDelete := authorizedJSONRequest(t, http.MethodDelete, "/v1/attachments/"+id, readerToken, nil)
	wrongDeleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(wrongDeleteRecorder, wrongDelete)
	if wrongDeleteRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected owner isolation, got %d", wrongDeleteRecorder.Code)
	}
	deleteRequest := authorizedJSONRequest(t, http.MethodDelete, "/v1/attachments/"+id, ownerToken, nil)
	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNoContent || len(objects.deleted) != 1 || objects.deleted[0] != "attachments/"+id {
		t.Fatalf("unexpected delete result: status=%d deleted=%#v", deleteRecorder.Code, objects.deleted)
	}
	if _, err := metadataStore.FindReady(context.Background(), id, now); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected purged metadata, got %v", err)
	}
}

func TestCompletionRejectsObjectMetadataMismatch(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	server, _, objects, id := newTestServer(t, now)
	token := accessToken(t, "owner", "device")
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, authorizedJSONRequest(t, http.MethodPost, "/v1/attachments", token, createRequest{CiphertextSize: 42, CiphertextSHA256: testSHA256}))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create failed: %s", createRecorder.Body.String())
	}
	objects.info = objectstore.ObjectInfo{Size: 41, CiphertextSHA256: testSHA256}
	completeRecorder := httptest.NewRecorder()
	server.ServeHTTP(completeRecorder, authorizedJSONRequest(t, http.MethodPost, "/v1/attachments/"+id+"/complete", token, nil))
	if completeRecorder.Code != http.StatusConflict {
		t.Fatalf("expected verification conflict, got %d: %s", completeRecorder.Code, completeRecorder.Body.String())
	}
}

func TestCreateRejectsPlaintextMetadataAndOversizedBlobs(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	server, _, _, _ := newTestServer(t, now)
	token := accessToken(t, "owner", "device")
	plaintextFields := authorizedJSONRequest(t, http.MethodPost, "/v1/attachments", token, map[string]any{
		"ciphertext_size":   42,
		"ciphertext_sha256": testSHA256,
		"media_key":         "forbidden",
		"mime_type":         "image/png",
		"filename":          "secret.png",
	})
	plaintextRecorder := httptest.NewRecorder()
	server.ServeHTTP(plaintextRecorder, plaintextFields)
	if plaintextRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected plaintext metadata rejection, got %d", plaintextRecorder.Code)
	}
	oversizedRecorder := httptest.NewRecorder()
	server.ServeHTTP(oversizedRecorder, authorizedJSONRequest(t, http.MethodPost, "/v1/attachments", token, createRequest{CiphertextSize: 1025, CiphertextSHA256: testSHA256}))
	if oversizedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized rejection, got %d", oversizedRecorder.Code)
	}
}

func TestReadinessChecksBothStores(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	server, _, objects, _ := newTestServer(t, now)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected readiness, got %d", response.Code)
	}
	objects.pingError = errors.New("unavailable")
	unavailable := httptest.NewRecorder()
	server.ServeHTTP(unavailable, request)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable readiness, got %d", unavailable.Code)
	}
}

func newTestServer(t *testing.T, now time.Time) (*Server, *metadata.MemoryStore, *fakeObjectStore, string) {
	t.Helper()
	verifier, err := auth.NewVerifier(apiTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	metadataStore := metadata.NewMemoryStore()
	objects := &fakeObjectStore{now: now}
	server, err := NewServer(metadataStore, objects, verifier, Limits{MaxCiphertextSize: 1024, UploadTTL: 15 * time.Minute, PendingTTL: time.Hour, DownloadTTL: 5 * time.Minute, RetentionTTL: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	id := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, attachmentIDBytes))
	server.now = func() time.Time { return now }
	server.identifiers = func() (string, error) { return id, nil }
	return server, metadataStore, objects, id
}

func authorizedJSONRequest(t *testing.T, method string, path string, token string, body any) *http.Request {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &payload)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func accessToken(t *testing.T, userID string, deviceID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"sub": userID, "device_id": deviceID, "exp": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, apiTestSecret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
