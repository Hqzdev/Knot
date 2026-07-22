package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pushInternalHeader = "X-Knot-Internal-Token"

type PushClient interface {
	MessageAvailable(ctx context.Context, userID string, deviceID string) error
	RevokeDevice(ctx context.Context, userID string, deviceID string) error
}

type HTTPPushClient struct {
	baseURL       *url.URL
	internalToken string
	httpClient    *http.Client
}

type pushDispatchRequest struct {
	RecipientUserID   string `json:"recipient_user_id"`
	RecipientDeviceID string `json:"recipient_device_id"`
	Event             string `json:"event"`
}

type pushRevocationRequest struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

func NewHTTPPushClient(rawBaseURL string, internalToken string, httpClient *http.Client) (*HTTPPushClient, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("invalid push service URL")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("invalid push service URL")
	}
	if len(internalToken) < 32 || strings.TrimSpace(internalToken) != internalToken {
		return nil, errors.New("invalid push internal token")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPPushClient{
		baseURL:       baseURL,
		internalToken: internalToken,
		httpClient:    httpClient,
	}, nil
}

func (client *HTTPPushClient) MessageAvailable(ctx context.Context, userID string, deviceID string) error {
	return client.send(ctx, http.MethodPost, "internal/v1/push/dispatch", pushDispatchRequest{
		RecipientUserID:   userID,
		RecipientDeviceID: deviceID,
		Event:             "message_available",
	})
}

func (client *HTTPPushClient) RevokeDevice(ctx context.Context, userID string, deviceID string) error {
	return client.send(ctx, http.MethodDelete, "internal/v1/push/subscriptions", pushRevocationRequest{
		UserID:   userID,
		DeviceID: deviceID,
	})
}

func (client *HTTPPushClient) send(ctx context.Context, method string, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.JoinPath(path).String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(pushInternalHeader, client.internalToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("push service returned status %d", response.StatusCode)
	}
	return nil
}
