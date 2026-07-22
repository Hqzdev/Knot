package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmailTaken        = errors.New("email already exists")
	ErrUsernameTaken     = errors.New("username already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrDeviceNotFound    = errors.New("device not found")
	ErrDeviceRevoked     = errors.New("device is revoked")
	ErrLastDevice        = errors.New("last active device cannot be revoked")
	ErrInvalidPreKeys    = errors.New("invalid prekey material")
	ErrPreKeyConflict    = errors.New("prekey identifier already exists")
	ErrDeviceSetChanged  = errors.New("recipient device set changed")
	ErrMessageAbsent     = errors.New("message not found")
	ErrRefreshInvalid    = errors.New("refresh token is invalid")
	ErrDeviceLinkAbsent  = errors.New("device link not found")
	ErrDeviceLinkDenied  = errors.New("device link credential is invalid")
	ErrDeviceLinkState   = errors.New("device link state is invalid")
	ErrDeviceLinkExpired = errors.New("device link expired")
	ErrPreKeyCapacity    = errors.New("prekey capacity exceeded")
	ErrGroupNotFound     = errors.New("group not found")
	ErrGroupForbidden    = errors.New("group action is forbidden")
	ErrGroupStateChanged = errors.New("group membership changed")
	ErrGroupMemberExists = errors.New("group member already exists")
)

type User struct {
	ID           string
	Email        string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Device struct {
	ID        string
	UserID    string
	Name      string
	Platform  string
	CreatedAt time.Time
	RevokedAt *time.Time
}

func (device Device) Active() bool {
	return device.RevokedAt == nil
}

type OneTimePreKey struct {
	ID        uint64
	PublicKey []byte
}

type PreKeyMaterial struct {
	IdentityEncryptionPublic []byte
	IdentitySigningPublic    []byte
	SignedPreKeyID           uint64
	SignedPreKeyPublic       []byte
	SignedPreKeySignature    []byte
	OneTimePreKeys           []OneTimePreKey
}

type ConsumedPreKeyBundle struct {
	DeviceID                 string
	IdentityEncryptionPublic []byte
	IdentitySigningPublic    []byte
	SignedPreKeyID           uint64
	SignedPreKeyPublic       []byte
	SignedPreKeySignature    []byte
	OneTimePreKey            *OneTimePreKey
	RemainingOneTimePreKeys  int
}

type MessageEnvelope struct {
	RecipientDeviceID string
	Ciphertext        []byte
}

type PendingMessage struct {
	ID                string
	RecipientUserID   string
	RecipientDeviceID string
	SenderUserID      string
	SenderDeviceID    string
	Ciphertext        []byte
	CreatedAt         time.Time
	GroupID           string
	GroupRevision     uint64
}

type Group struct {
	ID        string
	OwnerID   string
	Revision  uint64
	CreatedAt time.Time
}

type GroupMember struct {
	GroupID  string
	UserID   string
	Role     string
	JoinedAt time.Time
}

type RefreshToken struct {
	Hash       []byte
	UserID     string
	DeviceID   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

type DeviceLink struct {
	ID                  string
	LinkingPublicKey    []byte
	ApprovalSecretHash  []byte
	ClaimTokenHash      []byte
	UserID              string
	AuthorizingDeviceID string
	EncryptedTransfer   []byte
	CreatedAt           time.Time
	ExpiresAt           time.Time
	ApprovedAt          *time.Time
	ClaimedAt           *time.Time
}

type Store interface {
	Ping(ctx context.Context) error
	CreateUserWithDevice(email string, username string, passwordHash string, name string, platform string, keys PreKeyMaterial) (User, Device, error)
	FindUserByEmail(email string) (User, error)
	FindUserByUsername(username string) (User, error)
	FindUserByID(id string) (User, error)
	RegisterDevice(userID string, authorizingDeviceID string, name string, platform string, keys PreKeyMaterial) (Device, error)
	FindDevice(deviceID string) (Device, error)
	ActiveDevices(userID string) ([]Device, error)
	Devices(userID string) ([]Device, error)
	RevokeDevice(userID string, authorizingDeviceID string, deviceID string) error
	ReplacePreKeys(deviceID string, keys PreKeyMaterial) error
	AddOneTimePreKeys(deviceID string, preKeys []OneTimePreKey) error
	OneTimePreKeyCount(deviceID string) (int, error)
	ConsumePreKeyBundles(userID string) ([]ConsumedPreKeyBundle, error)
	ConsumePreKeyBundlesExcluding(userID string, excludedDeviceID string) ([]ConsumedPreKeyBundle, error)
	FanoutMessage(recipientUserID string, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error)
	PendingMessages(recipientDeviceID string, afterID string) ([]PendingMessage, error)
	AcknowledgeMessage(recipientDeviceID string, messageID string) error
	CreateRefreshToken(userID string, deviceID string, tokenHash []byte, expiresAt time.Time) error
	RotateRefreshToken(tokenHash []byte, replacementHash []byte, replacementExpiresAt time.Time) (User, Device, error)
	RevokeRefreshToken(tokenHash []byte) error
	CreateDeviceLink(linkingPublicKey []byte, approvalSecretHash []byte, claimTokenHash []byte, expiresAt time.Time) (DeviceLink, error)
	DeviceLinkStatus(linkID string, claimTokenHash []byte) (DeviceLink, error)
	ApproveDeviceLink(linkID string, approvalSecretHash []byte, linkingPublicKey []byte, userID string, authorizingDeviceID string, encryptedTransfer []byte) error
	ClaimDeviceLink(linkID string, claimTokenHash []byte, name string, platform string, keys PreKeyMaterial, refreshTokenHash []byte, refreshExpiresAt time.Time) (User, Device, []byte, error)
	CreateGroup(ownerUserID string, memberUserIDs []string) (Group, []GroupMember, error)
	Groups(userID string) ([]Group, error)
	GroupMembers(groupID string, requestingUserID string) (Group, []GroupMember, error)
	AddGroupMembers(groupID string, requestingUserID string, memberUserIDs []string) (Group, []GroupMember, error)
	RemoveGroupMember(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error)
	TransferGroupOwnership(groupID string, requestingUserID string, memberUserID string) (Group, []GroupMember, error)
	FanoutGroupMessage(groupID string, expectedRevision uint64, senderUserID string, senderDeviceID string, envelopes []MessageEnvelope) ([]PendingMessage, error)
}
