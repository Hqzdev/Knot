package queue

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
)

type ResilientQueue struct {
	primary  Queue
	fallback Queue
}

func NewResilientQueue(primary Queue, fallback Queue) (*ResilientQueue, error) {
	if primary == nil || fallback == nil {
		return nil, errors.New("primary and fallback queues are required")
	}
	return &ResilientQueue{primary: primary, fallback: fallback}, nil
}

func (queue *ResilientQueue) Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error) {
	fallbackResult, fallbackError := queue.fallback.Enqueue(ctx, envelope)
	primaryResult, primaryError := queue.primary.Enqueue(ctx, envelope)
	if errors.Is(primaryError, ErrMessageConflict) || errors.Is(fallbackError, ErrMessageConflict) {
		return EnqueueResult{}, ErrMessageConflict
	}
	if fallbackError == nil {
		return EnqueueResult{Duplicate: fallbackResult.Duplicate || primaryResult.Duplicate, Sequence: fallbackResult.Sequence}, nil
	}
	if primaryError == nil {
		return primaryResult, nil
	}
	return EnqueueResult{}, errors.Join(primaryError, fallbackError)
}

func (queue *ResilientQueue) Sync(ctx context.Context, userID string, deviceID string, after Cursor, limit int) ([]Pending, error) {
	primaryValues, primaryError := queue.primary.Sync(ctx, userID, deviceID, after, limit)
	fallbackValues, fallbackError := queue.fallback.Sync(ctx, userID, deviceID, after, limit)
	if primaryError != nil && fallbackError != nil {
		return nil, errors.Join(primaryError, fallbackError)
	}
	merged := make(map[string]Pending, len(primaryValues)+len(fallbackValues))
	for _, value := range primaryValues {
		value.AckHandle = combineHandles(value.AckHandle, "")
		merged[value.Envelope.MessageID] = value
	}
	for _, value := range fallbackValues {
		if existing, found := merged[value.Envelope.MessageID]; found {
			if !sameEnvelope(existing.Envelope, value.Envelope) {
				return nil, ErrMessageConflict
			}
			existing.AckHandle = combineHandles(primaryHandle(existing.AckHandle), value.AckHandle)
			existing.Redelivered = existing.Redelivered || value.Redelivered
			merged[value.Envelope.MessageID] = existing
			continue
		}
		value.AckHandle = combineHandles("", value.AckHandle)
		merged[value.Envelope.MessageID] = value
	}
	values := make([]Pending, 0, len(merged))
	for _, value := range merged {
		values = append(values, value)
	}
	sortPending(values)
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (queue *ResilientQueue) Acknowledge(ctx context.Context, userID string, deviceID string, messageID string, handle string) error {
	primaryValue, fallbackValue, err := splitHandles(handle)
	if err != nil {
		return ErrAckNotFound
	}
	var failures []error
	if primaryValue != "" {
		if err := queue.primary.Acknowledge(ctx, userID, deviceID, messageID, primaryValue); err != nil && !errors.Is(err, ErrAckNotFound) {
			failures = append(failures, err)
		}
	}
	if fallbackValue != "" {
		if err := queue.fallback.Acknowledge(ctx, userID, deviceID, messageID, fallbackValue); err != nil && !errors.Is(err, ErrAckNotFound) {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

func (queue *ResilientQueue) Ping(ctx context.Context) error {
	primaryError := queue.primary.Ping(ctx)
	fallbackError := queue.fallback.Ping(ctx)
	if primaryError == nil || fallbackError == nil {
		return nil
	}
	return errors.Join(primaryError, fallbackError)
}

func combineHandles(primary string, fallback string) string {
	return "n:" + base64.RawURLEncoding.EncodeToString([]byte(primary)) + ";p:" + base64.RawURLEncoding.EncodeToString([]byte(fallback))
}

func splitHandles(value string) (string, string, error) {
	parts := strings.Split(value, ";")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "n:") || !strings.HasPrefix(parts[1], "p:") {
		return "", "", ErrAckNotFound
	}
	primary, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(parts[0], "n:"))
	if err != nil {
		return "", "", ErrAckNotFound
	}
	fallback, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(parts[1], "p:"))
	if err != nil {
		return "", "", ErrAckNotFound
	}
	return string(primary), string(fallback), nil
}

func primaryHandle(value string) string {
	primary, _, _ := splitHandles(value)
	return primary
}
