package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"sort"
)

func envelopeDigest(envelopes []Envelope) [sha256.Size]byte {
	values := append([]Envelope(nil), envelopes...)
	sort.Slice(values, func(left int, right int) bool {
		return values[left].DeviceID < values[right].DeviceID
	})
	digest := sha256.New()
	for _, value := range values {
		binary.Write(digest, binary.BigEndian, uint32(len(value.DeviceID)))
		digest.Write([]byte(value.DeviceID))
		binary.Write(digest, binary.BigEndian, uint32(len(value.Ciphertext)))
		digest.Write(value.Ciphertext)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func randomClaim() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func exactDeviceSet(deviceIDs []string, envelopes []Envelope) bool {
	if len(deviceIDs) == 0 || len(deviceIDs) != len(envelopes) {
		return false
	}
	expected := make(map[string]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		expected[deviceID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if _, exists := expected[envelope.DeviceID]; !exists {
			return false
		}
		if _, duplicate := seen[envelope.DeviceID]; duplicate {
			return false
		}
		seen[envelope.DeviceID] = struct{}{}
	}
	return len(seen) == len(expected)
}
