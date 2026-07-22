package token

import (
	"strings"
	"testing"
	"time"
)

func TestSignerBindsCursorAndAcknowledgement(t *testing.T) {
	signer, err := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(1_700_000_000, 123_000_000).UTC()
	cursorToken, err := signer.Cursor("user", "device", Cursor{CreatedAt: createdAt, MessageID: "message"})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := signer.ParseCursor(cursorToken, "user", "device")
	if err != nil || !cursor.CreatedAt.Equal(createdAt) || cursor.MessageID != "message" {
		t.Fatalf("unexpected cursor: %#v %v", cursor, err)
	}
	if _, err := signer.ParseCursor(cursorToken, "user", "other-device"); err == nil {
		t.Fatal("cursor escaped device binding")
	}
	ackToken, err := signer.Acknowledgement("user", "device", "message", "backend-handle")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := signer.ParseAcknowledgement(ackToken, "user", "device", "message")
	if err != nil || handle != "backend-handle" {
		t.Fatalf("unexpected acknowledgement: %q %v", handle, err)
	}
	if _, err := signer.ParseAcknowledgement(ackToken, "user", "device", "other-message"); err == nil {
		t.Fatal("acknowledgement escaped message binding")
	}
	tampered := ackToken[:len(ackToken)-1] + strings.ToUpper(ackToken[len(ackToken)-1:])
	if tampered == ackToken {
		tampered = ackToken[:len(ackToken)-1] + "A"
	}
	if _, err := signer.ParseAcknowledgement(tampered, "user", "device", "message"); err == nil {
		t.Fatal("tampered acknowledgement accepted")
	}
}
