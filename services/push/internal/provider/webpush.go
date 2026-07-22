package provider

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/yaroslavfairfieldd/knot/services/push/internal/subscription"
)

type WebPushConfig struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	Subject         string
	Timeout         time.Duration
}

type WebPushProvider struct {
	options webpush.Options
}

func NewWebPushProvider(config WebPushConfig) (*WebPushProvider, error) {
	return newWebPushProvider(config, newPublicHTTPClient(config.Timeout))
}

func newWebPushProvider(config WebPushConfig, client webpush.HTTPClient) (*WebPushProvider, error) {
	if config.Timeout <= 0 || client == nil || !validVAPIDSubject(config.Subject) {
		return nil, errors.New("invalid Web Push configuration")
	}
	publicKey, err := decodeVAPIDKey(config.VAPIDPublicKey)
	if err != nil || len(publicKey) != 65 || publicKey[0] != 4 {
		return nil, errors.New("invalid VAPID public key")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return nil, errors.New("invalid VAPID public key")
	}
	privateKey, err := decodeVAPIDKey(config.VAPIDPrivateKey)
	if err != nil || len(privateKey) != 32 {
		return nil, errors.New("invalid VAPID private key")
	}
	privateScalar := new(big.Int).SetBytes(privateKey)
	if privateScalar.Sign() <= 0 || privateScalar.Cmp(elliptic.P256().Params().N) >= 0 {
		return nil, errors.New("invalid VAPID private key")
	}
	derivedX, derivedY := elliptic.P256().ScalarBaseMult(privateKey)
	if x.Cmp(derivedX) != 0 || y.Cmp(derivedY) != 0 {
		return nil, errors.New("VAPID key pair does not match")
	}
	subscriber := strings.TrimPrefix(config.Subject, "mailto:")
	return &WebPushProvider{options: webpush.Options{
		HTTPClient:      client,
		Subscriber:      subscriber,
		Topic:           MessageAvailable,
		TTL:             60,
		Urgency:         webpush.UrgencyHigh,
		VAPIDPublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		VAPIDPrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}}, nil
}

func (provider *WebPushProvider) Send(ctx context.Context, target subscription.Subscription, notification Notification) error {
	if target.Channel != subscription.ChannelWeb || target.WebEndpoint == "" || target.WebKeys.P256DH == "" || target.WebKeys.Auth == "" {
		return errors.New("invalid Web Push subscription")
	}
	endpoint, keys, err := subscription.NormalizeWebSubscription(target.WebEndpoint, target.WebKeys)
	if err != nil {
		return errors.New("invalid Web Push subscription")
	}
	payload, err := notification.Payload()
	if err != nil {
		return err
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: endpoint,
		Keys: webpush.Keys{
			P256dh: keys.P256DH,
			Auth:   keys.Auth,
		},
	}, &provider.options)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, readError := io.ReadAll(io.LimitReader(response.Body, maxProviderResponse+1))
	if readError != nil {
		return readError
	}
	if len(body) > maxProviderResponse {
		return errors.New("Web Push response exceeded limit")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return ErrSubscriptionInvalid
	}
	return fmt.Errorf("Web Push rejected delivery with status %d", response.StatusCode)
}

func decodeVAPIDKey(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

func validVAPIDSubject(value string) bool {
	if len(value) == 0 || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme == "mailto" {
		mailbox := parsed.Opaque
		if mailbox == "" {
			mailbox = parsed.Path
		}
		return strings.Contains(mailbox, "@") && !strings.ContainsAny(mailbox, "\r\n")
	}
	return parsed.Scheme == "https" && parsed.Hostname() != ""
}
