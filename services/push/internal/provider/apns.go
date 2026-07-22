package provider

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

const (
	apnsProductionEndpoint = "https://api.push.apple.com"
	apnsSandboxEndpoint    = "https://api.sandbox.push.apple.com"
	apnsTokenLifetime      = 50 * time.Minute
	maxProviderResponse    = 4 << 10
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type APNSConfig struct {
	KeyID       string
	TeamID      string
	Topic       string
	Environment string
	PrivateKey  []byte
	Timeout     time.Duration
}

type APNSProvider struct {
	keyID       string
	teamID      string
	topic       string
	privateKey  *ecdsa.PrivateKey
	endpoint    string
	client      HTTPClient
	now         func() time.Time
	tokenMutex  sync.Mutex
	cachedToken string
	tokenIssued time.Time
}

type apnsPayload struct {
	APS  apnsAPS `json:"aps"`
	Type string  `json:"type"`
}

type apnsAPS struct {
	ContentAvailable int `json:"content-available"`
}

type apnsErrorResponse struct {
	Reason string `json:"reason"`
}

func NewAPNSProvider(config APNSConfig) (*APNSProvider, error) {
	endpoint := apnsProductionEndpoint
	if config.Environment == "sandbox" {
		endpoint = apnsSandboxEndpoint
	} else if config.Environment != "production" {
		return nil, errors.New("invalid APNs environment")
	}
	client := newPublicHTTPClient(config.Timeout)
	return newAPNSProvider(config, endpoint, client, time.Now)
}

func newAPNSProvider(config APNSConfig, endpoint string, client HTTPClient, now func() time.Time) (*APNSProvider, error) {
	if !validAPNSIdentifier(config.KeyID, 10) || !validAPNSIdentifier(config.TeamID, 10) || !validTopic(config.Topic) || config.Timeout <= 0 || client == nil || now == nil {
		return nil, errors.New("invalid APNs configuration")
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" || parsedEndpoint.Host == "" || parsedEndpoint.Path != "" {
		return nil, errors.New("invalid APNs endpoint")
	}
	privateKey, err := parseAPNSPrivateKey(config.PrivateKey)
	if err != nil {
		return nil, err
	}
	return &APNSProvider{
		keyID:      config.KeyID,
		teamID:     config.TeamID,
		topic:      config.Topic,
		privateKey: privateKey,
		endpoint:   strings.TrimSuffix(endpoint, "/"),
		client:     client,
		now:        now,
	}, nil
}

func (provider *APNSProvider) Send(ctx context.Context, target subscription.Subscription, notification Notification) error {
	if target.Channel != subscription.ChannelAPNS || target.APNSToken == "" {
		return errors.New("invalid APNs subscription")
	}
	deviceToken, err := subscription.NormalizeAPNSToken(target.APNSToken)
	if err != nil {
		return errors.New("invalid APNs subscription")
	}
	if notification.Type != MessageAvailable {
		return errors.New("unsupported APNs notification")
	}
	payload, err := json.Marshal(apnsPayload{
		APS:  apnsAPS{ContentAvailable: 1},
		Type: notification.Type,
	})
	if err != nil {
		return err
	}
	token, err := provider.authorizationToken()
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint+"/3/device/"+deviceToken, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("apns-topic", provider.topic)
	request.Header.Set("apns-push-type", "background")
	request.Header.Set("apns-priority", "5")
	request.Header.Set("apns-collapse-id", MessageAvailable)
	response, err := provider.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, readError := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	if readError != nil {
		return readError
	}
	if len(body) > maxProviderResponse {
		return errors.New("APNs response exceeded limit")
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var providerError apnsErrorResponse
	json.Unmarshal(body, &providerError)
	if invalidAPNSResponse(response.StatusCode, providerError.Reason) {
		return ErrSubscriptionInvalid
	}
	return fmt.Errorf("APNs rejected delivery with status %d and reason %s", response.StatusCode, sanitizedReason(providerError.Reason))
}

func (provider *APNSProvider) authorizationToken() (string, error) {
	provider.tokenMutex.Lock()
	defer provider.tokenMutex.Unlock()
	now := provider.now().UTC()
	if provider.cachedToken != "" && !now.Before(provider.tokenIssued) && now.Sub(provider.tokenIssued) < apnsTokenLifetime {
		return provider.cachedToken, nil
	}
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": provider.keyID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{"iss": provider.teamID, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, provider.privateKey, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	provider.cachedToken = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	provider.tokenIssued = now
	return provider.cachedToken, nil
}

func parseAPNSPrivateKey(encoded []byte) (*ecdsa.PrivateKey, error) {
	block, remaining := pem.Decode(encoded)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(remaining)) != 0 {
		return nil, errors.New("invalid APNs private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid APNs private key")
	}
	privateKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || privateKey.Curve != elliptic.P256() {
		return nil, errors.New("invalid APNs private key")
	}
	return privateKey, nil
}

func validAPNSIdentifier(value string, requiredLength int) bool {
	if len(value) != requiredLength {
		return false
	}
	for _, character := range value {
		if !(character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func validTopic(value string) bool {
	if value == "" || len(value) > 255 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, character := range value {
		if !(character == '.' || character == '-' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return false
		}
	}
	return true
}

func invalidAPNSResponse(status int, reason string) bool {
	if status == http.StatusGone {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	return reason == "BadDeviceToken" || reason == "DeviceTokenNotForTopic" || reason == "Unregistered"
}

func sanitizedReason(reason string) string {
	if reason == "" || len(reason) > 128 {
		return "unknown"
	}
	for _, character := range reason {
		if !(character == '_' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return "unknown"
		}
	}
	return reason
}
