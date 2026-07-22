package store

import (
	"sort"
	"time"
)

const maxGroupMembers = 100

func (store *MemoryStore) CreateGroup(ownerUserID string, memberUserIDs []string) (Group, []GroupMember, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.usersByID[ownerUserID]; !exists {
		return Group{}, nil, ErrUserNotFound
	}
	memberIDs, err := normalizedGroupMemberIDs(ownerUserID, memberUserIDs)
	if err != nil {
		return Group{}, nil, err
	}
	for _, memberID := range memberIDs {
		if _, exists := store.usersByID[memberID]; !exists {
			return Group{}, nil, ErrUserNotFound
		}
	}
	now := time.Now().UTC()
	group := Group{ID: randomID(), OwnerID: ownerUserID, Revision: 1, CreatedAt: now}
	members := make(map[string]GroupMember, len(memberIDs))
	for _, memberID := range memberIDs {
		role := "member"
		if memberID == ownerUserID {
			role = "owner"
		}
		members[memberID] = GroupMember{
			GroupID:  group.ID,
			UserID:   memberID,
			Role:     role,
			JoinedAt: now,
		}
	}
	store.groupsByID[group.ID] = group
	store.groupMembers[group.ID] = members
	return group, groupMemberSnapshot(members), nil
}

func (store *MemoryStore) Groups(userID string) ([]Group, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.usersByID[userID]; !exists {
		return nil, ErrUserNotFound
	}
	groups := make([]Group, 0)
	for groupID, members := range store.groupMembers {
		if _, exists := members[userID]; exists {
			groups = append(groups, store.groupsByID[groupID])
		}
	}
	sort.Slice(groups, func(left int, right int) bool {
		if groups[left].CreatedAt.Equal(groups[right].CreatedAt) {
			return groups[left].ID < groups[right].ID
		}
		return groups[left].CreatedAt.Before(groups[right].CreatedAt)
	})
	return groups, nil
}

func (store *MemoryStore) GroupMembers(groupID string, requestingUserID string) (Group, []GroupMember, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	group, members, err := store.requireGroupMemberLocked(groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	return group, groupMemberSnapshot(members), nil
}

func (store *MemoryStore) AddGroupMembers(groupID string, requestingUserID string, memberUserIDs []string) (Group, []GroupMember, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	group, members, err := store.requireGroupOwnerLocked(groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	if len(memberUserIDs) == 0 || len(members)+len(memberUserIDs) > maxGroupMembers {
		return Group{}, nil, ErrGroupStateChanged
	}
	seen := make(map[string]struct{}, len(memberUserIDs))
	for _, memberID := range memberUserIDs {
		if _, exists := seen[memberID]; exists {
			return Group{}, nil, ErrGroupMemberExists
		}
		seen[memberID] = struct{}{}
		if _, exists := members[memberID]; exists {
			return Group{}, nil, ErrGroupMemberExists
		}
		if _, exists := store.usersByID[memberID]; !exists {
			return Group{}, nil, ErrUserNotFound
		}
	}
	now := time.Now().UTC()
	for _, memberID := range memberUserIDs {
		members[memberID] = GroupMember{
			GroupID:  groupID,
			UserID:   memberID,
			Role:     "member",
			JoinedAt: now,
		}
	}
	group.Revision++
	store.groupsByID[groupID] = group
	return group, groupMemberSnapshot(members), nil
}

func (store *MemoryStore) RemoveGroupMember(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	group, members, err := store.requireGroupMemberLocked(groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	if requestingUserID != group.OwnerID && requestingUserID != memberUserID {
		return Group{}, nil, ErrGroupForbidden
	}
	if memberUserID == group.OwnerID {
		return Group{}, nil, ErrGroupForbidden
	}
	if _, exists := members[memberUserID]; !exists {
		return Group{}, nil, ErrGroupNotFound
	}
	delete(members, memberUserID)
	group.Revision++
	store.groupsByID[groupID] = group
	return group, groupMemberSnapshot(members), nil
}

func (store *MemoryStore) TransferGroupOwnership(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	group, members, err := store.requireGroupOwnerLocked(groupID, requestingUserID)
	if err != nil {
		return Group{}, nil, err
	}
	if memberUserID == group.OwnerID {
		return group, groupMemberSnapshot(members), nil
	}
	target, exists := members[memberUserID]
	if !exists {
		return Group{}, nil, ErrGroupNotFound
	}
	previousOwner := members[group.OwnerID]
	previousOwner.Role = "member"
	members[group.OwnerID] = previousOwner
	target.Role = "owner"
	members[memberUserID] = target
	group.OwnerID = memberUserID
	group.Revision++
	store.groupsByID[groupID] = group
	return group, groupMemberSnapshot(members), nil
}

func (store *MemoryStore) FanoutGroupMessage(groupID string, expectedRevision uint64, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	group, members, err := store.requireGroupMemberLocked(groupID, senderUserID)
	if err != nil {
		return nil, err
	}
	if group.Revision != expectedRevision {
		return nil, ErrGroupStateChanged
	}
	senderDevice, exists := store.devicesByID[senderDeviceID]
	if !exists || senderDevice.UserID != senderUserID {
		return nil, ErrDeviceNotFound
	}
	if !senderDevice.Active() {
		return nil, ErrDeviceRevoked
	}
	targetDevices := make([]Device, 0)
	for memberID := range members {
		for _, deviceID := range store.deviceIDsByUser[memberID] {
			device := store.devicesByID[deviceID]
			if device.Active() && device.ID != senderDeviceID {
				targetDevices = append(targetDevices, device)
			}
		}
	}
	sort.Slice(targetDevices, func(left int, right int) bool {
		if targetDevices[left].CreatedAt.Equal(targetDevices[right].CreatedAt) {
			return targetDevices[left].ID < targetDevices[right].ID
		}
		return targetDevices[left].CreatedAt.Before(targetDevices[right].CreatedAt)
	})
	envelopesByDevice, valid := exactEnvelopes(targetDevices, envelopes)
	if !valid {
		return nil, ErrGroupStateChanged
	}
	now := time.Now().UTC()
	messages := make([]PendingMessage, 0, len(targetDevices))
	for _, device := range targetDevices {
		message := PendingMessage{
			ID:                randomID(),
			RecipientUserID:   device.UserID,
			RecipientDeviceID: device.ID,
			SenderUserID:      senderUserID,
			SenderDeviceID:    senderDeviceID,
			Ciphertext:        append([]byte(nil), envelopesByDevice[device.ID].Ciphertext...),
			CreatedAt:         now,
			GroupID:           groupID,
			GroupRevision:     group.Revision,
		}
		store.pendingByID[message.ID] = message
		store.pendingByDevice[device.ID] = append(store.pendingByDevice[device.ID], message.ID)
		messages = append(messages, copyMessage(message))
	}
	return messages, nil
}

func (store *MemoryStore) requireGroupMemberLocked(groupID string, userID string) (Group, map[string]GroupMember, error) {
	group, exists := store.groupsByID[groupID]
	if !exists {
		return Group{}, nil, ErrGroupNotFound
	}
	members := store.groupMembers[groupID]
	if _, exists := members[userID]; !exists {
		return Group{}, nil, ErrGroupForbidden
	}
	return group, members, nil
}

func (store *MemoryStore) requireGroupOwnerLocked(groupID string, userID string) (Group, map[string]GroupMember, error) {
	group, members, err := store.requireGroupMemberLocked(groupID, userID)
	if err != nil {
		return Group{}, nil, err
	}
	if group.OwnerID != userID {
		return Group{}, nil, ErrGroupForbidden
	}
	return group, members, nil
}

func normalizedGroupMemberIDs(ownerUserID string, memberUserIDs []string) ([]string, error) {
	if len(memberUserIDs)+1 > maxGroupMembers {
		return nil, ErrGroupStateChanged
	}
	memberIDs := []string{ownerUserID}
	seen := map[string]struct{}{ownerUserID: {}}
	for _, memberID := range memberUserIDs {
		if memberID == ownerUserID {
			continue
		}
		if _, exists := seen[memberID]; exists {
			return nil, ErrGroupMemberExists
		}
		seen[memberID] = struct{}{}
		memberIDs = append(memberIDs, memberID)
	}
	return memberIDs, nil
}

func groupMemberSnapshot(members map[string]GroupMember) []GroupMember {
	snapshot := make([]GroupMember, 0, len(members))
	for _, member := range members {
		snapshot = append(snapshot, member)
	}
	sort.Slice(snapshot, func(left int, right int) bool {
		if snapshot[left].JoinedAt.Equal(snapshot[right].JoinedAt) {
			return snapshot[left].UserID < snapshot[right].UserID
		}
		return snapshot[left].JoinedAt.Before(snapshot[right].JoinedAt)
	})
	return snapshot
}

func exactEnvelopes(devices []Device, envelopes []MessageEnvelope) (map[string]MessageEnvelope, bool) {
	envelopesByDevice := make(map[string]MessageEnvelope, len(envelopes))
	for _, envelope := range envelopes {
		if _, exists := envelopesByDevice[envelope.RecipientDeviceID]; exists {
			return nil, false
		}
		envelopesByDevice[envelope.RecipientDeviceID] = envelope
	}
	if len(envelopesByDevice) != len(devices) {
		return nil, false
	}
	for _, device := range devices {
		if _, exists := envelopesByDevice[device.ID]; !exists {
			return nil, false
		}
	}
	return envelopesByDevice, true
}
