package store

import (
	"context"
	"strings"
	"sync"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
)

type MemoryDirectory struct {
	mutex         sync.RWMutex
	users         map[string]string
	conversations map[string]Conversation
}

func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{users: make(map[string]string), conversations: make(map[string]Conversation)}
}

func (directory *MemoryDirectory) AddUser(id string, username string) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	directory.users[id] = username
}

func (directory *MemoryDirectory) AddConversation(conversation Conversation) {
	directory.mutex.Lock()
	defer directory.mutex.Unlock()
	directory.conversations[conversation.ID] = conversation
}

func (directory *MemoryDirectory) User(ctx context.Context, userID string, username string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	directory.mutex.RLock()
	defer directory.mutex.RUnlock()
	return strings.EqualFold(directory.users[userID], username), nil
}

func (directory *MemoryDirectory) Conversation(ctx context.Context, conversationID string, userID string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	directory.mutex.RLock()
	defer directory.mutex.RUnlock()
	conversation, exists := directory.conversations[conversationID]
	if !exists {
		return Conversation{}, ErrConversationNotFound
	}
	if conversation.Kind == knotv1.ConversationKind_CONVERSATION_KIND_WALL || contains(conversation.ParticipantUserIDs, userID) {
		return conversation, nil
	}
	return Conversation{}, ErrConversationDenied
}

func (directory *MemoryDirectory) Alternatives(ctx context.Context, userID string, excludedID string) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory.mutex.RLock()
	defer directory.mutex.RUnlock()
	values := make([]Conversation, 0)
	for _, conversation := range directory.conversations {
		if conversation.ID == excludedID || conversation.Kind != knotv1.ConversationKind_CONVERSATION_KIND_DIRECT && conversation.Kind != knotv1.ConversationKind_CONVERSATION_KIND_GROUP || !contains(conversation.ParticipantUserIDs, userID) || contains(conversation.ParticipantUserIDs, "usr_knot_support") {
			continue
		}
		values = append(values, conversation)
	}
	return values, nil
}

func (directory *MemoryDirectory) Ping(ctx context.Context) error {
	return ctx.Err()
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
