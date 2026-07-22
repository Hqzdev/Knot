package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
)

const (
	maxRequestBytes    = 4 << 10
	attachmentIDBytes  = 24
	identifierAttempts = 3
)

type Limits struct {
	MaxCiphertextSize int64
	UploadTTL         time.Duration
	PendingTTL        time.Duration
	DownloadTTL       time.Duration
	RetentionTTL      time.Duration
}

type Server struct {
	metadata    metadata.Store
	objects     objectstore.Store
	verifier    *auth.Verifier
	limits      Limits
	now         func() time.Time
	identifiers func() (string, error)
}

type createRequest struct {
	CiphertextSize   int64  `json:"ciphertext_size"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
}

type createResponse struct {
	AttachmentID     string            `json:"attachment_id"`
	UploadURL        string            `json:"upload_url"`
	UploadExpiresAt  time.Time         `json:"upload_expires_at"`
	RequiredHeaders  map[string]string `json:"required_headers"`
	CiphertextSize   int64             `json:"ciphertext_size"`
	CiphertextSHA256 string            `json:"ciphertext_sha256"`
}

type readyResponse struct {
	AttachmentID     string    `json:"attachment_id"`
	Status           string    `json:"status"`
	CiphertextSize   int64     `json:"ciphertext_size"`
	CiphertextSHA256 string    `json:"ciphertext_sha256"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type downloadResponse struct {
	AttachmentID      string    `json:"attachment_id"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
	CiphertextSize    int64     `json:"ciphertext_size"`
	CiphertextSHA256  string    `json:"ciphertext_sha256"`
}

func NewServer(metadataStore metadata.Store, objectStore objectstore.Store, verifier *auth.Verifier, limits Limits) (*Server, error) {
	if metadataStore == nil || objectStore == nil || verifier == nil || limits.MaxCiphertextSize <= 0 || limits.UploadTTL < time.Second || limits.PendingTTL < limits.UploadTTL || limits.DownloadTTL < time.Second || limits.RetentionTTL < time.Second {
		return nil, errors.New("invalid attachments server configuration")
	}
	return &Server{metadata: metadataStore, objects: objectStore, verifier: verifier, limits: limits, now: time.Now, identifiers: randomIdentifier}, nil
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	case request.Method == http.MethodGet && request.URL.Path == "/readyz":
		server.readiness(writer, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/attachments":
		server.create(writer, request)
	case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/v1/attachments/") && strings.HasSuffix(request.URL.Path, "/complete"):
		server.complete(writer, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/attachments/"):
		server.download(writer, request)
	case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/v1/attachments/"):
		server.delete(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "route not found")
	}
}

func (server *Server) readiness(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	if err := server.metadata.Ping(ctx); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	if err := server.objects.Ping(ctx); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) create(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	var input createRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.CiphertextSize <= 0 || input.CiphertextSize > server.limits.MaxCiphertextSize || !validSHA256(input.CiphertextSHA256) {
		writeError(writer, http.StatusBadRequest, "invalid ciphertext metadata")
		return
	}
	for attempt := 0; attempt < identifierAttempts; attempt++ {
		id, err := server.identifiers()
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "attachment creation failed")
			return
		}
		objectKey := "attachments/" + id
		upload, err := server.objects.PresignUpload(request.Context(), objectKey, input.CiphertextSize, input.CiphertextSHA256, server.limits.UploadTTL)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "object store unavailable")
			return
		}
		now := server.now().UTC()
		attachment := metadata.Attachment{
			ID:               id,
			OwnerUserID:      identity.UserID,
			OwnerDeviceID:    identity.DeviceID,
			ObjectKey:        objectKey,
			CiphertextSize:   input.CiphertextSize,
			CiphertextSHA256: input.CiphertextSHA256,
			Status:           metadata.StatusPending,
			CreatedAt:        now,
			ExpiresAt:        now.Add(server.limits.PendingTTL),
		}
		if err := server.metadata.Create(request.Context(), attachment); errors.Is(err, metadata.ErrConflict) {
			continue
		} else if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
			return
		}
		writeJSON(writer, http.StatusCreated, createResponse{
			AttachmentID:     id,
			UploadURL:        upload.URL,
			UploadExpiresAt:  upload.ExpiresAt,
			RequiredHeaders:  upload.Headers,
			CiphertextSize:   input.CiphertextSize,
			CiphertextSHA256: input.CiphertextSHA256,
		})
		return
	}
	writeError(writer, http.StatusServiceUnavailable, "attachment creation failed")
}

func (server *Server) complete(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	id, valid := completeAttachmentID(request.URL.Path)
	if !valid {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	if !requireEmptyBody(writer, request) {
		return
	}
	now := server.now().UTC()
	attachment, err := server.metadata.FindForCompletion(request.Context(), id, identity.UserID, now)
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
		return
	}
	if attachment.Status == metadata.StatusReady {
		writeJSON(writer, http.StatusOK, newReadyResponse(attachment))
		return
	}
	objectInfo, err := server.objects.Head(request.Context(), attachment.ObjectKey)
	if errors.Is(err, objectstore.ErrObjectNotFound) {
		writeError(writer, http.StatusConflict, "ciphertext upload not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "object store unavailable")
		return
	}
	if objectInfo.Size != attachment.CiphertextSize || objectInfo.CiphertextSHA256 != attachment.CiphertextSHA256 {
		writeError(writer, http.StatusConflict, "ciphertext upload verification failed")
		return
	}
	completedAt := server.now().UTC()
	attachment, err = server.metadata.Complete(request.Context(), id, identity.UserID, completedAt, completedAt.Add(server.limits.RetentionTTL))
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, newReadyResponse(attachment))
}

func (server *Server) download(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authenticate(writer, request); !ok {
		return
	}
	id, valid := attachmentID(request.URL.Path)
	if !valid {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	now := server.now().UTC()
	attachment, err := server.metadata.FindReady(request.Context(), id, now)
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
		return
	}
	ttl := server.limits.DownloadTTL
	if remaining := attachment.ExpiresAt.Sub(now); remaining < ttl {
		ttl = remaining.Truncate(time.Second)
	}
	if ttl < time.Second {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	download, err := server.objects.PresignDownload(request.Context(), attachment.ObjectKey, ttl)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "object store unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, downloadResponse{
		AttachmentID:      attachment.ID,
		DownloadURL:       download.URL,
		DownloadExpiresAt: download.ExpiresAt,
		CiphertextSize:    attachment.CiphertextSize,
		CiphertextSHA256:  attachment.CiphertextSHA256,
	})
}

func (server *Server) delete(writer http.ResponseWriter, request *http.Request) {
	identity, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	id, valid := attachmentID(request.URL.Path)
	if !valid {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	attachment, err := server.metadata.ClaimDelete(request.Context(), id, identity.UserID, server.now().UTC())
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
		return
	}
	if err := server.objects.Delete(request.Context(), attachment.ObjectKey); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "object store unavailable")
		return
	}
	if err := server.metadata.Purge(request.Context(), attachment.ID); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "metadata store unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (auth.Identity, bool) {
	identity, err := server.verifier.VerifyAuthorization(request.Header.Get("Authorization"))
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return auth.Identity{}, false
	}
	return identity, true
}

func attachmentID(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/v1/attachments/")
	return id, validAttachmentID(id)
}

func completeAttachmentID(path string) (string, bool) {
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/attachments/"), "/complete")
	return id, validAttachmentID(id)
}

func validAttachmentID(id string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(decoded) == attachmentIDBytes && !strings.Contains(id, "/")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func randomIdentifier() (string, error) {
	value := make([]byte, attachmentIDBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func newReadyResponse(attachment metadata.Attachment) readyResponse {
	return readyResponse{AttachmentID: attachment.ID, Status: "ready", CiphertextSize: attachment.CiphertextSize, CiphertextSHA256: attachment.CiphertextSHA256, ExpiresAt: attachment.ExpiresAt}
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return false
	}
	return true
}

func requireEmptyBody(writer http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 1)
	buffer := make([]byte, 1)
	count, err := request.Body.Read(buffer)
	if count != 0 || err != nil && err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must be empty")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
