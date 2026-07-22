package subscription

import (
	"crypto/elliptic"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
)

const maxEndpointBytes = 2048

func NormalizeAPNSToken(value string) (string, error) {
	if len(value) != 64 {
		return "", errors.New("invalid APNs token")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid APNs token")
	}
	return hex.EncodeToString(decoded), nil
}

func NormalizeWebSubscription(endpoint string, keys WebKeys) (string, WebKeys, error) {
	if len(endpoint) == 0 || len(endpoint) > maxEndpointBytes {
		return "", WebKeys{}, errors.New("invalid Web Push endpoint")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Path == "" {
		return "", WebKeys{}, errors.New("invalid Web Push endpoint")
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return "", WebKeys{}, errors.New("invalid Web Push endpoint")
	}
	if net.ParseIP(parsed.Hostname()) != nil || !asciiHostname(parsed.Hostname()) {
		return "", WebKeys{}, errors.New("invalid Web Push endpoint")
	}
	p256dh, err := decodeWebKey(keys.P256DH)
	if err != nil || len(p256dh) != 65 || p256dh[0] != 4 {
		return "", WebKeys{}, errors.New("invalid Web Push p256dh key")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), p256dh)
	if x == nil || y == nil {
		return "", WebKeys{}, errors.New("invalid Web Push p256dh key")
	}
	auth, err := decodeWebKey(keys.Auth)
	if err != nil || len(auth) != 16 {
		return "", WebKeys{}, errors.New("invalid Web Push auth key")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), WebKeys{
		P256DH: base64.RawURLEncoding.EncodeToString(p256dh),
		Auth:   base64.RawURLEncoding.EncodeToString(auth),
	}, nil
}

func decodeWebKey(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

func asciiHostname(value string) bool {
	if strings.TrimSpace(value) != value || strings.Contains(value, "..") {
		return false
	}
	for _, character := range value {
		if character > 127 || !(character == '.' || character == '-' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return false
		}
	}
	return true
}
