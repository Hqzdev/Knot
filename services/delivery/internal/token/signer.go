package token

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

const (
	version         = byte(1)
	cursorKind      = byte(1)
	ackKind         = byte(2)
	maxTokenBytes   = 2048
	maxIdentitySize = 128
	maxHandleSize   = 1024
)

var ErrInvalid = errors.New("invalid delivery token")

type Cursor struct {
	CreatedAt time.Time
	MessageID string
}

type Signer struct {
	secret []byte
}

func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) < 32 {
		return nil, errors.New("delivery token secret must contain at least 32 bytes")
	}
	return &Signer{secret: append([]byte(nil), secret...)}, nil
}

func (signer *Signer) Cursor(userID string, deviceID string, cursor Cursor) (string, error) {
	return signer.encode(cursorKind, userID, deviceID, cursor.MessageID, "", cursor.CreatedAt.UnixMilli())
}

func (signer *Signer) ParseCursor(value string, userID string, deviceID string) (Cursor, error) {
	parsed, err := signer.decode(value, cursorKind, userID, deviceID)
	if err != nil {
		return Cursor{}, err
	}
	if parsed.handle != "" || parsed.timestamp < 0 {
		return Cursor{}, ErrInvalid
	}
	return Cursor{CreatedAt: time.UnixMilli(parsed.timestamp).UTC(), MessageID: parsed.messageID}, nil
}

func (signer *Signer) Acknowledgement(userID string, deviceID string, messageID string, handle string) (string, error) {
	return signer.encode(ackKind, userID, deviceID, messageID, handle, 0)
}

func (signer *Signer) ParseAcknowledgement(value string, userID string, deviceID string, messageID string) (string, error) {
	parsed, err := signer.decode(value, ackKind, userID, deviceID)
	if err != nil || parsed.messageID != messageID || parsed.handle == "" || parsed.timestamp != 0 {
		return "", ErrInvalid
	}
	return parsed.handle, nil
}

type parsedToken struct {
	messageID string
	handle    string
	timestamp int64
}

func (signer *Signer) encode(kind byte, userID string, deviceID string, messageID string, handle string, timestamp int64) (string, error) {
	if !validLength(userID, maxIdentitySize) || !validLength(deviceID, maxIdentitySize) || len(messageID) > maxIdentitySize || len(handle) > maxHandleSize {
		return "", ErrInvalid
	}
	payload := bytes.NewBuffer(make([]byte, 0, 32+len(userID)+len(deviceID)+len(messageID)+len(handle)))
	payload.WriteByte(version)
	payload.WriteByte(kind)
	binary.Write(payload, binary.BigEndian, timestamp)
	writeValue(payload, userID)
	writeValue(payload, deviceID)
	writeValue(payload, messageID)
	writeValue(payload, handle)
	mac := hmac.New(sha256.New, signer.secret)
	mac.Write(payload.Bytes())
	payload.Write(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(payload.Bytes()), nil
}

func (signer *Signer) decode(value string, expectedKind byte, userID string, deviceID string) (parsedToken, error) {
	if value == "" || len(value) > maxTokenBytes {
		return parsedToken{}, ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) < 2+8+8+sha256.Size {
		return parsedToken{}, ErrInvalid
	}
	payload := decoded[:len(decoded)-sha256.Size]
	provided := decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, signer.secret)
	mac.Write(payload)
	if subtle.ConstantTimeCompare(provided, mac.Sum(nil)) != 1 {
		return parsedToken{}, ErrInvalid
	}
	reader := bytes.NewReader(payload)
	parsedVersion, _ := reader.ReadByte()
	kind, _ := reader.ReadByte()
	if parsedVersion != version || kind != expectedKind {
		return parsedToken{}, ErrInvalid
	}
	var timestamp int64
	if binary.Read(reader, binary.BigEndian, &timestamp) != nil {
		return parsedToken{}, ErrInvalid
	}
	parsedUserID, err := readValue(reader, maxIdentitySize)
	if err != nil {
		return parsedToken{}, ErrInvalid
	}
	parsedDeviceID, err := readValue(reader, maxIdentitySize)
	if err != nil {
		return parsedToken{}, ErrInvalid
	}
	messageID, err := readValue(reader, maxIdentitySize)
	if err != nil {
		return parsedToken{}, ErrInvalid
	}
	handle, err := readValue(reader, maxHandleSize)
	if err != nil || reader.Len() != 0 {
		return parsedToken{}, ErrInvalid
	}
	if subtle.ConstantTimeCompare([]byte(parsedUserID), []byte(userID)) != 1 || subtle.ConstantTimeCompare([]byte(parsedDeviceID), []byte(deviceID)) != 1 {
		return parsedToken{}, ErrInvalid
	}
	return parsedToken{messageID: messageID, handle: handle, timestamp: timestamp}, nil
}

func writeValue(buffer *bytes.Buffer, value string) {
	binary.Write(buffer, binary.BigEndian, uint16(len(value)))
	buffer.WriteString(value)
}

func readValue(reader *bytes.Reader, maximum int) (string, error) {
	var length uint16
	if binary.Read(reader, binary.BigEndian, &length) != nil || int(length) > maximum || reader.Len() < int(length) {
		return "", ErrInvalid
	}
	if length == 0 {
		return "", nil
	}
	value := make([]byte, int(length))
	if _, err := reader.Read(value); err != nil {
		return "", ErrInvalid
	}
	return string(value), nil
}

func validLength(value string, maximum int) bool {
	return value != "" && len(value) <= maximum
}
