package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu              sync.RWMutex
	usersByID       map[string]User
	userIDsByEmail  map[string]string
	userIDsByName   map[string]string
	devicesByID     map[string]Device
	deviceIDsByUser map[string][]string
	preKeysByDevice map[string]PreKeyMaterial
	pendingByID     map[string]PendingMessage
	pendingByDevice map[string][]string
	refreshTokens   map[string]RefreshToken
	deviceLinks     map[string]DeviceLink
	groupsByID      map[string]Group
	groupMembers    map[string]map[string]GroupMember
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:       make(map[string]User),
		userIDsByEmail:  make(map[string]string),
		userIDsByName:   make(map[string]string),
		devicesByID:     make(map[string]Device),
		deviceIDsByUser: make(map[string][]string),
		preKeysByDevice: make(map[string]PreKeyMaterial),
		pendingByID:     make(map[string]PendingMessage),
		pendingByDevice: make(map[string][]string),
		refreshTokens:   make(map[string]RefreshToken),
		deviceLinks:     make(map[string]DeviceLink),
		groupsByID:      make(map[string]Group),
		groupMembers:    make(map[string]map[string]GroupMember),
	}
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (store *MemoryStore) CreateUserWithDevice(email string, username string, passwordHash string, name string, platform string, keys PreKeyMaterial) (User, Device, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	emailKey := normalizedEmail(email)
	if emailKey != "" {
		if _, exists := store.userIDsByEmail[emailKey]; exists {
			return User{}, Device{}, ErrEmailTaken
		}
	}
	usernameKey := normalizedUsername(username)
	if _, exists := store.userIDsByName[usernameKey]; exists {
		return User{}, Device{}, ErrUsernameTaken
	}
	if duplicatePreKeyIDs(keys.OneTimePreKeys) {
		return User{}, Device{}, ErrPreKeyConflict
	}
	now := time.Now().UTC()
	user := User{
		ID:           randomID(),
		Email:        strings.TrimSpace(email),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
	}
	device := Device{
		ID:        randomID(),
		UserID:    user.ID,
		Name:      name,
		Platform:  platform,
		CreatedAt: now,
	}
	store.usersByID[user.ID] = user
	if emailKey != "" {
		store.userIDsByEmail[emailKey] = user.ID
	}
	store.userIDsByName[usernameKey] = user.ID
	store.devicesByID[device.ID] = device
	store.deviceIDsByUser[user.ID] = []string{device.ID}
	store.preKeysByDevice[device.ID] = copyPreKeyMaterial(keys)
	return user, device, nil
}

func (store *MemoryStore) FindUserByEmail(email string) (User, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	id, exists := store.userIDsByEmail[normalizedEmail(email)]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return store.usersByID[id], nil
}

func (store *MemoryStore) FindUserByUsername(username string) (User, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	id, exists := store.userIDsByName[normalizedUsername(username)]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return store.usersByID[id], nil
}

func (store *MemoryStore) FindUserByID(id string) (User, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	user, exists := store.usersByID[id]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (store *MemoryStore) RegisterDevice(userID string, authorizingDeviceID string, name string, platform string, keys PreKeyMaterial) (Device, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.usersByID[userID]; !exists {
		return Device{}, ErrUserNotFound
	}
	authorizingDevice, exists := store.devicesByID[authorizingDeviceID]
	if !exists || authorizingDevice.UserID != userID {
		return Device{}, ErrDeviceNotFound
	}
	if !authorizingDevice.Active() {
		return Device{}, ErrDeviceRevoked
	}
	if duplicatePreKeyIDs(keys.OneTimePreKeys) {
		return Device{}, ErrPreKeyConflict
	}
	device := Device{
		ID:        randomID(),
		UserID:    userID,
		Name:      name,
		Platform:  platform,
		CreatedAt: time.Now().UTC(),
	}
	store.devicesByID[device.ID] = device
	store.deviceIDsByUser[userID] = append(store.deviceIDsByUser[userID], device.ID)
	store.preKeysByDevice[device.ID] = copyPreKeyMaterial(keys)
	return device, nil
}

func (store *MemoryStore) FindDevice(deviceID string) (Device, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	device, exists := store.devicesByID[deviceID]
	if !exists {
		return Device{}, ErrDeviceNotFound
	}
	return copyDevice(device), nil
}

func (store *MemoryStore) ActiveDevices(userID string) ([]Device, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.usersByID[userID]; !exists {
		return nil, ErrUserNotFound
	}
	return store.devicesLocked(userID, true), nil
}

func (store *MemoryStore) Devices(userID string) ([]Device, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if _, exists := store.usersByID[userID]; !exists {
		return nil, ErrUserNotFound
	}
	return store.devicesLocked(userID, false), nil
}

func (store *MemoryStore) RevokeDevice(userID string, authorizingDeviceID string, deviceID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	authorizingDevice, exists := store.devicesByID[authorizingDeviceID]
	if !exists || authorizingDevice.UserID != userID {
		return ErrDeviceNotFound
	}
	if !authorizingDevice.Active() {
		return ErrDeviceRevoked
	}
	device, exists := store.devicesByID[deviceID]
	if !exists || device.UserID != userID {
		return ErrDeviceNotFound
	}
	if !device.Active() {
		return nil
	}
	activeCount := 0
	for _, id := range store.deviceIDsByUser[userID] {
		if store.devicesByID[id].Active() {
			activeCount++
		}
	}
	if activeCount <= 1 {
		return ErrLastDevice
	}
	now := time.Now().UTC()
	device.RevokedAt = &now
	store.devicesByID[deviceID] = device
	delete(store.preKeysByDevice, deviceID)
	for _, messageID := range store.pendingByDevice[deviceID] {
		delete(store.pendingByID, messageID)
	}
	delete(store.pendingByDevice, deviceID)
	store.consumeRefreshTokensLocked(deviceID)
	return nil
}

func (store *MemoryStore) ReplacePreKeys(deviceID string, keys PreKeyMaterial) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	device, exists := store.devicesByID[deviceID]
	if !exists {
		return ErrDeviceNotFound
	}
	if !device.Active() {
		return ErrDeviceRevoked
	}
	if duplicatePreKeyIDs(keys.OneTimePreKeys) {
		return ErrPreKeyConflict
	}
	store.preKeysByDevice[deviceID] = copyPreKeyMaterial(keys)
	return nil
}

func (store *MemoryStore) AddOneTimePreKeys(deviceID string, preKeys []OneTimePreKey) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	device, exists := store.devicesByID[deviceID]
	if !exists {
		return ErrDeviceNotFound
	}
	if !device.Active() {
		return ErrDeviceRevoked
	}
	if len(preKeys) == 0 || duplicatePreKeyIDs(preKeys) {
		return ErrInvalidPreKeys
	}
	for _, preKey := range preKeys {
		if !validOneTimePreKey(preKey) {
			return ErrInvalidPreKeys
		}
	}
	keys, exists := store.preKeysByDevice[deviceID]
	if !exists {
		return ErrInvalidPreKeys
	}
	existingByID := make(map[uint64]OneTimePreKey, len(keys.OneTimePreKeys))
	for _, preKey := range keys.OneTimePreKeys {
		existingByID[preKey.ID] = preKey
	}
	newCount := 0
	for _, preKey := range preKeys {
		if existing, exists := existingByID[preKey.ID]; exists {
			if !bytes.Equal(existing.PublicKey, preKey.PublicKey) {
				return ErrPreKeyConflict
			}
			continue
		}
		newCount++
	}
	if len(keys.OneTimePreKeys)+newCount > maxStoredOneTimePreKeys {
		return ErrPreKeyCapacity
	}
	for _, preKey := range preKeys {
		if _, exists := existingByID[preKey.ID]; !exists {
			keys.OneTimePreKeys = append(keys.OneTimePreKeys, copyOneTimePreKey(preKey))
		}
	}
	store.preKeysByDevice[deviceID] = copyPreKeyMaterial(keys)
	return nil
}

func (store *MemoryStore) OneTimePreKeyCount(deviceID string) (int, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	device, exists := store.devicesByID[deviceID]
	if !exists {
		return 0, ErrDeviceNotFound
	}
	if !device.Active() {
		return 0, ErrDeviceRevoked
	}
	keys, exists := store.preKeysByDevice[deviceID]
	if !exists {
		return 0, ErrInvalidPreKeys
	}
	return len(keys.OneTimePreKeys), nil
}

func (store *MemoryStore) ConsumePreKeyBundles(userID string) ([]ConsumedPreKeyBundle, error) {
	return store.consumePreKeyBundles(userID, "")
}

func (store *MemoryStore) ConsumePreKeyBundlesExcluding(userID string, excludedDeviceID string) ([]ConsumedPreKeyBundle, error) {
	return store.consumePreKeyBundles(userID, excludedDeviceID)
}

func (store *MemoryStore) consumePreKeyBundles(userID string, excludedDeviceID string) ([]ConsumedPreKeyBundle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.usersByID[userID]; !exists {
		return nil, ErrUserNotFound
	}
	devices := store.devicesLocked(userID, true)
	bundles := make([]ConsumedPreKeyBundle, 0, len(devices))
	for _, device := range devices {
		if device.ID == excludedDeviceID {
			continue
		}
		keys, exists := store.preKeysByDevice[device.ID]
		if !exists {
			continue
		}
		bundle := consumedBundle(device.ID, keys)
		if len(keys.OneTimePreKeys) > 0 {
			oneTimePreKey := copyOneTimePreKey(keys.OneTimePreKeys[0])
			bundle.OneTimePreKey = &oneTimePreKey
			keys.OneTimePreKeys = append([]OneTimePreKey(nil), keys.OneTimePreKeys[1:]...)
			store.preKeysByDevice[device.ID] = keys
		}
		bundle.RemainingOneTimePreKeys = len(keys.OneTimePreKeys)
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

func (store *MemoryStore) FanoutMessage(recipientUserID string, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.usersByID[recipientUserID]; !exists {
		return nil, ErrUserNotFound
	}
	senderDevice, exists := store.devicesByID[senderDeviceID]
	if !exists || senderDevice.UserID != senderUserID {
		return nil, ErrDeviceNotFound
	}
	if !senderDevice.Active() {
		return nil, ErrDeviceRevoked
	}
	activeDevices := store.devicesLocked(recipientUserID, true)
	envelopesByDevice := make(map[string]MessageEnvelope, len(envelopes))
	for _, envelope := range envelopes {
		if _, exists := envelopesByDevice[envelope.RecipientDeviceID]; exists {
			return nil, ErrDeviceSetChanged
		}
		envelopesByDevice[envelope.RecipientDeviceID] = envelope
	}
	if len(envelopesByDevice) != len(activeDevices) {
		return nil, ErrDeviceSetChanged
	}
	for _, device := range activeDevices {
		if _, exists := envelopesByDevice[device.ID]; !exists {
			return nil, ErrDeviceSetChanged
		}
	}
	now := time.Now().UTC()
	messages := make([]PendingMessage, 0, len(activeDevices))
	for _, device := range activeDevices {
		envelope := envelopesByDevice[device.ID]
		message := PendingMessage{
			ID:                randomID(),
			RecipientUserID:   recipientUserID,
			RecipientDeviceID: device.ID,
			SenderUserID:      senderUserID,
			SenderDeviceID:    senderDeviceID,
			Ciphertext:        append([]byte(nil), envelope.Ciphertext...),
			CreatedAt:         now,
		}
		store.pendingByID[message.ID] = message
		store.pendingByDevice[device.ID] = append(store.pendingByDevice[device.ID], message.ID)
		messages = append(messages, copyMessage(message))
	}
	return messages, nil
}

func (store *MemoryStore) PendingMessages(recipientDeviceID string, afterID string) ([]PendingMessage, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	messageIDs := store.pendingByDevice[recipientDeviceID]
	start := 0
	if afterID != "" {
		found := false
		for index, id := range messageIDs {
			if id == afterID {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			return nil, ErrMessageAbsent
		}
	}
	messages := make([]PendingMessage, 0, len(messageIDs)-start)
	for _, id := range messageIDs[start:] {
		if message, exists := store.pendingByID[id]; exists {
			messages = append(messages, copyMessage(message))
		}
	}
	return messages, nil
}

func (store *MemoryStore) AcknowledgeMessage(recipientDeviceID string, messageID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	message, exists := store.pendingByID[messageID]
	if !exists || message.RecipientDeviceID != recipientDeviceID {
		return ErrMessageAbsent
	}
	delete(store.pendingByID, messageID)
	messageIDs := store.pendingByDevice[recipientDeviceID]
	remainingMessageIDs := make([]string, 0, len(messageIDs)-1)
	for _, id := range messageIDs {
		if id != messageID {
			remainingMessageIDs = append(remainingMessageIDs, id)
		}
	}
	if len(remainingMessageIDs) == 0 {
		delete(store.pendingByDevice, recipientDeviceID)
	} else {
		store.pendingByDevice[recipientDeviceID] = remainingMessageIDs
	}
	return nil
}

func (store *MemoryStore) CreateRefreshToken(userID string, deviceID string, tokenHash []byte, expiresAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	device, exists := store.devicesByID[deviceID]
	if !exists || device.UserID != userID || !device.Active() {
		return ErrRefreshInvalid
	}
	key := hex.EncodeToString(tokenHash)
	if _, exists := store.refreshTokens[key]; exists {
		return ErrRefreshInvalid
	}
	store.refreshTokens[key] = RefreshToken{
		Hash:      append([]byte(nil), tokenHash...),
		UserID:    userID,
		DeviceID:  deviceID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	return nil
}

func (store *MemoryStore) RotateRefreshToken(tokenHash []byte, replacementHash []byte, replacementExpiresAt time.Time) (User, Device, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now().UTC()
	key := hex.EncodeToString(tokenHash)
	token, exists := store.refreshTokens[key]
	if !exists {
		return User{}, Device{}, ErrRefreshInvalid
	}
	device, deviceExists := store.devicesByID[token.DeviceID]
	if token.ConsumedAt != nil {
		store.consumeRefreshTokensLocked(token.DeviceID)
		return User{}, Device{}, ErrRefreshInvalid
	}
	if !deviceExists || !device.Active() || !token.ExpiresAt.After(now) {
		token.ConsumedAt = &now
		store.refreshTokens[key] = token
		return User{}, Device{}, ErrRefreshInvalid
	}
	replacementKey := hex.EncodeToString(replacementHash)
	if _, exists := store.refreshTokens[replacementKey]; exists {
		return User{}, Device{}, ErrRefreshInvalid
	}
	token.ConsumedAt = &now
	store.refreshTokens[key] = token
	store.refreshTokens[replacementKey] = RefreshToken{
		Hash:      append([]byte(nil), replacementHash...),
		UserID:    token.UserID,
		DeviceID:  token.DeviceID,
		CreatedAt: now,
		ExpiresAt: replacementExpiresAt,
	}
	return store.usersByID[token.UserID], copyDevice(device), nil
}

func (store *MemoryStore) RevokeRefreshToken(tokenHash []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := hex.EncodeToString(tokenHash)
	token, exists := store.refreshTokens[key]
	if !exists || token.ConsumedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	token.ConsumedAt = &now
	store.refreshTokens[key] = token
	return nil
}

func (store *MemoryStore) consumeRefreshTokensLocked(deviceID string) {
	now := time.Now().UTC()
	for key, token := range store.refreshTokens {
		if token.DeviceID == deviceID && token.ConsumedAt == nil {
			token.ConsumedAt = &now
			store.refreshTokens[key] = token
		}
	}
}

func (store *MemoryStore) CreateDeviceLink(linkingPublicKey []byte, approvalSecretHash []byte, claimTokenHash []byte, expiresAt time.Time) (DeviceLink, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	link := DeviceLink{
		ID:                 randomID(),
		LinkingPublicKey:   append([]byte(nil), linkingPublicKey...),
		ApprovalSecretHash: append([]byte(nil), approvalSecretHash...),
		ClaimTokenHash:     append([]byte(nil), claimTokenHash...),
		CreatedAt:          time.Now().UTC(),
		ExpiresAt:          expiresAt,
	}
	store.deviceLinks[link.ID] = link
	return copyDeviceLink(link), nil
}

func (store *MemoryStore) DeviceLinkStatus(linkID string, claimTokenHash []byte) (DeviceLink, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	link, exists := store.deviceLinks[linkID]
	if !exists {
		return DeviceLink{}, ErrDeviceLinkAbsent
	}
	if subtle.ConstantTimeCompare(link.ClaimTokenHash, claimTokenHash) != 1 {
		return DeviceLink{}, ErrDeviceLinkDenied
	}
	if !link.ExpiresAt.After(time.Now().UTC()) {
		return DeviceLink{}, ErrDeviceLinkExpired
	}
	return copyDeviceLink(link), nil
}

func (store *MemoryStore) ApproveDeviceLink(linkID string, approvalSecretHash []byte, linkingPublicKey []byte, userID string, authorizingDeviceID string, encryptedTransfer []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	link, exists := store.deviceLinks[linkID]
	if !exists {
		return ErrDeviceLinkAbsent
	}
	if subtle.ConstantTimeCompare(link.ApprovalSecretHash, approvalSecretHash) != 1 {
		return ErrDeviceLinkDenied
	}
	if subtle.ConstantTimeCompare(link.LinkingPublicKey, linkingPublicKey) != 1 {
		return ErrDeviceLinkDenied
	}
	now := time.Now().UTC()
	if !link.ExpiresAt.After(now) {
		return ErrDeviceLinkExpired
	}
	if link.ApprovedAt != nil || link.ClaimedAt != nil {
		return ErrDeviceLinkState
	}
	device, exists := store.devicesByID[authorizingDeviceID]
	if !exists || device.UserID != userID {
		return ErrDeviceNotFound
	}
	if !device.Active() {
		return ErrDeviceRevoked
	}
	link.UserID = userID
	link.AuthorizingDeviceID = authorizingDeviceID
	link.EncryptedTransfer = append([]byte(nil), encryptedTransfer...)
	link.ApprovedAt = &now
	store.deviceLinks[linkID] = link
	return nil
}

func (store *MemoryStore) ClaimDeviceLink(linkID string, claimTokenHash []byte, name string, platform string, keys PreKeyMaterial, refreshTokenHash []byte, refreshExpiresAt time.Time) (User, Device, []byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	link, exists := store.deviceLinks[linkID]
	if !exists {
		return User{}, Device{}, nil, ErrDeviceLinkAbsent
	}
	if subtle.ConstantTimeCompare(link.ClaimTokenHash, claimTokenHash) != 1 {
		return User{}, Device{}, nil, ErrDeviceLinkDenied
	}
	now := time.Now().UTC()
	if !link.ExpiresAt.After(now) {
		return User{}, Device{}, nil, ErrDeviceLinkExpired
	}
	if link.ApprovedAt == nil || link.ClaimedAt != nil {
		return User{}, Device{}, nil, ErrDeviceLinkState
	}
	authorizingDevice, exists := store.devicesByID[link.AuthorizingDeviceID]
	if !exists || authorizingDevice.UserID != link.UserID {
		return User{}, Device{}, nil, ErrDeviceNotFound
	}
	if !authorizingDevice.Active() {
		return User{}, Device{}, nil, ErrDeviceRevoked
	}
	if duplicatePreKeyIDs(keys.OneTimePreKeys) {
		return User{}, Device{}, nil, ErrPreKeyConflict
	}
	refreshKey := hex.EncodeToString(refreshTokenHash)
	if _, exists := store.refreshTokens[refreshKey]; exists {
		return User{}, Device{}, nil, ErrRefreshInvalid
	}
	device := Device{
		ID:        randomID(),
		UserID:    link.UserID,
		Name:      name,
		Platform:  platform,
		CreatedAt: now,
	}
	store.devicesByID[device.ID] = device
	store.deviceIDsByUser[link.UserID] = append(store.deviceIDsByUser[link.UserID], device.ID)
	store.preKeysByDevice[device.ID] = copyPreKeyMaterial(keys)
	store.refreshTokens[refreshKey] = RefreshToken{
		Hash:      append([]byte(nil), refreshTokenHash...),
		UserID:    link.UserID,
		DeviceID:  device.ID,
		CreatedAt: now,
		ExpiresAt: refreshExpiresAt,
	}
	link.ClaimedAt = &now
	store.deviceLinks[linkID] = link
	return store.usersByID[link.UserID], copyDevice(device), append([]byte(nil), link.EncryptedTransfer...), nil
}

func (store *MemoryStore) devicesLocked(userID string, activeOnly bool) []Device {
	devices := make([]Device, 0, len(store.deviceIDsByUser[userID]))
	for _, id := range store.deviceIDsByUser[userID] {
		device := store.devicesByID[id]
		if activeOnly && !device.Active() {
			continue
		}
		devices = append(devices, copyDevice(device))
	}
	sort.Slice(devices, func(left int, right int) bool {
		if devices[left].CreatedAt.Equal(devices[right].CreatedAt) {
			return devices[left].ID < devices[right].ID
		}
		return devices[left].CreatedAt.Before(devices[right].CreatedAt)
	})
	return devices
}

func normalizedUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func randomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

func duplicatePreKeyIDs(preKeys []OneTimePreKey) bool {
	identifiers := make(map[uint64]struct{}, len(preKeys))
	for _, preKey := range preKeys {
		if _, exists := identifiers[preKey.ID]; exists {
			return true
		}
		identifiers[preKey.ID] = struct{}{}
	}
	return false
}

func copyDevice(device Device) Device {
	if device.RevokedAt != nil {
		revokedAt := *device.RevokedAt
		device.RevokedAt = &revokedAt
	}
	return device
}

func copyPreKeyMaterial(keys PreKeyMaterial) PreKeyMaterial {
	copied := PreKeyMaterial{
		IdentityEncryptionPublic: append([]byte(nil), keys.IdentityEncryptionPublic...),
		IdentitySigningPublic:    append([]byte(nil), keys.IdentitySigningPublic...),
		SignedPreKeyID:           keys.SignedPreKeyID,
		SignedPreKeyPublic:       append([]byte(nil), keys.SignedPreKeyPublic...),
		SignedPreKeySignature:    append([]byte(nil), keys.SignedPreKeySignature...),
		OneTimePreKeys:           make([]OneTimePreKey, 0, len(keys.OneTimePreKeys)),
	}
	for _, preKey := range keys.OneTimePreKeys {
		copied.OneTimePreKeys = append(copied.OneTimePreKeys, copyOneTimePreKey(preKey))
	}
	sort.Slice(copied.OneTimePreKeys, func(left int, right int) bool {
		return copied.OneTimePreKeys[left].ID < copied.OneTimePreKeys[right].ID
	})
	return copied
}

func copyOneTimePreKey(preKey OneTimePreKey) OneTimePreKey {
	return OneTimePreKey{ID: preKey.ID, PublicKey: append([]byte(nil), preKey.PublicKey...)}
}

func consumedBundle(deviceID string, keys PreKeyMaterial) ConsumedPreKeyBundle {
	return ConsumedPreKeyBundle{
		DeviceID:                 deviceID,
		IdentityEncryptionPublic: append([]byte(nil), keys.IdentityEncryptionPublic...),
		IdentitySigningPublic:    append([]byte(nil), keys.IdentitySigningPublic...),
		SignedPreKeyID:           keys.SignedPreKeyID,
		SignedPreKeyPublic:       append([]byte(nil), keys.SignedPreKeyPublic...),
		SignedPreKeySignature:    append([]byte(nil), keys.SignedPreKeySignature...),
	}
}

func copyMessage(message PendingMessage) PendingMessage {
	message.Ciphertext = append([]byte(nil), message.Ciphertext...)
	return message
}

func copyDeviceLink(link DeviceLink) DeviceLink {
	link.LinkingPublicKey = append([]byte(nil), link.LinkingPublicKey...)
	link.ApprovalSecretHash = append([]byte(nil), link.ApprovalSecretHash...)
	link.ClaimTokenHash = append([]byte(nil), link.ClaimTokenHash...)
	link.EncryptedTransfer = append([]byte(nil), link.EncryptedTransfer...)
	if link.ApprovedAt != nil {
		approvedAt := *link.ApprovedAt
		link.ApprovedAt = &approvedAt
	}
	if link.ClaimedAt != nil {
		claimedAt := *link.ClaimedAt
		link.ClaimedAt = &claimedAt
	}
	return link
}
