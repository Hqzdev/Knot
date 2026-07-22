package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
)

type recordedPush struct {
	userID   string
	deviceID string
}

type recordingPushClient struct {
	messages    []recordedPush
	revocations []recordedPush
}

func (client *recordingPushClient) MessageAvailable(_ context.Context, userID string, deviceID string) error {
	client.messages = append(client.messages, recordedPush{userID: userID, deviceID: deviceID})
	return nil
}

func (client *recordingPushClient) RevokeDevice(_ context.Context, userID string, deviceID string) error {
	client.revocations = append(client.revocations, recordedPush{userID: userID, deviceID: deviceID})
	return nil
}

func TestOfflineMessageTriggersPushAndRevocationCleanup(t *testing.T) {
	push := &recordingPushClient{}
	server := NewServerWithPush(store.NewMemoryStore(), []byte("test-signing-secret"), push)
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	bobWeb := registerDevice(t, server, bob.AccessToken, "Bob Web", "web", 30)
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Envelopes: []messageEnvelopeRequest{
			{RecipientDeviceID: bob.DeviceID, Ciphertext: encodedBytes(41, 48)},
			{RecipientDeviceID: bobWeb.ID, Ciphertext: encodedBytes(42, 48)},
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || len(push.messages) != 2 {
		t.Fatalf("unexpected push fanout: %d %#v", response.Code, push.messages)
	}
	for _, notification := range push.messages {
		if notification.userID != bob.UserID {
			t.Fatalf("unexpected push user: %#v", notification)
		}
	}
	revokeRequest := authorizedRequest(http.MethodDelete, "/v1/devices/"+bobWeb.ID, bob.AccessToken)
	revokeResponse := httptest.NewRecorder()
	server.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent || len(push.revocations) != 1 || push.revocations[0].deviceID != bobWeb.ID {
		t.Fatalf("unexpected push cleanup: %d %#v", revokeResponse.Code, push.revocations)
	}
}

func TestLiveMessageDoesNotTriggerPush(t *testing.T) {
	push := &recordingPushClient{}
	server := NewServerWithPush(store.NewMemoryStore(), []byte("test-signing-secret"), push)
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	client := &websocketClient{outgoing: make(chan websocketEvent, 1)}
	server.hub.register(bob.DeviceID, client)
	defer server.hub.unregister(bob.DeviceID, client)
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Ciphertext:        encodedBytes(41, 48),
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || len(push.messages) != 0 || len(client.outgoing) != 1 {
		t.Fatalf("unexpected live delivery: %d %#v %d", response.Code, push.messages, len(client.outgoing))
	}
}

func TestHTTPPushClientUsesInternalContracts(t *testing.T) {
	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(pushInternalHeader) != "12345678901234567890123456789012" {
			t.Error("missing internal token")
		}
		requests <- request.Clone(context.Background())
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.URL.Path == "/internal/v1/push/dispatch" {
			if request.Method != http.MethodPost || payload["event"] != "message_available" {
				t.Fatalf("unexpected dispatch: %s %#v", request.Method, payload)
			}
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		if request.URL.Path != "/internal/v1/push/subscriptions" || request.Method != http.MethodDelete {
			t.Fatalf("unexpected revocation: %s %s", request.Method, request.URL.Path)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewHTTPPushClient(server.URL, "12345678901234567890123456789012", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.MessageAvailable(context.Background(), "user", "device"); err != nil {
		t.Fatal(err)
	}
	if err := client.RevokeDevice(context.Background(), "user", "device"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected two push requests, got %d", len(requests))
	}
}
