package api

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/delivery"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

const (
	testJWTSecret     = "0123456789abcdef0123456789abcdef"
	testInternalToken = "abcdef0123456789abcdef0123456789"
)

type fakeDispatcher struct {
	userID   string
	deviceID string
	result   delivery.Result
	err      error
}

func (dispatcher *fakeDispatcher) Dispatch(_ context.Context, userID string, deviceID string) (delivery.Result, error) {
	dispatcher.userID = userID
	dispatcher.deviceID = deviceID
	return dispatcher.result, dispatcher.err
}

func TestAPNSRegistrationIsBoundToJWTDevice(t *testing.T) {
	server, store, _ := testServer(t)
	body := `{"device_token":"` + strings.Repeat("A", 64) + `"}`
	request := authenticatedRequest(t, http.MethodPut, "/v1/push/subscriptions/apns", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	values, err := store.ForDevice(context.Background(), "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].UserID != "user-1" || values[0].DeviceID != "device-1" || values[0].APNSToken != strings.Repeat("a", 64) {
		t.Fatalf("unexpected subscription: %#v", values)
	}
	otherValues, err := store.ForDevice(context.Background(), "user-1", "device-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(otherValues) != 0 {
		t.Fatalf("request escaped JWT device: %#v", otherValues)
	}
}

func TestWebRegistrationAndDeletion(t *testing.T) {
	server, store, _ := testServer(t)
	p256dh := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), elliptic.P256().Params().Gx, elliptic.P256().Params().Gy))
	authKey := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	body := fmt.Sprintf(`{"endpoint":"https://push.example.test/subscription","keys":{"p256dh":"%s","auth":"%s"}}`, p256dh, authKey)
	request := authenticatedRequest(t, http.MethodPut, "/v1/push/subscriptions/web", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	request = authenticatedRequest(t, http.MethodDelete, "/v1/push/subscriptions/web", "")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected delete response: %d %s", response.Code, response.Body.String())
	}
	values, err := store.ForDevice(context.Background(), "user-1", "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("subscription not deleted: %#v", values)
	}
}

func TestRegistrationRejectsUntrustedDeviceFieldsAndMissingJWT(t *testing.T) {
	server, _, _ := testServer(t)
	body := `{"device_token":"` + strings.Repeat("a", 64) + `","device_id":"device-2"}`
	request := authenticatedRequest(t, http.MethodPut, "/v1/push/subscriptions/apns", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPut, "/v1/push/subscriptions/apns", strings.NewReader(`{"device_token":"`+strings.Repeat("a", 64)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthenticated response: %d", response.Code)
	}
}

func TestInternalDispatchRequiresSeparateTokenAndFixedEvent(t *testing.T) {
	server, _, dispatcher := testServer(t)
	dispatcher.result = delivery.Result{Attempted: 2, Delivered: 2}
	body := `{"recipient_user_id":"user-2","recipient_device_id":"device-2","event":"message_available"}`
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/push/dispatch", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(internalHeader, testInternalToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || dispatcher.userID != "user-2" || dispatcher.deviceID != "device-2" {
		t.Fatalf("unexpected dispatch response: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/internal/v1/push/dispatch", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testJWT(t))
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("JWT unexpectedly authorized internal dispatch: %d", response.Code)
	}
	invalidBody := `{"recipient_user_id":"user-2","recipient_device_id":"device-2","event":"plaintext_message"}`
	request = httptest.NewRequest(http.MethodPost, "/internal/v1/push/dispatch", strings.NewReader(invalidBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(internalHeader, testInternalToken)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary dispatch event accepted: %d", response.Code)
	}
}

func TestInternalDeviceRevocationRemovesAllChannels(t *testing.T) {
	server, store, _ := testServer(t)
	if _, err := store.UpsertAPNS(context.Background(), "user-2", "device-2", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWeb(context.Background(), "user-2", "device-2", "https://push.example.test/id", subscription.WebKeys{P256DH: "p256dh", Auth: "auth"}); err != nil {
		t.Fatal(err)
	}
	body := `{"user_id":"user-2","device_id":"device-2"}`
	request := httptest.NewRequest(http.MethodDelete, "/internal/v1/push/subscriptions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(internalHeader, testInternalToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unexpected revocation response: %d %s", response.Code, response.Body.String())
	}
	values, err := store.ForDevice(context.Background(), "user-2", "device-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("device subscriptions survived revocation: %#v", values)
	}
}

func TestDispatchReportsPartialProviderFailureWithoutDetails(t *testing.T) {
	server, _, dispatcher := testServer(t)
	dispatcher.result = delivery.Result{Attempted: 2, Delivered: 1}
	dispatcher.err = delivery.ErrDeliveryFailed
	body := `{"recipient_user_id":"user-2","recipient_device_id":"device-2","event":"message_available"}`
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/push/dispatch", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(internalHeader, testInternalToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"delivered":1`) || strings.Contains(response.Body.String(), "provider") {
		t.Fatalf("unexpected partial failure response: %d %s", response.Code, response.Body.String())
	}
}

func TestHealthAndReadiness(t *testing.T) {
	server, _, _ := testServer(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected %s response: %d", path, response.Code)
		}
	}
}

func testServer(t *testing.T) (*Server, *subscription.MemoryStore, *fakeDispatcher) {
	t.Helper()
	store := subscription.NewMemoryStore()
	verifier, err := auth.NewVerifier([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	internalVerifier, err := auth.NewInternalVerifier([]byte(testInternalToken))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeDispatcher{}
	server, err := NewServer(store, verifier, internalVerifier, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	return server, store, dispatcher
}

func authenticatedRequest(t *testing.T, method string, path string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+testJWT(t))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func testJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":"user-1","device_id":"device-1","exp":%d}`, time.Now().Add(time.Minute).Unix())))
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, []byte(testJWTSecret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
