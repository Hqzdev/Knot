package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	knotv1 "github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1"
	"github.com/yaroslavfairfieldd/knot/services/shared/session"
	"google.golang.org/protobuf/proto"
)

type MemoryStore struct {
	mutex        sync.RWMutex
	now          func() time.Time
	messages     map[string]*knotv1.Message
	messageOrder []*knotv1.Message
	commands     map[string]string
	events       map[string]string
	wiretap      []*knotv1.WiretapRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now:      time.Now,
		messages: make(map[string]*knotv1.Message),
		commands: make(map[string]string),
		events:   make(map[string]string),
	}
}

func (store *MemoryStore) Append(ctx context.Context, input *knotv1.Message) (*knotv1.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if messageID := store.commands[input.GetClientCommandId()]; messageID != "" {
		return cloneMessage(store.messages[messageID]), true, nil
	}
	now := store.now().UTC()
	messageID, err := session.NewID()
	if err != nil {
		return nil, false, err
	}
	message := cloneMessage(input)
	message.Sequence = uint64(len(store.messageOrder) + 1)
	message.Id = "msg_" + messageID
	message.CreatedAtUnixMillis = now.UnixMilli()
	message.ServerSeenAtUnixMillis = now.UnixMilli()
	message.DeliveredAtUnixMillis = now.UnixMilli()
	message.CurrentText = message.OriginalText
	message.Reactions = []*knotv1.Reaction{{Emoji: "👁", Usernames: []string{"server"}}}
	message.Route = append(message.Route, &knotv1.RouteHop{Service: "delivery", Status: "stored plaintext", OccurredAtUnixMillis: now.UnixMilli()})
	store.messages[message.Id] = message
	store.messageOrder = append(store.messageOrder, message)
	store.commands[message.ClientCommandId] = message.Id
	store.appendWiretapLocked(&knotv1.WiretapRecord{
		EventId:              message.ClientCommandId,
		EventKind:            knotv1.MessageEventKind_MESSAGE_EVENT_KIND_CREATE,
		Message:              cloneMessage(message),
		ActorUserId:          message.AuthorUserId,
		ActorUsername:        message.AuthorUsername,
		SessionId:            message.SessionId,
		SessionMode:          message.SessionMode,
		OccurredAtUnixMillis: now.UnixMilli(),
	})
	return cloneMessage(message), false, nil
}

func (store *MemoryStore) ApplyEvent(ctx context.Context, request *knotv1.ApplyEventRequest) (*knotv1.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if messageID := store.events[request.GetClientCommandId()]; messageID != "" {
		return cloneMessage(store.messages[messageID]), true, nil
	}
	message := store.messages[request.GetMessageId()]
	if message == nil {
		return nil, false, ErrMessageNotFound
	}
	if !contains(message.ParticipantUserIds, request.ActorUserId) && message.ConversationKind != knotv1.ConversationKind_CONVERSATION_KIND_WALL {
		return nil, false, ErrForbidden
	}
	if (request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_EDIT || request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE) && message.AuthorUserId != request.ActorUserId {
		return nil, false, ErrForbidden
	}
	occurredAt := time.UnixMilli(request.GetOccurredAtUnixMillis()).UTC()
	if request.GetOccurredAtUnixMillis() <= 0 {
		occurredAt = store.now().UTC()
	}
	if err := applyEvent(message, request, occurredAt); err != nil {
		return nil, false, err
	}
	store.events[request.ClientCommandId] = message.Id
	store.appendWiretapLocked(&knotv1.WiretapRecord{
		EventId:              request.ClientCommandId,
		EventKind:            request.Kind,
		Message:              cloneMessage(message),
		ActorUserId:          request.ActorUserId,
		ActorUsername:        request.ActorUsername,
		SessionId:            request.SessionId,
		SessionMode:          request.SessionMode,
		Text:                 request.Text,
		Emoji:                request.Emoji,
		Active:               request.Active,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	})
	return cloneMessage(message), false, nil
}

func (store *MemoryStore) History(ctx context.Context, userID string, conversationID string, after uint64, limit int) ([]*knotv1.Message, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, after, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	values := make([]*knotv1.Message, 0, limit)
	next := after
	for _, message := range store.messageOrder {
		if message.Sequence <= after || message.ConversationId != conversationID {
			continue
		}
		if !contains(message.ParticipantUserIds, userID) && message.ConversationKind != knotv1.ConversationKind_CONVERSATION_KIND_WALL {
			continue
		}
		values = append(values, cloneMessage(message))
		next = message.Sequence
		if len(values) == limit {
			break
		}
	}
	return values, next, nil
}

func (store *MemoryStore) Wiretap(ctx context.Context, filter WiretapFilter) ([]*knotv1.WiretapRecord, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, filter.AfterSequence, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	values := make([]*knotv1.WiretapRecord, 0, filter.Limit)
	next := filter.AfterSequence
	for _, record := range store.wiretap {
		if record.Sequence <= filter.AfterSequence || !matches(record, filter) {
			continue
		}
		values = append(values, cloneWiretap(record))
		next = record.Sequence
		if len(values) == filter.Limit {
			break
		}
	}
	return values, next, nil
}

func (store *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (store *MemoryStore) appendWiretapLocked(record *knotv1.WiretapRecord) {
	record.Sequence = uint64(len(store.wiretap) + 1)
	store.wiretap = append(store.wiretap, record)
}

func applyEvent(message *knotv1.Message, request *knotv1.ApplyEventRequest, occurredAt time.Time) error {
	switch request.Kind {
	case knotv1.MessageEventKind_MESSAGE_EVENT_KIND_EDIT:
		text := strings.TrimSpace(request.Text)
		if text == "" || len(text) > 64<<10 || message.DeletedAtUnixMillis > 0 {
			return ErrInvalidEvent
		}
		message.CurrentText = text
		message.EditedAtUnixMillis = occurredAt.UnixMilli()
	case knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE:
		if message.DeletedAtUnixMillis > 0 {
			return nil
		}
		message.CurrentText = ""
		message.DeletedAtUnixMillis = occurredAt.UnixMilli()
	case knotv1.MessageEventKind_MESSAGE_EVENT_KIND_REACTION:
		if request.Emoji == "" || len(request.Emoji) > 32 {
			return ErrInvalidEvent
		}
		updateReaction(message, request.Emoji, request.ActorUsername, request.Active)
	case knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT:
		switch request.Text {
		case "delivered":
			message.DeliveredAtUnixMillis = occurredAt.UnixMilli()
		case "read":
			message.ReadAtUnixMillis = occurredAt.UnixMilli()
		default:
			return ErrInvalidEvent
		}
	default:
		return ErrInvalidEvent
	}
	return nil
}

func updateReaction(message *knotv1.Message, emoji string, username string, active bool) {
	for _, reaction := range message.Reactions {
		if reaction.Emoji != emoji {
			continue
		}
		index := sort.SearchStrings(reaction.Usernames, username)
		exists := index < len(reaction.Usernames) && reaction.Usernames[index] == username
		if active && !exists {
			reaction.Usernames = append(reaction.Usernames, username)
			sort.Strings(reaction.Usernames)
		}
		if !active && exists {
			reaction.Usernames = append(reaction.Usernames[:index], reaction.Usernames[index+1:]...)
		}
		return
	}
	if active {
		message.Reactions = append(message.Reactions, &knotv1.Reaction{Emoji: emoji, Usernames: []string{username}})
	}
}

func matches(record *knotv1.WiretapRecord, filter WiretapFilter) bool {
	message := record.Message
	if message == nil {
		return false
	}
	if filter.Author != "" && !strings.EqualFold(message.AuthorUsername, filter.Author) {
		return false
	}
	if filter.Participant != "" && !containsFold(message.ParticipantUsernames, filter.Participant) {
		return false
	}
	if filter.Conversation != "" && message.ConversationId != filter.Conversation {
		return false
	}
	if filter.SessionMode != knotv1.SessionMode_SESSION_MODE_UNSPECIFIED && record.SessionMode != filter.SessionMode {
		return false
	}
	return filter.EventKind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_UNSPECIFIED || record.EventKind == filter.EventKind
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

func cloneMessage(value *knotv1.Message) *knotv1.Message {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*knotv1.Message)
}

func cloneWiretap(value *knotv1.WiretapRecord) *knotv1.WiretapRecord {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*knotv1.WiretapRecord)
}

func validateMessage(message *knotv1.Message) error {
	if message == nil || message.ClientCommandId == "" || message.ConversationId == "" || message.AuthorUserId == "" || message.AuthorUsername == "" || message.SessionId == "" || message.SessionMode == knotv1.SessionMode_SESSION_MODE_UNSPECIFIED || message.ConversationKind == knotv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED || message.Kind == knotv1.MessageKind_MESSAGE_KIND_UNSPECIFIED || len(message.ParticipantUserIds) == 0 || len(message.ParticipantUserIds) != len(message.ParticipantUsernames) {
		return fmt.Errorf("%w: incomplete message", ErrInvalidEvent)
	}
	if message.Kind == knotv1.MessageKind_MESSAGE_KIND_TEXT && strings.TrimSpace(message.OriginalText) == "" {
		return fmt.Errorf("%w: empty message", ErrInvalidEvent)
	}
	if message.Kind == knotv1.MessageKind_MESSAGE_KIND_ATTACHMENT && message.AttachmentId == "" {
		return fmt.Errorf("%w: missing attachment", ErrInvalidEvent)
	}
	if len(message.OriginalText) > 64<<10 {
		return fmt.Errorf("%w: message too large", ErrInvalidEvent)
	}
	return nil
}
