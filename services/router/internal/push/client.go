package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 8 << 10

type Dispatcher interface {
	Dispatch(ctx context.Context, userID string, deviceID string) error
}

type Client struct {
	endpoint      string
	internalToken string
	httpClient    *http.Client
}

func NewClient(baseURL string, internalToken string, timeout time.Duration) (*Client, error) {
	return newClient(baseURL, internalToken, timeout, http.DefaultTransport)
}

func NewClientWithTransport(baseURL string, internalToken string, timeout time.Duration, transport http.RoundTripper) (*Client, error) {
	if transport == nil {
		return nil, errors.New("Push client transport is required")
	}
	return newClient(baseURL, internalToken, timeout, transport)
}

func newClient(baseURL string, internalToken string, timeout time.Duration, transport http.RoundTripper) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("invalid Push service URL")
	}
	if len(internalToken) < 32 || timeout <= 0 {
		return nil, errors.New("invalid Push client configuration")
	}
	return &Client{
		endpoint:      strings.TrimSuffix(parsed.String(), "/") + "/internal/v1/push/dispatch",
		internalToken: internalToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (client *Client) Dispatch(ctx context.Context, userID string, deviceID string) error {
	payload, err := json.Marshal(map[string]string{
		"recipient_user_id":   userID,
		"recipient_device_id": deviceID,
		"event":               "message_available",
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Knot-Internal-Token", client.internalToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes)); err != nil {
		return err
	}
	if response.StatusCode != http.StatusAccepted {
		return errors.New("Push service rejected dispatch")
	}
	return nil
}
