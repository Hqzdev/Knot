package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/auth"
	"github.com/yaroslavfairfieldd/knot/services/presence/internal/presence"
)

var testSecret = []byte("presence-test-secret")

type testClaims struct {
	Subject  string `json:"sub"`
	DeviceID string `json:"device_id,omitempty"`
	Expiry   int64  `json:"exp"`
}

func TestHeartbeatAndLookup(t *testing.T) {
	server := newTestServer(t)
	aliceToken := testToken(t, testClaims{Subject: "alice", DeviceID: "alice-phone", Expiry: time.Now().Add(time.Minute).Unix()})
	bobToken := testToken(t, testClaims{Subject: "bob", DeviceID: "bob-phone", Expiry: time.Now().Add(time.Minute).Unix()})
	heartbeat := authorizedRequest(http.MethodPost, "/v1/presence/heartbeat", aliceToken, nil)
	heartbeatResponse := httptest.NewRecorder()
	server.ServeHTTP(heartbeatResponse, heartbeat)
	if heartbeatResponse.Code != http.StatusNoContent {
		t.Fatalf("expected heartbeat status %d, got %d: %s", http.StatusNoContent, heartbeatResponse.Code, heartbeatResponse.Body.String())
	}
	lookup := authorizedRequest(http.MethodPost, "/v1/presence/lookup", bobToken, lookupRequest{UserIDs: []string{"alice", "charlie"}})
	lookupResponseRecorder := httptest.NewRecorder()
	server.ServeHTTP(lookupResponseRecorder, lookup)
	if lookupResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected lookup status %d, got %d: %s", http.StatusOK, lookupResponseRecorder.Code, lookupResponseRecorder.Body.String())
	}
	var response lookupResponse
	decodeResponse(t, lookupResponseRecorder, &response)
	if len(response.Users) != 2 || response.Users[0] != (userPresenceResponse{UserID: "alice", Online: true}) || response.Users[1] != (userPresenceResponse{UserID: "charlie", Online: false}) {
		t.Fatalf("unexpected lookup response: %#v", response)
	}
}

func TestHealthAndReadiness(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected %s status %d, got %d", path, http.StatusOK, response.Code)
		}
	}
}

func TestAuthenticationRequiresDeviceBoundToken(t *testing.T) {
	server := newTestServer(t)
	token := testToken(t, testClaims{Subject: "alice", Expiry: time.Now().Add(time.Minute).Unix()})
	request := authorizedRequest(http.MethodPost, "/v1/presence/heartbeat", token, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestLookupIsBoundedAndStrict(t *testing.T) {
	server := newTestServer(t)
	token := testToken(t, testClaims{Subject: "alice", DeviceID: "alice-phone", Expiry: time.Now().Add(time.Minute).Unix()})
	userIDs := make([]string, maxLookupUserIDs+1)
	for index := range userIDs {
		userIDs[index] = "user-" + strings.Repeat("x", index%4+1)
	}
	request := authorizedRequest(http.MethodPost, "/v1/presence/lookup", token, lookupRequest{UserIDs: userIDs})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	unknownFieldRequest := httptest.NewRequest(http.MethodPost, "/v1/presence/lookup", strings.NewReader(`{"user_ids":["bob"],"unexpected":true}`))
	unknownFieldRequest.Header.Set("Authorization", "Bearer "+token)
	unknownFieldResponse := httptest.NewRecorder()
	server.ServeHTTP(unknownFieldResponse, unknownFieldRequest)
	if unknownFieldResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected strict JSON rejection, got %d", unknownFieldResponse.Code)
	}
}

func TestTypingEventsUseAuthenticatedSenderIdentity(t *testing.T) {
	server := newTestServer(t)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	bobToken := testToken(t, testClaims{Subject: "bob", DeviceID: "bob-mac", Expiry: time.Now().Add(time.Minute).Unix()})
	aliceToken := testToken(t, testClaims{Subject: "alice", DeviceID: "alice-phone", Expiry: time.Now().Add(time.Minute).Unix()})
	bobConnection := dialWebsocket(t, httpServer.URL, bobToken)
	defer bobConnection.Close()
	aliceConnection := dialWebsocket(t, httpServer.URL, aliceToken)
	defer aliceConnection.Close()
	if err := aliceConnection.WriteJSON(map[string]any{
		"type":              "typing",
		"recipient_user_id": "bob",
		"active":            true,
	}); err != nil {
		t.Fatal(err)
	}
	bobConnection.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event websocketEvent
	if err := bobConnection.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "typing" || event.SenderUserID != "alice" || event.SenderDeviceID != "alice-phone" || event.RecipientUserID != "bob" || !event.Active || event.OccurredAt.IsZero() {
		t.Fatalf("unexpected typing event: %#v", event)
	}
}

func TestTypingCommandRejectsCallerSuppliedIdentity(t *testing.T) {
	server := newTestServer(t)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	token := testToken(t, testClaims{Subject: "alice", DeviceID: "alice-phone", Expiry: time.Now().Add(time.Minute).Unix()})
	connection := dialWebsocket(t, httpServer.URL, token)
	defer connection.Close()
	if err := connection.WriteJSON(map[string]any{
		"type":              "typing",
		"recipient_user_id": "bob",
		"active":            true,
		"sender_user_id":    "mallory",
	}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := connection.ReadMessage()
	if err == nil {
		t.Fatal("expected websocket policy rejection")
	}
	closeError, ok := err.(*websocket.CloseError)
	if !ok || closeError.Code != websocket.ClosePolicyViolation {
		t.Fatalf("expected policy violation, got %v", err)
	}
}

func TestBrowserWebsocketUsesJWTSubprotocol(t *testing.T) {
	server := newTestServer(t)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	token := testToken(t, testClaims{Subject: "alice", DeviceID: "alice-browser", Expiry: time.Now().Add(time.Minute).Unix()})
	protocol := websocketJWTProtocol + token
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/presence/ws", nil)
	if err != nil {
		if response != nil {
			t.Fatalf("websocket dial failed with status %d: %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	defer connection.Close()
	if connection.Subprotocol() != protocol {
		t.Fatalf("expected selected protocol %q, got %q", protocol, connection.Subprotocol())
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	verifier, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(presence.NewMemoryStore(), verifier)
}

func authorizedRequest(method string, path string, token string, body any) *http.Request {
	var encodedBody bytes.Buffer
	if body != nil {
		json.NewEncoder(&encodedBody).Encode(body)
	}
	request := httptest.NewRequest(method, path, &encodedBody)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func dialWebsocket(t *testing.T, baseURL string, token string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	connection, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(baseURL, "http")+"/v1/presence/ws", header)
	if err != nil {
		if response != nil {
			t.Fatalf("websocket dial failed with status %d: %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	return connection
}

func testToken(t *testing.T, claims testClaims) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, testSecret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
