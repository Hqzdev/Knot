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
	achievements map[string]*knotv1.Achievement
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now:          time.Now,
		messages:     make(map[string]*knotv1.Message),
		commands:     make(map[string]string),
		events:       make(map[string]string),
		achievements: make(map[string]*knotv1.Achievement),
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
	store.prepareForwardChainLocked(message, now)
	message.Sequence = uint64(len(store.messageOrder) + 1)
	message.Id = "msg_" + messageID
	message.CreatedAtUnixMillis = now.UnixMilli()
	message.ServerSeenAtUnixMillis = now.UnixMilli()
	message.DeliveredAtUnixMillis = now.UnixMilli()
	if message.CurrentSourceText == "" {
		message.CurrentSourceText = message.OriginalText
	}
	message.CurrentText = projectedText(message.CurrentSourceText, message.TextEffect)
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
		Device:               message.AuthorDevice,
		OccurredAtUnixMillis: now.UnixMilli(),
	})
	store.unlockMessageAchievementsLocked(message)
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
	if request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_RECEIPT && hasReceipt(message, request.ActorUserId, request.GetDevice().GetDeviceId()) {
		return cloneMessage(message), true, nil
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
		Device:               request.Device,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	})
	if request.Kind == knotv1.MessageEventKind_MESSAGE_EVENT_KIND_DELETE {
		store.unlockLocked(message.AuthorUsername, "delete_attempt", "Tried To Hide It", message.Id, occurredAt)
	}
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
		values = append(values, projectForUser(message, userID, time.Now().UTC()))
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

func (store *MemoryStore) Dossier(ctx context.Context, username string, limit int) ([]*knotv1.WiretapRecord, []*knotv1.Message, []*knotv1.Achievement, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, false, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	records := make([]*knotv1.WiretapRecord, 0, limit)
	messagesByID := make(map[string]*knotv1.Message)
	truncated := false
	for _, record := range store.wiretap {
		if !recordMatchesUsername(record, username) {
			continue
		}
		if len(records) == limit {
			truncated = true
			break
		}
		cloned := cloneWiretap(record)
		records = append(records, cloned)
		messagesByID[cloned.Message.Id] = cloneMessage(cloned.Message)
	}
	messages := make([]*knotv1.Message, 0, len(messagesByID))
	for _, message := range messagesByID {
		messages = append(messages, message)
	}
	sort.Slice(messages, func(left int, right int) bool { return messages[left].Sequence < messages[right].Sequence })
	achievements := make([]*knotv1.Achievement, 0)
	for _, achievement := range store.achievements {
		if strings.EqualFold(achievement.Username, username) {
			achievements = append(achievements, proto.Clone(achievement).(*knotv1.Achievement))
		}
	}
	sort.Slice(achievements, func(left int, right int) bool {
		return achievements[left].UnlockedAtUnixMillis < achievements[right].UnlockedAtUnixMillis
	})
	return records, messages, achievements, truncated, nil
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
		message.CurrentSourceText = text
		message.CurrentText = projectedText(text, message.TextEffect)
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
			message.ReadReceipts = append(message.ReadReceipts, &knotv1.ReadReceipt{
				UserId:           request.ActorUserId,
				Username:         request.ActorUsername,
				SessionId:        request.SessionId,
				SessionMode:      request.SessionMode,
				Device:           cloneDevice(request.Device),
				ReadAtUnixMillis: occurredAt.UnixMilli(),
			})
		default:
			return ErrInvalidEvent
		}
	default:
		return ErrInvalidEvent
	}
	return nil
}

func projectedText(value string, effect knotv1.TextEffect) string {
	switch effect {
	case knotv1.TextEffect_TEXT_EFFECT_BUREAUCRATIC:
		return "Regarding the matter of: " + value + ". Please take this information into consideration and ensure appropriate follow-up."
	case knotv1.TextEffect_TEXT_EFFECT_CAESAR3:
		return caesar3(value)
	default:
		return value
	}
}

func caesar3(value string) string {
	characters := []rune(value)
	for index, character := range characters {
		switch {
		case character >= 'a' && character <= 'z':
			characters[index] = 'a' + (character-'a'+3)%26
		case character >= 'A' && character <= 'Z':
			characters[index] = 'A' + (character-'A'+3)%26
		}
	}
	return string(characters)
}

func hasReceipt(message *knotv1.Message, userID string, deviceID string) bool {
	if deviceID == "" {
		return false
	}
	for _, receipt := range message.ReadReceipts {
		if receipt.UserId == userID && receipt.GetDevice().GetDeviceId() == deviceID {
			return true
		}
	}
	return false
}

func cloneDevice(value *knotv1.DeviceDescriptor) *knotv1.DeviceDescriptor {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*knotv1.DeviceDescriptor)
}

func (store *MemoryStore) prepareForwardChainLocked(message *knotv1.Message, occurredAt time.Time) {
	if message.ForwardedFromId == "" {
		return
	}
	source := store.messages[message.ForwardedFromId]
	if source == nil {
		return
	}
	message.ForwardChain = append(message.ForwardChain, source.ForwardChain...)
	message.ForwardChain = append(message.ForwardChain, &knotv1.ForwardHop{
		MessageId:            source.Id,
		ConversationId:       source.ConversationId,
		AuthorUsername:       source.AuthorUsername,
		OccurredAtUnixMillis: occurredAt.UnixMilli(),
	})
}

func (store *MemoryStore) unlockMessageAchievementsLocked(message *knotv1.Message) {
	occurredAt := time.UnixMilli(message.CreatedAtUnixMillis).UTC()
	if message.SessionMode == knotv1.SessionMode_SESSION_MODE_IMPERSONATED {
		store.unlockLocked(message.AuthorUsername, "impersonated_action", "Borrowed Identity", message.Id, occurredAt)
	}
	if message.DeliveryMode == knotv1.DeliveryMode_DELIVERY_MODE_UNRELIABLE && message.RequestedConversationId != message.ConversationId {
		store.unlockLocked(message.AuthorUsername, "unreliable_delivery", "Wrong Room, Real Message", message.Id, occurredAt)
	}
	if message.GetVoice().GetDurationMillis() >= int64(9*time.Minute/time.Millisecond) {
		store.unlockLocked(message.AuthorUsername, "nine_minute_voice", "Nine Minute Broadcast", message.Id, occurredAt)
	}
	if passwordShaped(message.OriginalText) {
		store.unlockLocked(message.AuthorUsername, "password_shaped_text", "Password-Shaped Text", message.Id, occurredAt)
	}
	if source := store.messages[message.ReplyToId]; source != nil && message.CreatedAtUnixMillis-source.CreatedAtUnixMillis >= int64(14*24*time.Hour/time.Millisecond) {
		store.unlockLocked(message.AuthorUsername, "late_reply", "Replied Two Weeks Later", message.Id, occurredAt)
	}
}

func (store *MemoryStore) unlockLocked(username string, kind string, title string, messageID string, occurredAt time.Time) {
	key := strings.ToLower(username) + ":" + kind
	if store.achievements[key] != nil {
		return
	}
	store.achievements[key] = &knotv1.Achievement{
		Id:                   "ach_" + strings.ReplaceAll(key, ":", "_"),
		Username:             username,
		Kind:                 kind,
		Title:                title,
		EvidenceMessageId:    messageID,
		UnlockedAtUnixMillis: occurredAt.UnixMilli(),
	}
}

func passwordShaped(value string) bool {
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "password=") || strings.Contains(normalized, "password:") || strings.Contains(normalized, "passwd=") || strings.Contains(normalized, "пароль")
}

func recordMatchesUsername(record *knotv1.WiretapRecord, username string) bool {
	if record == nil || record.Message == nil {
		return false
	}
	return strings.EqualFold(record.ActorUsername, username) ||
		strings.EqualFold(record.Message.AuthorUsername, username) ||
		containsFold(record.Message.ParticipantUsernames, username)
}

func projectForUser(message *knotv1.Message, userID string, now time.Time) *knotv1.Message {
	value := cloneMessage(message)
	if value.AuthorUserId == userID && value.AuthorHideAtUnixMillis > 0 && value.AuthorHideAtUnixMillis <= now.UnixMilli() {
		value.CurrentText = ""
		value.AuthorProjectionHidden = true
	}
	return value
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
