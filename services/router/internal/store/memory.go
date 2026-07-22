package store

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type memoryRoute struct {
	senderUserID    string
	senderDeviceID  string
	recipientUserID string
	groupID         string
	groupRevision   uint64
	digest          [32]byte
	claimToken      string
	claimUntil      time.Time
	completed       bool
}

type memoryGroup struct {
	revision uint64
	members  map[string]struct{}
}

type MemoryStore struct {
	mutex   sync.Mutex
	devices map[string]map[string]bool
	users   map[string]string
	groups  map[string]memoryGroup
	routes  map[string]*memoryRoute
	now     func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{devices: make(map[string]map[string]bool), users: make(map[string]string), groups: make(map[string]memoryGroup), routes: make(map[string]*memoryRoute), now: time.Now}
}

func (store *MemoryStore) SetDevice(userID string, deviceID string, active bool) {
	store.mutex.Lock()
	if store.devices[userID] == nil {
		store.devices[userID] = make(map[string]bool)
	}
	if store.users[userID] == "" {
		store.users[userID] = userID
	}
	store.devices[userID][deviceID] = active
	store.mutex.Unlock()
}

func (store *MemoryStore) SetUsername(userID string, username string) {
	store.mutex.Lock()
	store.users[userID] = username
	store.mutex.Unlock()
}

func (store *MemoryStore) SetGroup(groupID string, revision uint64, userIDs ...string) {
	members := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		members[userID] = struct{}{}
	}
	store.mutex.Lock()
	store.groups[groupID] = memoryGroup{revision: revision, members: members}
	store.mutex.Unlock()
}

func (store *MemoryStore) ActiveDevice(ctx context.Context, userID string, deviceID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	store.mutex.Lock()
	active := store.devices[userID][deviceID]
	store.mutex.Unlock()
	return active, nil
}

func (store *MemoryStore) ClaimRoute(ctx context.Context, messageID string, senderUserID string, senderDeviceID string, recipientUserID string, groupID string, groupRevision uint64, envelopes []Envelope, lease time.Duration) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	claimToken, err := randomClaim()
	if err != nil {
		return Plan{}, err
	}
	digest := envelopeDigest(envelopes)
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if !store.devices[senderUserID][senderDeviceID] {
		return Plan{}, ErrSenderInactive
	}
	senderUsername := store.users[senderUserID]
	if groupID != "" {
		group, exists := store.groups[groupID]
		_, senderMember := group.members[senderUserID]
		_, recipientMember := group.members[recipientUserID]
		if !exists || group.revision != groupRevision || !senderMember || !recipientMember {
			return Plan{}, ErrGroupState
		}
	}
	deviceIDs := make([]string, 0)
	for deviceID, active := range store.devices[recipientUserID] {
		if active && (recipientUserID != senderUserID || deviceID != senderDeviceID) {
			deviceIDs = append(deviceIDs, deviceID)
		}
	}
	sort.Strings(deviceIDs)
	if len(deviceIDs) == 0 {
		return Plan{}, ErrRecipientAbsent
	}
	if !exactDeviceSet(deviceIDs, envelopes) {
		return Plan{}, ErrEnvelopeCoverage
	}
	now := store.now().UTC()
	if existing := store.routes[messageID]; existing != nil {
		if existing.senderUserID != senderUserID || existing.senderDeviceID != senderDeviceID || existing.recipientUserID != recipientUserID || existing.groupID != groupID || existing.groupRevision != groupRevision || !bytes.Equal(existing.digest[:], digest[:]) {
			return Plan{}, ErrMessageConflict
		}
		if existing.completed {
			return Plan{DeviceIDs: deviceIDs, SenderUsername: senderUsername, Duplicate: true}, nil
		}
		if existing.claimUntil.After(now) {
			return Plan{}, ErrRouteInProgress
		}
		existing.claimToken = claimToken
		existing.claimUntil = now.Add(lease)
		return Plan{DeviceIDs: deviceIDs, SenderUsername: senderUsername, ClaimToken: claimToken}, nil
	}
	store.routes[messageID] = &memoryRoute{
		senderUserID:    senderUserID,
		senderDeviceID:  senderDeviceID,
		recipientUserID: recipientUserID,
		groupID:         groupID,
		groupRevision:   groupRevision,
		digest:          digest,
		claimToken:      claimToken,
		claimUntil:      now.Add(lease),
	}
	return Plan{DeviceIDs: deviceIDs, SenderUsername: senderUsername, ClaimToken: claimToken}, nil
}

func (store *MemoryStore) CompleteRoute(ctx context.Context, messageID string, claimToken string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	route := store.routes[messageID]
	if route == nil || route.claimToken != claimToken || route.completed {
		return ErrClaimInvalid
	}
	route.completed = true
	route.claimToken = ""
	return nil
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}
