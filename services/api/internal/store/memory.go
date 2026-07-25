package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mutex         sync.RWMutex
	users         map[string]User
	sessions      map[string]Session
	conversations map[string]Conversation
	direct        map[string]string
	contacts      map[string]map[string]bool
}

func NewMemoryStore() *MemoryStore {
	now := time.Now().UTC()
	return &MemoryStore{
		users: map[string]User{
			SupportUserID: {ID: SupportUserID, Username: "knot-support", DisplayName: "Toxic Support", Kind: "system", CreatedAt: now},
		},
		sessions: make(map[string]Session),
		conversations: map[string]Conversation{
			WallConversationID: {ID: WallConversationID, Kind: "wall", Title: "THE WALL", CreatedAt: now},
		},
		direct:   make(map[string]string),
		contacts: make(map[string]map[string]bool),
	}
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (store *MemoryStore) CreateUser(ctx context.Context, user User) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, existing := range store.users {
		if strings.EqualFold(existing.Username, user.Username) {
			return User{}, ErrUsernameTaken
		}
		if user.Email != "" && strings.EqualFold(existing.Email, user.Email) {
			return User{}, ErrEmailTaken
		}
	}
	store.users[user.ID] = user
	return user, nil
}

func (store *MemoryStore) UserByID(ctx context.Context, id string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	user, exists := store.users[id]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (store *MemoryStore) UserByUsername(ctx context.Context, username string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	for _, user := range store.users {
		if strings.EqualFold(user.Username, username) {
			return user, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (store *MemoryStore) UserByEmail(ctx context.Context, email string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	for _, user := range store.users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (store *MemoryStore) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	normalized := strings.ToLower(query)
	values := make([]User, 0, limit)
	for _, user := range store.users {
		if strings.Contains(strings.ToLower(user.Username), normalized) || strings.Contains(strings.ToLower(user.DisplayName), normalized) {
			values = append(values, user)
		}
	}
	sort.Slice(values, func(left int, right int) bool { return values[left].Username < values[right].Username })
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (store *MemoryStore) UpdateProfile(ctx context.Context, userID string, displayName string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	user, exists := store.users[userID]
	if !exists {
		return User{}, ErrUserNotFound
	}
	user.DisplayName = displayName
	store.users[userID] = user
	return user, nil
}

func (store *MemoryStore) CreateSession(ctx context.Context, value Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.sessions[string(value.TokenHash)] = value
	return nil
}

func (store *MemoryStore) RotateSession(ctx context.Context, tokenHash []byte, replacement Session) (User, Session, error) {
	if err := ctx.Err(); err != nil {
		return User{}, Session{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	current, exists := store.sessions[string(tokenHash)]
	if !exists || current.ExpiresAt.Before(time.Now().UTC()) {
		return User{}, Session{}, ErrSessionInvalid
	}
	delete(store.sessions, string(tokenHash))
	replacement.ID = current.ID
	replacement.UserID = current.UserID
	replacement.Mode = current.Mode
	replacement.FirstSeen = current.FirstSeen
	if replacement.DeviceID == "" {
		replacement.DeviceID = current.DeviceID
		replacement.UserAgent = current.UserAgent
		replacement.Browser = current.Browser
		replacement.OS = current.OS
		replacement.FormFactor = current.FormFactor
	}
	if replacement.LastSeen.IsZero() {
		replacement.LastSeen = time.Now().UTC()
	}
	store.sessions[string(replacement.TokenHash)] = replacement
	return store.users[current.UserID], replacement, nil
}

func (store *MemoryStore) RevokeSession(ctx context.Context, tokenHash []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.sessions, string(tokenHash))
	return nil
}

func (store *MemoryStore) Conversations(ctx context.Context, userID string) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	values := []Conversation{store.conversations[WallConversationID]}
	now := time.Now().UTC()
	for _, conversation := range store.conversations {
		if conversation.ID != WallConversationID && member(conversation, userID) && (conversation.ExpiresAt == nil || conversation.ExpiresAt.After(now)) {
			values = append(values, conversation)
		}
	}
	sort.Slice(values, func(left int, right int) bool { return values[left].CreatedAt.Before(values[right].CreatedAt) })
	return cloneConversations(values), nil
}

func (store *MemoryStore) Conversation(ctx context.Context, userID string, conversationID string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	conversation, exists := store.conversations[conversationID]
	if !exists || conversation.ExpiresAt != nil && !conversation.ExpiresAt.After(time.Now().UTC()) || conversation.Kind != "wall" && !member(conversation, userID) {
		return Conversation{}, ErrConversation
	}
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) CreateDirect(ctx context.Context, ownerID string, peerID string, id string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	key := directKey(ownerID, peerID)
	if existingID := store.direct[key]; existingID != "" {
		return cloneConversation(store.conversations[existingID]), nil
	}
	conversation := Conversation{
		ID: id, Kind: "direct", Members: []Member{
			{UserID: ownerID, Username: store.users[ownerID].Username, Role: "member"},
			{UserID: peerID, Username: store.users[peerID].Username, Role: "member"},
		}, CreatedAt: time.Now().UTC(),
	}
	store.conversations[id] = conversation
	store.direct[key] = id
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) CreateGroup(ctx context.Context, ownerID string, title string, userIDs []string, id string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	members := []Member{{UserID: ownerID, Username: store.users[ownerID].Username, Role: "owner"}}
	for _, userID := range unique(userIDs) {
		if userID != ownerID {
			members = append(members, Member{UserID: userID, Username: store.users[userID].Username, Role: "member"})
		}
	}
	conversation := Conversation{ID: id, Kind: "group", Title: title, OwnerID: ownerID, Members: members, CreatedAt: time.Now().UTC()}
	store.conversations[id] = conversation
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) CreateRoulette(ctx context.Context, leftID string, rightID string, id string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	conversation := Conversation{
		ID: id, Kind: "roulette", Title: "RANDOM INTERCEPT", Members: []Member{
			{UserID: leftID, Username: store.users[leftID].Username, Role: "member"},
			{UserID: rightID, Username: store.users[rightID].Username, Role: "member"},
		}, CreatedAt: time.Now().UTC(),
	}
	store.conversations[id] = conversation
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) CreateBurner(ctx context.Context, ownerID string, sourceID string, id string, expiresAt time.Time) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	source, exists := store.conversations[sourceID]
	if !exists || !member(source, ownerID) || source.Kind != "direct" && source.Kind != "group" && source.Kind != "roulette" {
		return Conversation{}, ErrConversation
	}
	members := []Member{{UserID: ownerID, Username: store.users[ownerID].Username, Role: "owner"}}
	for _, member := range source.Members {
		if member.UserID != ownerID && member.UserID != SupportUserID {
			members = append(members, Member{UserID: member.UserID, Username: member.Username, Role: "member"})
		}
	}
	if len(members) < 2 {
		return Conversation{}, ErrConversation
	}
	expiry := expiresAt.UTC()
	conversation := Conversation{ID: id, Kind: "burner", Title: "60 SECOND BURNER", OwnerID: ownerID, Members: members, CreatedAt: time.Now().UTC(), ExpiresAt: &expiry}
	store.conversations[id] = conversation
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) EnsureSupportConversation(ctx context.Context, userID string) error {
	_, err := store.CreateDirect(ctx, userID, SupportUserID, "support_"+userID)
	return err
}

func (store *MemoryStore) AddMembers(ctx context.Context, ownerID string, conversationID string, userIDs []string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	conversation, exists := store.conversations[conversationID]
	if !exists || conversation.OwnerID != ownerID {
		return Conversation{}, ErrConversation
	}
	for _, userID := range unique(userIDs) {
		if !member(conversation, userID) {
			conversation.Members = append(conversation.Members, Member{UserID: userID, Username: store.users[userID].Username, Role: "member"})
		}
	}
	store.conversations[conversationID] = conversation
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) RemoveMember(ctx context.Context, ownerID string, conversationID string, userID string) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	conversation, exists := store.conversations[conversationID]
	if !exists || conversation.OwnerID != ownerID || userID == ownerID {
		return Conversation{}, ErrConversation
	}
	filtered := conversation.Members[:0]
	for _, value := range conversation.Members {
		if value.UserID != userID {
			filtered = append(filtered, value)
		}
	}
	conversation.Members = filtered
	store.conversations[conversationID] = conversation
	return cloneConversation(conversation), nil
}

func (store *MemoryStore) Contacts(ctx context.Context, userID string) ([]User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	values := make([]User, 0)
	for contactID := range store.contacts[userID] {
		values = append(values, store.users[contactID])
	}
	sort.Slice(values, func(left int, right int) bool { return values[left].Username < values[right].Username })
	return values, nil
}

func (store *MemoryStore) SetContact(ctx context.Context, userID string, contactID string, active bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.contacts[userID] == nil {
		store.contacts[userID] = make(map[string]bool)
	}
	if active {
		store.contacts[userID][contactID] = true
	} else {
		delete(store.contacts[userID], contactID)
	}
	return nil
}

func member(conversation Conversation, userID string) bool {
	for _, value := range conversation.Members {
		if value.UserID == userID {
			return true
		}
	}
	return false
}

func directKey(left string, right string) string {
	values := []string{left, right}
	sort.Strings(values)
	return strings.Join(values, ":")
}

func unique(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func cloneConversation(value Conversation) Conversation {
	value.Members = append([]Member(nil), value.Members...)
	return value
}

func cloneConversations(values []Conversation) []Conversation {
	result := make([]Conversation, len(values))
	for index, value := range values {
		result[index] = cloneConversation(value)
	}
	return result
}
