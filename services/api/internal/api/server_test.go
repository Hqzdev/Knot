package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaroslavfairfieldd/knot/services/api/internal/ratelimit"
	"github.com/yaroslavfairfieldd/knot/services/api/internal/store"
)

const testPassword = "correct-horse-battery-staple"

func TestEmailRegistrationAndLogin(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	if alice.Email != "alice@example.com" {
		t.Fatalf("unexpected registered email: %q", alice.Email)
	}
	request := jsonRequest(t, http.MethodPost, "/v1/auth/login", loginRequest{
		Email:    "ALICE@EXAMPLE.COM",
		Password: testPassword,
		DeviceID: alice.DeviceID,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var authenticated authResponse
	decodeResponse(t, response, &authenticated)
	if authenticated.UserID != alice.UserID || authenticated.Username != "alice" || authenticated.Email != "alice@example.com" {
		t.Fatalf("unexpected authentication response: %#v", authenticated)
	}
}

func TestRegistrationRejectsDuplicateEmail(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	registerUser(t, server, "alice", "Alice iPhone", 10)
	request := jsonRequest(t, http.MethodPost, "/v1/auth/register", registerRequest{
		Email:    "ALICE@example.com",
		Username: "alice_two",
		Password: testPassword,
		Device: deviceRegistrationRequest{
			Name:      "Second iPhone",
			Platform:  "ios",
			KeyBundle: testPreKeyMaterial(20),
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, response.Code, response.Body.String())
	}
}

func TestRegistrationRejectsInvalidEmail(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	request := jsonRequest(t, http.MethodPost, "/v1/auth/register", registerRequest{
		Email:    "alice@example",
		Username: "alice",
		Password: testPassword,
		Device: deviceRegistrationRequest{
			Name:      "Alice iPhone",
			Platform:  "ios",
			KeyBundle: testPreKeyMaterial(10),
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

func TestLegacyRegistrationWithoutEmailStillWorks(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	request := jsonRequest(t, http.MethodPost, "/v1/auth/register", registerRequest{
		Username: "legacy",
		Password: testPassword,
		Device: deviceRegistrationRequest{
			Name:      "Legacy Browser",
			Platform:  "web",
			KeyBundle: testPreKeyMaterial(10),
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var registered authResponse
	decodeResponse(t, response, &registered)
	if registered.Email != "" || registered.Username != "legacy" {
		t.Fatalf("unexpected legacy registration: %#v", registered)
	}
	authenticated := loginUser(t, server, "legacy", registered.DeviceID)
	if authenticated.UserID != registered.UserID {
		t.Fatalf("unexpected legacy login: %#v", authenticated)
	}
}

func TestOwnKeyBundlesExcludeCurrentDevice(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	aliceMac := registerDevice(t, server, alice.AccessToken, "Alice Mac", "macos", 20)

	request := authorizedRequest(http.MethodGet, "/v1/devices/me/keys", alice.AccessToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var directory keyBundleResponse
	decodeResponse(t, response, &directory)
	if directory.UserID != alice.UserID || directory.Username != alice.Username || len(directory.Devices) != 1 {
		t.Fatalf("unexpected own key directory: %#v", directory)
	}
	if directory.Devices[0].DeviceID != aliceMac.ID || directory.Devices[0].DeviceID == alice.DeviceID {
		t.Fatalf("current device was not excluded: %#v", directory.Devices)
	}
}

func TestPerDeviceMessageFanoutAndAcknowledgement(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	bobMac := registerDevice(t, server, bob.AccessToken, "Bob Mac", "macos", 30)
	bobMacSession := loginUser(t, server, "bob", bobMac.ID)

	primaryCiphertext := encodedBytes(41, 48)
	secondaryCiphertext := encodedBytes(42, 48)
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Envelopes: []messageEnvelopeRequest{
			{RecipientDeviceID: bob.DeviceID, Ciphertext: primaryCiphertext},
			{RecipientDeviceID: bobMac.ID, Ciphertext: secondaryCiphertext},
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, response.Code, response.Body.String())
	}
	var sent sendMessageResponse
	decodeResponse(t, response, &sent)
	if len(sent.Messages) != 2 {
		t.Fatalf("expected 2 fanout messages, got %#v", sent.Messages)
	}
	primaryMessage := messageForDevice(t, sent.Messages, bob.DeviceID)
	secondaryMessage := messageForDevice(t, sent.Messages, bobMac.ID)
	if primaryMessage.Ciphertext != primaryCiphertext || secondaryMessage.Ciphertext != secondaryCiphertext {
		t.Fatalf("unexpected ciphertext fanout: %#v", sent.Messages)
	}
	if primaryMessage.SenderDeviceID != alice.DeviceID || secondaryMessage.SenderDeviceID != alice.DeviceID {
		t.Fatalf("unexpected sender device: %#v", sent.Messages)
	}

	primaryPending := pendingMessages(t, server, bob.AccessToken)
	secondaryPending := pendingMessages(t, server, bobMacSession.AccessToken)
	if len(primaryPending) != 1 || primaryPending[0].ID != primaryMessage.ID {
		t.Fatalf("unexpected primary pending messages: %#v", primaryPending)
	}
	if len(secondaryPending) != 1 || secondaryPending[0].ID != secondaryMessage.ID {
		t.Fatalf("unexpected secondary pending messages: %#v", secondaryPending)
	}

	wrongAcknowledgement := authorizedRequest(http.MethodPost, "/v1/messages/"+secondaryMessage.ID+"/ack", bob.AccessToken)
	wrongAcknowledgementResponse := httptest.NewRecorder()
	server.ServeHTTP(wrongAcknowledgementResponse, wrongAcknowledgement)
	if wrongAcknowledgementResponse.Code != http.StatusNotFound {
		t.Fatalf("expected device-scoped ack rejection, got %d", wrongAcknowledgementResponse.Code)
	}

	acknowledgement := authorizedRequest(http.MethodPost, "/v1/messages/"+primaryMessage.ID+"/ack", bob.AccessToken)
	acknowledgementResponse := httptest.NewRecorder()
	server.ServeHTTP(acknowledgementResponse, acknowledgement)
	if acknowledgementResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, acknowledgementResponse.Code)
	}
	if pending := pendingMessages(t, server, bob.AccessToken); len(pending) != 0 {
		t.Fatalf("expected no primary pending messages, got %#v", pending)
	}
	if pending := pendingMessages(t, server, bobMacSession.AccessToken); len(pending) != 1 {
		t.Fatalf("expected secondary message to remain pending, got %#v", pending)
	}
}

func TestMessageFanoutRejectsStaleDeviceCoverage(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	registerDevice(t, server, bob.AccessToken, "Bob Web", "web", 30)
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Envelopes: []messageEnvelopeRequest{
			{RecipientDeviceID: bob.DeviceID, Ciphertext: encodedBytes(41, 48)},
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d: %s", http.StatusConflict, response.Code, response.Body.String())
	}

	legacyRequest := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Ciphertext:        encodedBytes(42, 48),
	})
	legacyResponse := httptest.NewRecorder()
	server.ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected legacy multi-device rejection, got %d", legacyResponse.Code)
	}
}

func TestLegacyMessageContractIsPreservedForOneDevice(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	ciphertext := encodedBytes(41, 48)
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/messages", alice.AccessToken, sendMessageRequest{
		RecipientUsername: "bob",
		Ciphertext:        ciphertext,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, response.Code, response.Body.String())
	}
	var sent messageResponse
	decodeResponse(t, response, &sent)
	if sent.RecipientDeviceID != bob.DeviceID || sent.Ciphertext != ciphertext {
		t.Fatalf("unexpected legacy response: %#v", sent)
	}
}

func TestRegistrationRejectsInvalidSignedPreKey(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	keys := testPreKeyMaterial(10)
	keys.SignedPreKeySignature = encodedBytes(99, ed25519.SignatureSize)
	request := jsonRequest(t, http.MethodPost, "/v1/auth/register", registerRequest{
		Email:    "alice@example.com",
		Username: "alice",
		Password: testPassword,
		Device: deviceRegistrationRequest{
			Name:      "Alice iPhone",
			Platform:  "ios",
			KeyBundle: keys,
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestPreKeyLookupConsumesOneTimeKeysOnce(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)

	first := keyBundles(t, server, alice.AccessToken, "bob")
	second := keyBundles(t, server, alice.AccessToken, "bob")
	third := keyBundles(t, server, alice.AccessToken, "bob")
	if first.UserID != bob.UserID || first.Username != "bob" || len(first.Devices) != 1 || first.Devices[0].DeviceID != bob.DeviceID {
		t.Fatalf("unexpected first bundle: %#v", first)
	}
	if first.Devices[0].OneTimePreKey == nil || first.Devices[0].OneTimePreKey.ID != 1 {
		t.Fatalf("expected first one-time prekey, got %#v", first.Devices[0].OneTimePreKey)
	}
	if second.Devices[0].OneTimePreKey == nil || second.Devices[0].OneTimePreKey.ID != 2 {
		t.Fatalf("expected second one-time prekey, got %#v", second.Devices[0].OneTimePreKey)
	}
	if third.Devices[0].OneTimePreKey != nil {
		t.Fatalf("expected exhausted one-time prekeys, got %#v", third.Devices[0].OneTimePreKey)
	}
	if first.Devices[0].IdentityEncryptionPublic != encodedBytes(20, 32) {
		t.Fatalf("unexpected identity key: %#v", first.Devices[0])
	}
}

func TestPreKeyReplacementIsScopedToCurrentDevice(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	aliceMac := registerDevice(t, server, alice.AccessToken, "Alice Mac", "macos", 20)
	bob := registerUser(t, server, "bob", "Bob iPhone", 30)
	replacement := testPreKeyMaterial(40)
	request := authorizedJSONRequest(t, http.MethodPut, "/v1/devices/me/prekeys", alice.AccessToken, replacement)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, response.Code, response.Body.String())
	}
	bundles := keyBundles(t, server, bob.AccessToken, "alice")
	if len(bundles.Devices) != 2 {
		t.Fatalf("expected two device bundles, got %#v", bundles.Devices)
	}
	phoneBundle := keyBundleForDevice(t, bundles.Devices, alice.DeviceID)
	macBundle := keyBundleForDevice(t, bundles.Devices, aliceMac.ID)
	if phoneBundle.IdentityEncryptionPublic != encodedBytes(40, 32) {
		t.Fatalf("primary prekeys were not replaced: %#v", phoneBundle)
	}
	if macBundle.IdentityEncryptionPublic != encodedBytes(20, 32) {
		t.Fatalf("secondary prekeys were modified: %#v", macBundle)
	}
}

func TestOneTimePreKeyReplenishmentIsIncrementalAndIdempotent(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	bundles := keyBundles(t, server, alice.AccessToken, "bob")
	if bundles.Devices[0].OneTimePreKeysRemaining != 1 {
		t.Fatalf("expected one remaining prekey, got %#v", bundles.Devices[0])
	}

	statusRequest := authorizedRequest(http.MethodGet, "/v1/devices/me/prekeys", bob.AccessToken)
	statusResponse := httptest.NewRecorder()
	server.ServeHTTP(statusResponse, statusRequest)
	var initialStatus preKeyStatusResponse
	decodeResponse(t, statusResponse, &initialStatus)
	if statusResponse.Code != http.StatusOK || initialStatus.OneTimePreKeys != 1 {
		t.Fatalf("unexpected prekey status: %d %#v", statusResponse.Code, initialStatus)
	}

	replenishment := preKeyReplenishmentRequest{OneTimePreKeys: []oneTimePreKeyRequest{
		{ID: 3, PublicKey: encodedBytes(31, 32)},
		{ID: 4, PublicKey: encodedBytes(32, 32)},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		request := authorizedJSONRequest(
			t,
			http.MethodPost,
			"/v1/devices/me/prekeys",
			bob.AccessToken,
			replenishment,
		)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		var status preKeyStatusResponse
		decodeResponse(t, response, &status)
		if response.Code != http.StatusOK || status.OneTimePreKeys != 3 {
			t.Fatalf("unexpected replenishment attempt %d: %d %#v", attempt, response.Code, status)
		}
	}

	conflictRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/devices/me/prekeys",
		bob.AccessToken,
		preKeyReplenishmentRequest{OneTimePreKeys: []oneTimePreKeyRequest{
			{ID: 3, PublicKey: encodedBytes(99, 32)},
		}},
	)
	conflictResponse := httptest.NewRecorder()
	server.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("expected prekey identifier conflict, got %d", conflictResponse.Code)
	}
}

func TestDeviceRegistrationListingRevocationAndTokenInvalidation(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	aliceWeb := registerDevice(t, server, alice.AccessToken, "Alice Web", "web", 20)
	aliceWebSession := loginUser(t, server, "alice", aliceWeb.ID)

	listRequest := authorizedRequest(http.MethodGet, "/v1/devices", alice.AccessToken)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, listResponse.Code)
	}
	var devices []deviceResponse
	decodeResponse(t, listResponse, &devices)
	if len(devices) != 2 || !deviceForID(t, devices, alice.DeviceID).IsCurrent || deviceForID(t, devices, aliceWeb.ID).IsCurrent {
		t.Fatalf("unexpected devices: %#v", devices)
	}

	revokeRequest := authorizedRequest(http.MethodDelete, "/v1/devices/"+aliceWeb.ID, alice.AccessToken)
	revokeResponse := httptest.NewRecorder()
	server.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, revokeResponse.Code, revokeResponse.Body.String())
	}
	revokedTokenRequest := authorizedRequest(http.MethodGet, "/v1/devices", aliceWebSession.AccessToken)
	revokedTokenResponse := httptest.NewRecorder()
	server.ServeHTTP(revokedTokenResponse, revokedTokenRequest)
	if revokedTokenResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token rejection, got %d", revokedTokenResponse.Code)
	}
	revokedRefreshRequest := jsonRequest(
		t,
		http.MethodPost,
		"/v1/auth/refresh",
		refreshRequest{RefreshToken: aliceWebSession.RefreshToken},
	)
	revokedRefreshResponse := httptest.NewRecorder()
	server.ServeHTTP(revokedRefreshResponse, revokedRefreshRequest)
	if revokedRefreshResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked device refresh rejection, got %d", revokedRefreshResponse.Code)
	}
	lastDeviceRequest := authorizedRequest(http.MethodDelete, "/v1/devices/"+alice.DeviceID, alice.AccessToken)
	lastDeviceResponse := httptest.NewRecorder()
	server.ServeHTTP(lastDeviceResponse, lastDeviceRequest)
	if lastDeviceResponse.Code != http.StatusConflict {
		t.Fatalf("expected last-device conflict, got %d", lastDeviceResponse.Code)
	}
}

func TestLoginRequiresDeviceWhenMultipleAreActive(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	registerDevice(t, server, alice.AccessToken, "Alice Mac", "macos", 20)
	request := jsonRequest(t, http.MethodPost, "/v1/auth/login", loginRequest{
		Username: "alice",
		Password: testPassword,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestRefreshTokenRotationDetectsReplay(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	firstRefreshRequest := jsonRequest(t, http.MethodPost, "/v1/auth/refresh", refreshRequest{RefreshToken: alice.RefreshToken})
	firstRefreshResponse := httptest.NewRecorder()
	server.ServeHTTP(firstRefreshResponse, firstRefreshRequest)
	if firstRefreshResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, firstRefreshResponse.Code, firstRefreshResponse.Body.String())
	}
	var rotated authResponse
	decodeResponse(t, firstRefreshResponse, &rotated)
	if rotated.AccessToken == alice.AccessToken || rotated.RefreshToken == alice.RefreshToken || rotated.DeviceID != alice.DeviceID {
		t.Fatalf("unexpected rotated credentials: %#v", rotated)
	}
	replayRequest := jsonRequest(t, http.MethodPost, "/v1/auth/refresh", refreshRequest{RefreshToken: alice.RefreshToken})
	replayResponse := httptest.NewRecorder()
	server.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay rejection, got %d", replayResponse.Code)
	}
	compromisedRequest := jsonRequest(t, http.MethodPost, "/v1/auth/refresh", refreshRequest{RefreshToken: rotated.RefreshToken})
	compromisedResponse := httptest.NewRecorder()
	server.ServeHTTP(compromisedResponse, compromisedRequest)
	if compromisedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected refresh family revocation, got %d", compromisedResponse.Code)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	logoutRequest := jsonRequest(t, http.MethodPost, "/v1/auth/logout", refreshRequest{RefreshToken: alice.RefreshToken})
	logoutResponse := httptest.NewRecorder()
	server.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, logoutResponse.Code)
	}
	refreshRequest := jsonRequest(t, http.MethodPost, "/v1/auth/refresh", refreshRequest{RefreshToken: alice.RefreshToken})
	refreshResponse := httptest.NewRecorder()
	server.ServeHTTP(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked refresh token rejection, got %d", refreshResponse.Code)
	}
}

func TestDeviceLinkApprovalAndAtomicClaim(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	createRequest := jsonRequest(t, http.MethodPost, "/v1/device-links", createDeviceLinkRequest{
		LinkingPublicKey: encodedBytes(70, 32),
	})
	createResponse := httptest.NewRecorder()
	server.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, createResponse.Code, createResponse.Body.String())
	}
	var link createDeviceLinkResponse
	decodeResponse(t, createResponse, &link)
	if link.ID == "" || link.ApprovalSecret == "" || link.ClaimToken == "" {
		t.Fatalf("unexpected device link: %#v", link)
	}

	pendingRequest := jsonRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/status",
		deviceLinkStatusRequest{ClaimToken: link.ClaimToken},
	)
	pendingResponse := httptest.NewRecorder()
	server.ServeHTTP(pendingResponse, pendingRequest)
	var pending deviceLinkStatusResponse
	decodeResponse(t, pendingResponse, &pending)
	if pendingResponse.Code != http.StatusOK || pending.Status != "pending" {
		t.Fatalf("unexpected pending status: %d %#v", pendingResponse.Code, pending)
	}

	encryptedTransfer := encodedBytes(90, 48)
	approveRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/approve",
		alice.AccessToken,
		approveDeviceLinkRequest{
			ApprovalSecret:    link.ApprovalSecret,
			LinkingPublicKey:  link.LinkingPublicKey,
			EncryptedTransfer: encryptedTransfer,
		},
	)
	approveResponse := httptest.NewRecorder()
	server.ServeHTTP(approveResponse, approveRequest)
	if approveResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNoContent, approveResponse.Code, approveResponse.Body.String())
	}

	claimRequestBody := claimDeviceLinkRequest{
		ClaimToken: link.ClaimToken,
		Device: deviceRegistrationRequest{
			Name:      "Alice Mac",
			Platform:  "macos",
			KeyBundle: testPreKeyMaterial(30),
		},
	}
	claimRequest := jsonRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/claim",
		claimRequestBody,
	)
	claimResponseRecorder := httptest.NewRecorder()
	server.ServeHTTP(claimResponseRecorder, claimRequest)
	if claimResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, claimResponseRecorder.Code, claimResponseRecorder.Body.String())
	}
	var claimed claimDeviceLinkResponse
	decodeResponse(t, claimResponseRecorder, &claimed)
	if claimed.EncryptedTransfer != encryptedTransfer || claimed.Session.DeviceID == alice.DeviceID || claimed.Session.RefreshToken == "" {
		t.Fatalf("unexpected device link claim: %#v", claimed)
	}

	listRequest := authorizedRequest(http.MethodGet, "/v1/devices", claimed.Session.AccessToken)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	var devices []deviceResponse
	decodeResponse(t, listResponse, &devices)
	if listResponse.Code != http.StatusOK || len(devices) != 2 {
		t.Fatalf("unexpected linked devices: %d %#v", listResponse.Code, devices)
	}

	replayRequest := jsonRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/claim",
		claimRequestBody,
	)
	replayResponse := httptest.NewRecorder()
	server.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusConflict {
		t.Fatalf("expected one-time claim conflict, got %d", replayResponse.Code)
	}
}

func TestDeviceLinkCredentialsAreSeparated(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	createRequest := jsonRequest(t, http.MethodPost, "/v1/device-links", createDeviceLinkRequest{
		LinkingPublicKey: encodedBytes(70, 32),
	})
	createResponse := httptest.NewRecorder()
	server.ServeHTTP(createResponse, createRequest)
	var link createDeviceLinkResponse
	decodeResponse(t, createResponse, &link)
	wrongApprovalRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/approve",
		alice.AccessToken,
		approveDeviceLinkRequest{
			ApprovalSecret:   link.ClaimToken,
			LinkingPublicKey: link.LinkingPublicKey,
		},
	)
	wrongApprovalResponse := httptest.NewRecorder()
	server.ServeHTTP(wrongApprovalResponse, wrongApprovalRequest)
	if wrongApprovalResponse.Code != http.StatusNotFound {
		t.Fatalf("expected separated approval credential rejection, got %d", wrongApprovalResponse.Code)
	}
	tamperedKeyRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/approve",
		alice.AccessToken,
		approveDeviceLinkRequest{
			ApprovalSecret:   link.ApprovalSecret,
			LinkingPublicKey: encodedBytes(71, 32),
		},
	)
	tamperedKeyResponse := httptest.NewRecorder()
	server.ServeHTTP(tamperedKeyResponse, tamperedKeyRequest)
	if tamperedKeyResponse.Code != http.StatusNotFound {
		t.Fatalf("expected linking key tamper rejection, got %d", tamperedKeyResponse.Code)
	}
	wrongStatusRequest := jsonRequest(
		t,
		http.MethodPost,
		"/v1/device-links/"+link.ID+"/status",
		deviceLinkStatusRequest{ClaimToken: link.ApprovalSecret},
	)
	wrongStatusResponse := httptest.NewRecorder()
	server.ServeHTTP(wrongStatusResponse, wrongStatusRequest)
	if wrongStatusResponse.Code != http.StatusNotFound {
		t.Fatalf("expected separated claim credential rejection, got %d", wrongStatusResponse.Code)
	}
}

func TestGroupMembershipRevisionAndEncryptedFanout(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice iPhone", 10)
	bob := registerUser(t, server, "bob", "Bob iPhone", 20)
	charlie := registerUser(t, server, "charlie", "Charlie iPhone", 30)

	createRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups",
		alice.AccessToken,
		createGroupRequest{MemberUsernames: []string{"bob"}},
	)
	createResponse := httptest.NewRecorder()
	server.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, createResponse.Code, createResponse.Body.String())
	}
	var group groupResponse
	decodeResponse(t, createResponse, &group)
	if group.Revision != 1 || group.OwnerUsername != "alice" || len(group.Members) != 2 {
		t.Fatalf("unexpected group: %#v", group)
	}

	devices := groupDevices(t, server, alice.AccessToken, group.ID)
	if devices.Revision != 1 || len(devices.Devices) != 1 || devices.Devices[0].UserID != bob.UserID || devices.Devices[0].DeviceID != bob.DeviceID {
		t.Fatalf("unexpected group devices: %#v", devices)
	}
	firstSend := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups/"+group.ID+"/messages",
		alice.AccessToken,
		sendGroupMessageRequest{
			Revision: 1,
			Envelopes: []messageEnvelopeRequest{
				{RecipientDeviceID: bob.DeviceID, Ciphertext: encodedBytes(40, 48)},
			},
		},
	)
	firstSendResponse := httptest.NewRecorder()
	server.ServeHTTP(firstSendResponse, firstSend)
	if firstSendResponse.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d: %s", http.StatusAccepted, firstSendResponse.Code, firstSendResponse.Body.String())
	}
	bobPending := pendingMessages(t, server, bob.AccessToken)
	if len(bobPending) != 1 || bobPending[0].GroupID != group.ID || bobPending[0].GroupRevision != 1 {
		t.Fatalf("unexpected group message: %#v", bobPending)
	}

	addRequest := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups/"+group.ID+"/members",
		alice.AccessToken,
		groupMembersRequest{MemberUsernames: []string{"charlie"}},
	)
	addResponse := httptest.NewRecorder()
	server.ServeHTTP(addResponse, addRequest)
	if addResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, addResponse.Code, addResponse.Body.String())
	}
	decodeResponse(t, addResponse, &group)
	if group.Revision != 2 || len(group.Members) != 3 {
		t.Fatalf("unexpected expanded group: %#v", group)
	}

	staleSend := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups/"+group.ID+"/messages",
		alice.AccessToken,
		sendGroupMessageRequest{
			Revision: 1,
			Envelopes: []messageEnvelopeRequest{
				{RecipientDeviceID: bob.DeviceID, Ciphertext: encodedBytes(41, 48)},
			},
		},
	)
	staleResponse := httptest.NewRecorder()
	server.ServeHTTP(staleResponse, staleSend)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("expected stale revision conflict, got %d", staleResponse.Code)
	}

	forbiddenAdd := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups/"+group.ID+"/members",
		bob.AccessToken,
		groupMembersRequest{MemberUsernames: []string{"alice"}},
	)
	forbiddenResponse := httptest.NewRecorder()
	server.ServeHTTP(forbiddenResponse, forbiddenAdd)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("expected owner-only update rejection, got %d", forbiddenResponse.Code)
	}

	currentDevices := groupDevices(t, server, alice.AccessToken, group.ID)
	if currentDevices.Revision != 2 || len(currentDevices.Devices) != 2 {
		t.Fatalf("unexpected current group devices: %#v", currentDevices)
	}
	currentSend := authorizedJSONRequest(
		t,
		http.MethodPost,
		"/v1/groups/"+group.ID+"/messages",
		alice.AccessToken,
		sendGroupMessageRequest{
			Revision: 2,
			Envelopes: []messageEnvelopeRequest{
				{RecipientDeviceID: bob.DeviceID, Ciphertext: encodedBytes(42, 48)},
				{RecipientDeviceID: charlie.DeviceID, Ciphertext: encodedBytes(43, 48)},
			},
		},
	)
	currentResponse := httptest.NewRecorder()
	server.ServeHTTP(currentResponse, currentSend)
	if currentResponse.Code != http.StatusAccepted {
		t.Fatalf("expected current group fanout, got %d: %s", currentResponse.Code, currentResponse.Body.String())
	}

	transferRequest := authorizedJSONRequest(
		t,
		http.MethodPut,
		"/v1/groups/"+group.ID+"/owner",
		alice.AccessToken,
		groupOwnershipRequest{Username: "bob"},
	)
	transferResponse := httptest.NewRecorder()
	server.ServeHTTP(transferResponse, transferRequest)
	if transferResponse.Code != http.StatusOK {
		t.Fatalf("expected ownership transfer, got %d: %s", transferResponse.Code, transferResponse.Body.String())
	}
	decodeResponse(t, transferResponse, &group)
	if group.OwnerUsername != "bob" || group.Revision != 3 {
		t.Fatalf("unexpected transferred group: %#v", group)
	}

	leaveRequest := authorizedRequest(
		http.MethodDelete,
		"/v1/groups/"+group.ID+"/members/alice",
		alice.AccessToken,
	)
	leaveResponse := httptest.NewRecorder()
	server.ServeHTTP(leaveResponse, leaveRequest)
	if leaveResponse.Code != http.StatusOK {
		t.Fatalf("expected member leave, got %d: %s", leaveResponse.Code, leaveResponse.Body.String())
	}
	formerMemberRequest := authorizedRequest(http.MethodGet, "/v1/groups/"+group.ID, alice.AccessToken)
	formerMemberResponse := httptest.NewRecorder()
	server.ServeHTTP(formerMemberResponse, formerMemberRequest)
	if formerMemberResponse.Code != http.StatusForbidden {
		t.Fatalf("expected former member rejection, got %d", formerMemberResponse.Code)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	request := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestCORSHeadersAndLoginRateLimit(t *testing.T) {
	server := NewServerWithDependencies(
		store.NewMemoryStore(),
		[]byte("test-signing-secret"),
		ServerDependencies{
			RateLimiter:    ratelimit.NewMemoryLimiter(),
			AllowedOrigins: []string{"http://localhost:5173"},
		},
	)
	preflight := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	preflight.Header.Set("Origin", "http://localhost:5173")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflightResponse := httptest.NewRecorder()
	server.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent || preflightResponse.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("unexpected preflight: %d %#v", preflightResponse.Code, preflightResponse.Header())
	}
	disallowed := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	disallowed.Header.Set("Origin", "https://untrusted.example")
	disallowedResponse := httptest.NewRecorder()
	server.ServeHTTP(disallowedResponse, disallowed)
	if disallowedResponse.Code != http.StatusForbidden {
		t.Fatalf("expected disallowed origin rejection, got %d", disallowedResponse.Code)
	}
	for attempt := 0; attempt < 11; attempt++ {
		request := jsonRequest(t, http.MethodPost, "/v1/auth/login", loginRequest{
			Username: "missing",
			Password: testPassword,
		})
		request.RemoteAddr = "203.0.113.5:54321"
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if attempt < 10 && response.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected login attempt %d status %d", attempt, response.Code)
		}
		if attempt == 10 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatalf("expected rate limit on attempt %d, got %d", attempt, response.Code)
		}
	}
	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthResponse := httptest.NewRecorder()
	server.ServeHTTP(healthResponse, health)
	if healthResponse.Header().Get("Cache-Control") != "no-store" || healthResponse.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %#v", healthResponse.Header())
	}
}

func TestWebSocketAcceptsBrowserJWTSubprotocol(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), []byte("test-signing-secret"))
	alice := registerUser(t, server, "alice", "Alice Browser", 10)
	request := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
	protocol := websocketJWTProtocolPrefix + alice.AccessToken
	request.Header.Set("Sec-WebSocket-Protocol", protocol)
	response := httptest.NewRecorder()
	parsed, selectedProtocol, ok := server.authenticateWebSocket(response, request)
	if !ok || parsed.DeviceID != alice.DeviceID || selectedProtocol != protocol {
		t.Fatalf("unexpected websocket authentication: %#v %q %t", parsed, selectedProtocol, ok)
	}
}

func registerUser(t *testing.T, server *Server, username string, deviceName string, seed byte) authResponse {
	t.Helper()
	request := jsonRequest(t, http.MethodPost, "/v1/auth/register", registerRequest{
		Email:    username + "@example.com",
		Username: username,
		Password: testPassword,
		Device: deviceRegistrationRequest{
			Name:      deviceName,
			Platform:  "ios",
			KeyBundle: testPreKeyMaterial(seed),
		},
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var authenticated authResponse
	decodeResponse(t, response, &authenticated)
	if authenticated.DeviceID == "" || authenticated.AccessToken == "" || authenticated.RefreshToken == "" {
		t.Fatal("expected device-bound access and refresh tokens")
	}
	return authenticated
}

func loginUser(t *testing.T, server *Server, username string, deviceID string) authResponse {
	t.Helper()
	request := jsonRequest(t, http.MethodPost, "/v1/auth/login", loginRequest{
		Username: username,
		Password: testPassword,
		DeviceID: deviceID,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var authenticated authResponse
	decodeResponse(t, response, &authenticated)
	return authenticated
}

func registerDevice(t *testing.T, server *Server, token string, name string, platform string, seed byte) deviceResponse {
	t.Helper()
	request := authorizedJSONRequest(t, http.MethodPost, "/v1/devices", token, deviceRegistrationRequest{
		Name:      name,
		Platform:  platform,
		KeyBundle: testPreKeyMaterial(seed),
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	var device deviceResponse
	decodeResponse(t, response, &device)
	return device
}

func pendingMessages(t *testing.T, server *Server, token string) []messageResponse {
	t.Helper()
	request := authorizedRequest(http.MethodGet, "/v1/messages", token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var messages []messageResponse
	decodeResponse(t, response, &messages)
	return messages
}

func keyBundles(t *testing.T, server *Server, token string, username string) keyBundleResponse {
	t.Helper()
	request := authorizedRequest(http.MethodGet, "/v1/keys/"+username, token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var keyBundle keyBundleResponse
	decodeResponse(t, response, &keyBundle)
	return keyBundle
}

func groupDevices(t *testing.T, server *Server, token string, groupID string) groupDevicesResponse {
	t.Helper()
	request := authorizedRequest(http.MethodGet, "/v1/groups/"+groupID+"/devices", token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var devices groupDevicesResponse
	decodeResponse(t, response, &devices)
	return devices
}

func testPreKeyMaterial(seed byte) preKeyMaterialRequest {
	signingPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed + 1}, ed25519.SeedSize))
	signingPublicKey := signingPrivateKey.Public().(ed25519.PublicKey)
	signedPreKeyPublic := bytes.Repeat([]byte{seed + 2}, 32)
	return preKeyMaterialRequest{
		IdentityEncryptionPublic: encodedBytes(seed, 32),
		IdentitySigningPublic:    base64.RawStdEncoding.EncodeToString(signingPublicKey),
		SignedPreKeyID:           1,
		SignedPreKeyPublic:       base64.RawStdEncoding.EncodeToString(signedPreKeyPublic),
		SignedPreKeySignature:    base64.RawStdEncoding.EncodeToString(ed25519.Sign(signingPrivateKey, signedPreKeyPublic)),
		OneTimePreKeys: []oneTimePreKeyRequest{
			{ID: 1, PublicKey: encodedBytes(seed+4, 32)},
			{ID: 2, PublicKey: encodedBytes(seed+5, 32)},
		},
	}
}

func encodedBytes(value byte, count int) string {
	return base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{value}, count))
}

func messageForDevice(t *testing.T, messages []messageResponse, deviceID string) messageResponse {
	t.Helper()
	for _, message := range messages {
		if message.RecipientDeviceID == deviceID {
			return message
		}
	}
	t.Fatalf("message for device %s not found", deviceID)
	return messageResponse{}
}

func deviceForID(t *testing.T, devices []deviceResponse, deviceID string) deviceResponse {
	t.Helper()
	for _, device := range devices {
		if device.ID == deviceID {
			return device
		}
	}
	t.Fatalf("device %s not found", deviceID)
	return deviceResponse{}
}

func keyBundleForDevice(t *testing.T, bundles []consumedPreKeyBundleResponse, deviceID string) consumedPreKeyBundleResponse {
	t.Helper()
	for _, bundle := range bundles {
		if bundle.DeviceID == deviceID {
			return bundle
		}
	}
	t.Fatalf("key bundle for device %s not found", deviceID)
	return consumedPreKeyBundleResponse{}
}

func authorizedJSONRequest(t *testing.T, method string, target string, token string, value any) *http.Request {
	t.Helper()
	request := jsonRequest(t, method, target, value)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func authorizedRequest(method string, target string, token string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func jsonRequest(t *testing.T, method string, target string, value any) *http.Request {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
