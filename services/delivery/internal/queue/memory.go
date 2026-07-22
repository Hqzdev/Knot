package queue

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"sync"
	"time"
)

type memoryRecord struct {
	envelope      Envelope
	sequence      uint64
	availableAt   time.Time
	deliveryCount uint64
	acked         bool
}

type MemoryQueue struct {
	mutex        sync.Mutex
	records      map[string][]*memoryRecord
	dedup        map[string]*memoryRecord
	sequences    map[string]uint64
	now          func() time.Time
	ackWait      time.Duration
	retention    time.Duration
	maxPerDevice int
}

func NewMemoryQueue(ackWait time.Duration, retention time.Duration, maxPerDevice int) *MemoryQueue {
	return &MemoryQueue{
		records:      make(map[string][]*memoryRecord),
		dedup:        make(map[string]*memoryRecord),
		sequences:    make(map[string]uint64),
		now:          time.Now,
		ackWait:      ackWait,
		retention:    retention,
		maxPerDevice: maxPerDevice,
	}
}

func (queue *MemoryQueue) Enqueue(ctx context.Context, envelope Envelope) (EnqueueResult, error) {
	if err := ctx.Err(); err != nil {
		return EnqueueResult{}, err
	}
	deviceKey := memoryDeviceKey(envelope.RecipientUserID, envelope.RecipientDeviceID)
	dedupKey := memoryDedupKey(deviceKey, envelope.MessageID)
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	queue.purgeLocked(deviceKey)
	if existing, found := queue.dedup[dedupKey]; found {
		if !sameEnvelope(existing.envelope, envelope) {
			return EnqueueResult{}, ErrMessageConflict
		}
		return EnqueueResult{Duplicate: true, Sequence: existing.sequence}, nil
	}
	queue.sequences[deviceKey]++
	record := &memoryRecord{envelope: cloneEnvelope(envelope), sequence: queue.sequences[deviceKey]}
	queue.records[deviceKey] = append(queue.records[deviceKey], record)
	queue.dedup[dedupKey] = record
	queue.enforceLimitLocked(deviceKey)
	return EnqueueResult{Sequence: record.sequence}, nil
}

func (queue *MemoryQueue) Sync(ctx context.Context, userID string, deviceID string, after Cursor, limit int) ([]Pending, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deviceKey := memoryDeviceKey(userID, deviceID)
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	queue.purgeLocked(deviceKey)
	now := queue.now().UTC()
	result := make([]Pending, 0, limit)
	for _, record := range queue.records[deviceKey] {
		if record.acked {
			continue
		}
		redeliveryDue := record.deliveryCount > 0 && !record.availableAt.After(now)
		if !afterCursor(after, record.envelope) && !redeliveryDue {
			continue
		}
		if record.deliveryCount > 0 && record.availableAt.After(now) {
			continue
		}
		result = append(result, Pending{
			Envelope:    cloneEnvelope(record.envelope),
			Cursor:      Cursor{CreatedAt: record.envelope.CreatedAt, MessageID: record.envelope.MessageID},
			AckHandle:   strconv.FormatUint(record.sequence, 10),
			Redelivered: record.deliveryCount > 0,
		})
		record.deliveryCount++
		record.availableAt = now.Add(queue.ackWait)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (queue *MemoryQueue) Acknowledge(ctx context.Context, userID string, deviceID string, messageID string, handle string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sequence, err := strconv.ParseUint(handle, 10, 64)
	if err != nil {
		return ErrAckNotFound
	}
	deviceKey := memoryDeviceKey(userID, deviceID)
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	for _, record := range queue.records[deviceKey] {
		if record.envelope.MessageID != messageID || record.sequence != sequence {
			continue
		}
		record.acked = true
		return nil
	}
	return ErrAckNotFound
}

func (queue *MemoryQueue) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (queue *MemoryQueue) purgeLocked(deviceKey string) {
	if queue.retention <= 0 {
		return
	}
	threshold := queue.now().UTC().Add(-queue.retention)
	records := queue.records[deviceKey]
	retained := records[:0]
	for _, record := range records {
		if record.envelope.CreatedAt.Before(threshold) {
			delete(queue.dedup, memoryDedupKey(deviceKey, record.envelope.MessageID))
			continue
		}
		retained = append(retained, record)
	}
	queue.records[deviceKey] = retained
}

func (queue *MemoryQueue) enforceLimitLocked(deviceKey string) {
	if queue.maxPerDevice <= 0 {
		return
	}
	records := queue.records[deviceKey]
	if len(records) <= queue.maxPerDevice {
		return
	}
	removeCount := len(records) - queue.maxPerDevice
	for _, record := range records[:removeCount] {
		delete(queue.dedup, memoryDedupKey(deviceKey, record.envelope.MessageID))
	}
	queue.records[deviceKey] = records[removeCount:]
}

func memoryDeviceKey(userID string, deviceID string) string {
	return userID + "\x00" + deviceID
}

func memoryDedupKey(deviceKey string, messageID string) string {
	return deviceKey + "\x00" + messageID
}

func cloneEnvelope(envelope Envelope) Envelope {
	cloned := envelope
	cloned.Ciphertext = append([]byte(nil), envelope.Ciphertext...)
	return cloned
}

func sameEnvelope(left Envelope, right Envelope) bool {
	return left.MessageID == right.MessageID &&
		left.RecipientUserID == right.RecipientUserID &&
		left.RecipientDeviceID == right.RecipientDeviceID &&
		left.SenderUserID == right.SenderUserID &&
		left.SenderDeviceID == right.SenderDeviceID &&
		left.SenderUsername == right.SenderUsername &&
		left.GroupID == right.GroupID &&
		left.GroupRevision == right.GroupRevision &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		bytes.Equal(left.Ciphertext, right.Ciphertext)
}

func sortPending(values []Pending) {
	sort.Slice(values, func(left int, right int) bool {
		if values[left].Cursor.CreatedAt.Equal(values[right].Cursor.CreatedAt) {
			return values[left].Cursor.MessageID < values[right].Cursor.MessageID
		}
		return values[left].Cursor.CreatedAt.Before(values[right].Cursor.CreatedAt)
	})
}
