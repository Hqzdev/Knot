package knotv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type RouteKind int32

const (
	RouteKind_ROUTE_KIND_UNSPECIFIED RouteKind = 0
	RouteKind_ROUTE_KIND_LIVE        RouteKind = 1
	RouteKind_ROUTE_KIND_QUEUED      RouteKind = 2
)

var (
	RouteKind_name = map[int32]string{
		0: "ROUTE_KIND_UNSPECIFIED",
		1: "ROUTE_KIND_LIVE",
		2: "ROUTE_KIND_QUEUED",
	}
	RouteKind_value = map[string]int32{
		"ROUTE_KIND_UNSPECIFIED": 0,
		"ROUTE_KIND_LIVE":        1,
		"ROUTE_KIND_QUEUED":      2,
	}
)

func (x RouteKind) Enum() *RouteKind {
	p := new(RouteKind)
	*p = x
	return p
}

func (x RouteKind) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (RouteKind) Descriptor() protoreflect.EnumDescriptor {
	return file_knot_v1_messaging_proto_enumTypes[0].Descriptor()
}

func (RouteKind) Type() protoreflect.EnumType {
	return &file_knot_v1_messaging_proto_enumTypes[0]
}

func (x RouteKind) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

func (RouteKind) EnumDescriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{0}
}

type AuthorizeConnectionRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	UserId        string                 `protobuf:"bytes,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	DeviceId      string                 `protobuf:"bytes,2,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *AuthorizeConnectionRequest) Reset() {
	*x = AuthorizeConnectionRequest{}
	mi := &file_knot_v1_messaging_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AuthorizeConnectionRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AuthorizeConnectionRequest) ProtoMessage() {}

func (x *AuthorizeConnectionRequest) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*AuthorizeConnectionRequest) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{0}
}

func (x *AuthorizeConnectionRequest) GetUserId() string {
	if x != nil {
		return x.UserId
	}
	return ""
}

func (x *AuthorizeConnectionRequest) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

type AuthorizeConnectionResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Active        bool                   `protobuf:"varint,1,opt,name=active,proto3" json:"active,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *AuthorizeConnectionResponse) Reset() {
	*x = AuthorizeConnectionResponse{}
	mi := &file_knot_v1_messaging_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AuthorizeConnectionResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AuthorizeConnectionResponse) ProtoMessage() {}

func (x *AuthorizeConnectionResponse) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*AuthorizeConnectionResponse) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{1}
}

func (x *AuthorizeConnectionResponse) GetActive() bool {
	if x != nil {
		return x.Active
	}
	return false
}

type DeviceEnvelope struct {
	state             protoimpl.MessageState `protogen:"open.v1"`
	RecipientDeviceId string                 `protobuf:"bytes,1,opt,name=recipient_device_id,json=recipientDeviceId,proto3" json:"recipient_device_id,omitempty"`
	Ciphertext        []byte                 `protobuf:"bytes,2,opt,name=ciphertext,proto3" json:"ciphertext,omitempty"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *DeviceEnvelope) Reset() {
	*x = DeviceEnvelope{}
	mi := &file_knot_v1_messaging_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeviceEnvelope) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeviceEnvelope) ProtoMessage() {}

func (x *DeviceEnvelope) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*DeviceEnvelope) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{2}
}

func (x *DeviceEnvelope) GetRecipientDeviceId() string {
	if x != nil {
		return x.RecipientDeviceId
	}
	return ""
}

func (x *DeviceEnvelope) GetCiphertext() []byte {
	if x != nil {
		return x.Ciphertext
	}
	return nil
}

type RouteMessageRequest struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	MessageId       string                 `protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	SenderUserId    string                 `protobuf:"bytes,2,opt,name=sender_user_id,json=senderUserId,proto3" json:"sender_user_id,omitempty"`
	SenderDeviceId  string                 `protobuf:"bytes,3,opt,name=sender_device_id,json=senderDeviceId,proto3" json:"sender_device_id,omitempty"`
	RecipientUserId string                 `protobuf:"bytes,4,opt,name=recipient_user_id,json=recipientUserId,proto3" json:"recipient_user_id,omitempty"`
	Envelopes       []*DeviceEnvelope      `protobuf:"bytes,5,rep,name=envelopes,proto3" json:"envelopes,omitempty"`
	GroupId         string                 `protobuf:"bytes,6,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	GroupRevision   uint64                 `protobuf:"varint,7,opt,name=group_revision,json=groupRevision,proto3" json:"group_revision,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *RouteMessageRequest) Reset() {
	*x = RouteMessageRequest{}
	mi := &file_knot_v1_messaging_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RouteMessageRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RouteMessageRequest) ProtoMessage() {}

func (x *RouteMessageRequest) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*RouteMessageRequest) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{3}
}

func (x *RouteMessageRequest) GetMessageId() string {
	if x != nil {
		return x.MessageId
	}
	return ""
}

func (x *RouteMessageRequest) GetSenderUserId() string {
	if x != nil {
		return x.SenderUserId
	}
	return ""
}

func (x *RouteMessageRequest) GetSenderDeviceId() string {
	if x != nil {
		return x.SenderDeviceId
	}
	return ""
}

func (x *RouteMessageRequest) GetRecipientUserId() string {
	if x != nil {
		return x.RecipientUserId
	}
	return ""
}

func (x *RouteMessageRequest) GetEnvelopes() []*DeviceEnvelope {
	if x != nil {
		return x.Envelopes
	}
	return nil
}

func (x *RouteMessageRequest) GetGroupId() string {
	if x != nil {
		return x.GroupId
	}
	return ""
}

func (x *RouteMessageRequest) GetGroupRevision() uint64 {
	if x != nil {
		return x.GroupRevision
	}
	return 0
}

type DeviceRoute struct {
	state             protoimpl.MessageState `protogen:"open.v1"`
	RecipientDeviceId string                 `protobuf:"bytes,1,opt,name=recipient_device_id,json=recipientDeviceId,proto3" json:"recipient_device_id,omitempty"`
	Kind              RouteKind              `protobuf:"varint,2,opt,name=kind,proto3,enum=knot.v1.RouteKind" json:"kind,omitempty"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *DeviceRoute) Reset() {
	*x = DeviceRoute{}
	mi := &file_knot_v1_messaging_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeviceRoute) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeviceRoute) ProtoMessage() {}

func (x *DeviceRoute) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*DeviceRoute) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{4}
}

func (x *DeviceRoute) GetRecipientDeviceId() string {
	if x != nil {
		return x.RecipientDeviceId
	}
	return ""
}

func (x *DeviceRoute) GetKind() RouteKind {
	if x != nil {
		return x.Kind
	}
	return RouteKind_ROUTE_KIND_UNSPECIFIED
}

type RouteMessageResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	MessageId     string                 `protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	Duplicate     bool                   `protobuf:"varint,2,opt,name=duplicate,proto3" json:"duplicate,omitempty"`
	Routes        []*DeviceRoute         `protobuf:"bytes,3,rep,name=routes,proto3" json:"routes,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *RouteMessageResponse) Reset() {
	*x = RouteMessageResponse{}
	mi := &file_knot_v1_messaging_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RouteMessageResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RouteMessageResponse) ProtoMessage() {}

func (x *RouteMessageResponse) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*RouteMessageResponse) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{5}
}

func (x *RouteMessageResponse) GetMessageId() string {
	if x != nil {
		return x.MessageId
	}
	return ""
}

func (x *RouteMessageResponse) GetDuplicate() bool {
	if x != nil {
		return x.Duplicate
	}
	return false
}

func (x *RouteMessageResponse) GetRoutes() []*DeviceRoute {
	if x != nil {
		return x.Routes
	}
	return nil
}

type DeliveryEnvelope struct {
	state               protoimpl.MessageState `protogen:"open.v1"`
	MessageId           string                 `protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	RecipientUserId     string                 `protobuf:"bytes,2,opt,name=recipient_user_id,json=recipientUserId,proto3" json:"recipient_user_id,omitempty"`
	RecipientDeviceId   string                 `protobuf:"bytes,3,opt,name=recipient_device_id,json=recipientDeviceId,proto3" json:"recipient_device_id,omitempty"`
	SenderUserId        string                 `protobuf:"bytes,4,opt,name=sender_user_id,json=senderUserId,proto3" json:"sender_user_id,omitempty"`
	SenderDeviceId      string                 `protobuf:"bytes,5,opt,name=sender_device_id,json=senderDeviceId,proto3" json:"sender_device_id,omitempty"`
	Ciphertext          []byte                 `protobuf:"bytes,6,opt,name=ciphertext,proto3" json:"ciphertext,omitempty"`
	CreatedAtUnixMillis int64                  `protobuf:"varint,7,opt,name=created_at_unix_millis,json=createdAtUnixMillis,proto3" json:"created_at_unix_millis,omitempty"`
	SenderUsername      string                 `protobuf:"bytes,8,opt,name=sender_username,json=senderUsername,proto3" json:"sender_username,omitempty"`
	GroupId             string                 `protobuf:"bytes,9,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	GroupRevision       uint64                 `protobuf:"varint,10,opt,name=group_revision,json=groupRevision,proto3" json:"group_revision,omitempty"`
	unknownFields       protoimpl.UnknownFields
	sizeCache           protoimpl.SizeCache
}

func (x *DeliveryEnvelope) Reset() {
	*x = DeliveryEnvelope{}
	mi := &file_knot_v1_messaging_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *DeliveryEnvelope) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeliveryEnvelope) ProtoMessage() {}

func (x *DeliveryEnvelope) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*DeliveryEnvelope) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{6}
}

func (x *DeliveryEnvelope) GetMessageId() string {
	if x != nil {
		return x.MessageId
	}
	return ""
}

func (x *DeliveryEnvelope) GetRecipientUserId() string {
	if x != nil {
		return x.RecipientUserId
	}
	return ""
}

func (x *DeliveryEnvelope) GetRecipientDeviceId() string {
	if x != nil {
		return x.RecipientDeviceId
	}
	return ""
}

func (x *DeliveryEnvelope) GetSenderUserId() string {
	if x != nil {
		return x.SenderUserId
	}
	return ""
}

func (x *DeliveryEnvelope) GetSenderDeviceId() string {
	if x != nil {
		return x.SenderDeviceId
	}
	return ""
}

func (x *DeliveryEnvelope) GetCiphertext() []byte {
	if x != nil {
		return x.Ciphertext
	}
	return nil
}

func (x *DeliveryEnvelope) GetCreatedAtUnixMillis() int64 {
	if x != nil {
		return x.CreatedAtUnixMillis
	}
	return 0
}

func (x *DeliveryEnvelope) GetSenderUsername() string {
	if x != nil {
		return x.SenderUsername
	}
	return ""
}

func (x *DeliveryEnvelope) GetGroupId() string {
	if x != nil {
		return x.GroupId
	}
	return ""
}

func (x *DeliveryEnvelope) GetGroupRevision() uint64 {
	if x != nil {
		return x.GroupRevision
	}
	return 0
}

type EnqueueRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Envelope      *DeliveryEnvelope      `protobuf:"bytes,1,opt,name=envelope,proto3" json:"envelope,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnqueueRequest) Reset() {
	*x = EnqueueRequest{}
	mi := &file_knot_v1_messaging_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnqueueRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnqueueRequest) ProtoMessage() {}

func (x *EnqueueRequest) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*EnqueueRequest) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{7}
}

func (x *EnqueueRequest) GetEnvelope() *DeliveryEnvelope {
	if x != nil {
		return x.Envelope
	}
	return nil
}

type EnqueueResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Duplicate     bool                   `protobuf:"varint,1,opt,name=duplicate,proto3" json:"duplicate,omitempty"`
	Sequence      uint64                 `protobuf:"varint,2,opt,name=sequence,proto3" json:"sequence,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *EnqueueResponse) Reset() {
	*x = EnqueueResponse{}
	mi := &file_knot_v1_messaging_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *EnqueueResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*EnqueueResponse) ProtoMessage() {}

func (x *EnqueueResponse) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*EnqueueResponse) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{8}
}

func (x *EnqueueResponse) GetDuplicate() bool {
	if x != nil {
		return x.Duplicate
	}
	return false
}

func (x *EnqueueResponse) GetSequence() uint64 {
	if x != nil {
		return x.Sequence
	}
	return 0
}

type SyncRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	UserId        string                 `protobuf:"bytes,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	DeviceId      string                 `protobuf:"bytes,2,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	Cursor        string                 `protobuf:"bytes,3,opt,name=cursor,proto3" json:"cursor,omitempty"`
	Limit         uint32                 `protobuf:"varint,4,opt,name=limit,proto3" json:"limit,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *SyncRequest) Reset() {
	*x = SyncRequest{}
	mi := &file_knot_v1_messaging_proto_msgTypes[9]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SyncRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SyncRequest) ProtoMessage() {}

func (x *SyncRequest) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[9]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*SyncRequest) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{9}
}

func (x *SyncRequest) GetUserId() string {
	if x != nil {
		return x.UserId
	}
	return ""
}

func (x *SyncRequest) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

func (x *SyncRequest) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *SyncRequest) GetLimit() uint32 {
	if x != nil {
		return x.Limit
	}
	return 0
}

type SyncedEnvelope struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Envelope      *DeliveryEnvelope      `protobuf:"bytes,1,opt,name=envelope,proto3" json:"envelope,omitempty"`
	Cursor        string                 `protobuf:"bytes,2,opt,name=cursor,proto3" json:"cursor,omitempty"`
	AckToken      string                 `protobuf:"bytes,3,opt,name=ack_token,json=ackToken,proto3" json:"ack_token,omitempty"`
	Redelivered   bool                   `protobuf:"varint,4,opt,name=redelivered,proto3" json:"redelivered,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *SyncedEnvelope) Reset() {
	*x = SyncedEnvelope{}
	mi := &file_knot_v1_messaging_proto_msgTypes[10]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SyncedEnvelope) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SyncedEnvelope) ProtoMessage() {}

func (x *SyncedEnvelope) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[10]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*SyncedEnvelope) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{10}
}

func (x *SyncedEnvelope) GetEnvelope() *DeliveryEnvelope {
	if x != nil {
		return x.Envelope
	}
	return nil
}

func (x *SyncedEnvelope) GetCursor() string {
	if x != nil {
		return x.Cursor
	}
	return ""
}

func (x *SyncedEnvelope) GetAckToken() string {
	if x != nil {
		return x.AckToken
	}
	return ""
}

func (x *SyncedEnvelope) GetRedelivered() bool {
	if x != nil {
		return x.Redelivered
	}
	return false
}

type SyncResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Messages      []*SyncedEnvelope      `protobuf:"bytes,1,rep,name=messages,proto3" json:"messages,omitempty"`
	NextCursor    string                 `protobuf:"bytes,2,opt,name=next_cursor,json=nextCursor,proto3" json:"next_cursor,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *SyncResponse) Reset() {
	*x = SyncResponse{}
	mi := &file_knot_v1_messaging_proto_msgTypes[11]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SyncResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SyncResponse) ProtoMessage() {}

func (x *SyncResponse) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[11]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*SyncResponse) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{11}
}

func (x *SyncResponse) GetMessages() []*SyncedEnvelope {
	if x != nil {
		return x.Messages
	}
	return nil
}

func (x *SyncResponse) GetNextCursor() string {
	if x != nil {
		return x.NextCursor
	}
	return ""
}

type Acknowledgement struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	MessageId     string                 `protobuf:"bytes,1,opt,name=message_id,json=messageId,proto3" json:"message_id,omitempty"`
	AckToken      string                 `protobuf:"bytes,2,opt,name=ack_token,json=ackToken,proto3" json:"ack_token,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Acknowledgement) Reset() {
	*x = Acknowledgement{}
	mi := &file_knot_v1_messaging_proto_msgTypes[12]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Acknowledgement) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Acknowledgement) ProtoMessage() {}

func (x *Acknowledgement) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[12]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*Acknowledgement) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{12}
}

func (x *Acknowledgement) GetMessageId() string {
	if x != nil {
		return x.MessageId
	}
	return ""
}

func (x *Acknowledgement) GetAckToken() string {
	if x != nil {
		return x.AckToken
	}
	return ""
}

type AcknowledgeRequest struct {
	state            protoimpl.MessageState `protogen:"open.v1"`
	UserId           string                 `protobuf:"bytes,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	DeviceId         string                 `protobuf:"bytes,2,opt,name=device_id,json=deviceId,proto3" json:"device_id,omitempty"`
	Acknowledgements []*Acknowledgement     `protobuf:"bytes,3,rep,name=acknowledgements,proto3" json:"acknowledgements,omitempty"`
	unknownFields    protoimpl.UnknownFields
	sizeCache        protoimpl.SizeCache
}

func (x *AcknowledgeRequest) Reset() {
	*x = AcknowledgeRequest{}
	mi := &file_knot_v1_messaging_proto_msgTypes[13]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AcknowledgeRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AcknowledgeRequest) ProtoMessage() {}

func (x *AcknowledgeRequest) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[13]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*AcknowledgeRequest) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{13}
}

func (x *AcknowledgeRequest) GetUserId() string {
	if x != nil {
		return x.UserId
	}
	return ""
}

func (x *AcknowledgeRequest) GetDeviceId() string {
	if x != nil {
		return x.DeviceId
	}
	return ""
}

func (x *AcknowledgeRequest) GetAcknowledgements() []*Acknowledgement {
	if x != nil {
		return x.Acknowledgements
	}
	return nil
}

type AcknowledgeResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Acknowledged  uint32                 `protobuf:"varint,1,opt,name=acknowledged,proto3" json:"acknowledged,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *AcknowledgeResponse) Reset() {
	*x = AcknowledgeResponse{}
	mi := &file_knot_v1_messaging_proto_msgTypes[14]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *AcknowledgeResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AcknowledgeResponse) ProtoMessage() {}

func (x *AcknowledgeResponse) ProtoReflect() protoreflect.Message {
	mi := &file_knot_v1_messaging_proto_msgTypes[14]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (*AcknowledgeResponse) Descriptor() ([]byte, []int) {
	return file_knot_v1_messaging_proto_rawDescGZIP(), []int{14}
}

func (x *AcknowledgeResponse) GetAcknowledged() uint32 {
	if x != nil {
		return x.Acknowledged
	}
	return 0
}

var File_knot_v1_messaging_proto protoreflect.FileDescriptor

const file_knot_v1_messaging_proto_rawDesc = "" +
	"\n" +
	"\x17knot/v1/messaging.proto\x12\aknot.v1\"R\n" +
	"\x1aAuthorizeConnectionRequest\x12\x17\n" +
	"\auser_id\x18\x01 \x01(\tR\x06userId\x12\x1b\n" +
	"\tdevice_id\x18\x02 \x01(\tR\bdeviceId\"5\n" +
	"\x1bAuthorizeConnectionResponse\x12\x16\n" +
	"\x06active\x18\x01 \x01(\bR\x06active\"`\n" +
	"\x0eDeviceEnvelope\x12.\n" +
	"\x13recipient_device_id\x18\x01 \x01(\tR\x11recipientDeviceId\x12\x1e\n" +
	"\n" +
	"ciphertext\x18\x02 \x01(\fR\n" +
	"ciphertext\"\xa9\x02\n" +
	"\x13RouteMessageRequest\x12\x1d\n" +
	"\n" +
	"message_id\x18\x01 \x01(\tR\tmessageId\x12$\n" +
	"\x0esender_user_id\x18\x02 \x01(\tR\fsenderUserId\x12(\n" +
	"\x10sender_device_id\x18\x03 \x01(\tR\x0esenderDeviceId\x12*\n" +
	"\x11recipient_user_id\x18\x04 \x01(\tR\x0frecipientUserId\x125\n" +
	"\tenvelopes\x18\x05 \x03(\v2\x17.knot.v1.DeviceEnvelopeR\tenvelopes\x12\x19\n" +
	"\bgroup_id\x18\x06 \x01(\tR\agroupId\x12%\n" +
	"\x0egroup_revision\x18\a \x01(\x04R\rgroupRevision\"e\n" +
	"\vDeviceRoute\x12.\n" +
	"\x13recipient_device_id\x18\x01 \x01(\tR\x11recipientDeviceId\x12&\n" +
	"\x04kind\x18\x02 \x01(\x0e2\x12.knot.v1.RouteKindR\x04kind\"\x81\x01\n" +
	"\x14RouteMessageResponse\x12\x1d\n" +
	"\n" +
	"message_id\x18\x01 \x01(\tR\tmessageId\x12\x1c\n" +
	"\tduplicate\x18\x02 \x01(\bR\tduplicate\x12,\n" +
	"\x06routes\x18\x03 \x03(\v2\x14.knot.v1.DeviceRouteR\x06routes\"\x9d\x03\n" +
	"\x10DeliveryEnvelope\x12\x1d\n" +
	"\n" +
	"message_id\x18\x01 \x01(\tR\tmessageId\x12*\n" +
	"\x11recipient_user_id\x18\x02 \x01(\tR\x0frecipientUserId\x12.\n" +
	"\x13recipient_device_id\x18\x03 \x01(\tR\x11recipientDeviceId\x12$\n" +
	"\x0esender_user_id\x18\x04 \x01(\tR\fsenderUserId\x12(\n" +
	"\x10sender_device_id\x18\x05 \x01(\tR\x0esenderDeviceId\x12\x1e\n" +
	"\n" +
	"ciphertext\x18\x06 \x01(\fR\n" +
	"ciphertext\x123\n" +
	"\x16created_at_unix_millis\x18\a \x01(\x03R\x13createdAtUnixMillis\x12'\n" +
	"\x0fsender_username\x18\b \x01(\tR\x0esenderUsername\x12\x19\n" +
	"\bgroup_id\x18\t \x01(\tR\agroupId\x12%\n" +
	"\x0egroup_revision\x18\n" +
	" \x01(\x04R\rgroupRevision\"G\n" +
	"\x0eEnqueueRequest\x125\n" +
	"\benvelope\x18\x01 \x01(\v2\x19.knot.v1.DeliveryEnvelopeR\benvelope\"K\n" +
	"\x0fEnqueueResponse\x12\x1c\n" +
	"\tduplicate\x18\x01 \x01(\bR\tduplicate\x12\x1a\n" +
	"\bsequence\x18\x02 \x01(\x04R\bsequence\"q\n" +
	"\vSyncRequest\x12\x17\n" +
	"\auser_id\x18\x01 \x01(\tR\x06userId\x12\x1b\n" +
	"\tdevice_id\x18\x02 \x01(\tR\bdeviceId\x12\x16\n" +
	"\x06cursor\x18\x03 \x01(\tR\x06cursor\x12\x14\n" +
	"\x05limit\x18\x04 \x01(\rR\x05limit\"\x9e\x01\n" +
	"\x0eSyncedEnvelope\x125\n" +
	"\benvelope\x18\x01 \x01(\v2\x19.knot.v1.DeliveryEnvelopeR\benvelope\x12\x16\n" +
	"\x06cursor\x18\x02 \x01(\tR\x06cursor\x12\x1b\n" +
	"\tack_token\x18\x03 \x01(\tR\backToken\x12 \n" +
	"\vredelivered\x18\x04 \x01(\bR\vredelivered\"d\n" +
	"\fSyncResponse\x123\n" +
	"\bmessages\x18\x01 \x03(\v2\x17.knot.v1.SyncedEnvelopeR\bmessages\x12\x1f\n" +
	"\vnext_cursor\x18\x02 \x01(\tR\n" +
	"nextCursor\"M\n" +
	"\x0fAcknowledgement\x12\x1d\n" +
	"\n" +
	"message_id\x18\x01 \x01(\tR\tmessageId\x12\x1b\n" +
	"\tack_token\x18\x02 \x01(\tR\backToken\"\x90\x01\n" +
	"\x12AcknowledgeRequest\x12\x17\n" +
	"\auser_id\x18\x01 \x01(\tR\x06userId\x12\x1b\n" +
	"\tdevice_id\x18\x02 \x01(\tR\bdeviceId\x12D\n" +
	"\x10acknowledgements\x18\x03 \x03(\v2\x18.knot.v1.AcknowledgementR\x10acknowledgements\"9\n" +
	"\x13AcknowledgeResponse\x12\"\n" +
	"\facknowledged\x18\x01 \x01(\rR\facknowledged*S\n" +
	"\tRouteKind\x12\x1a\n" +
	"\x16ROUTE_KIND_UNSPECIFIED\x10\x00\x12\x13\n" +
	"\x0fROUTE_KIND_LIVE\x10\x01\x12\x15\n" +
	"\x11ROUTE_KIND_QUEUED\x10\x022\xbe\x01\n" +
	"\rRouterService\x12`\n" +
	"\x13AuthorizeConnection\x12#.knot.v1.AuthorizeConnectionRequest\x1a$.knot.v1.AuthorizeConnectionResponse\x12K\n" +
	"\fRouteMessage\x12\x1c.knot.v1.RouteMessageRequest\x1a\x1d.knot.v1.RouteMessageResponse2\xce\x01\n" +
	"\x0fDeliveryService\x12<\n" +
	"\aEnqueue\x12\x17.knot.v1.EnqueueRequest\x1a\x18.knot.v1.EnqueueResponse\x123\n" +
	"\x04Sync\x12\x14.knot.v1.SyncRequest\x1a\x15.knot.v1.SyncResponse\x12H\n" +
	"\vAcknowledge\x12\x1b.knot.v1.AcknowledgeRequest\x1a\x1c.knot.v1.AcknowledgeResponseB@Z>github.com/yaroslavfairfieldd/knot/proto/gen/go/knot/v1;knotv1b\x06proto3"

var (
	file_knot_v1_messaging_proto_rawDescOnce sync.Once
	file_knot_v1_messaging_proto_rawDescData []byte
)

func file_knot_v1_messaging_proto_rawDescGZIP() []byte {
	file_knot_v1_messaging_proto_rawDescOnce.Do(func() {
		file_knot_v1_messaging_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_knot_v1_messaging_proto_rawDesc), len(file_knot_v1_messaging_proto_rawDesc)))
	})
	return file_knot_v1_messaging_proto_rawDescData
}

var file_knot_v1_messaging_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_knot_v1_messaging_proto_msgTypes = make([]protoimpl.MessageInfo, 15)
var file_knot_v1_messaging_proto_goTypes = []any{
	(RouteKind)(0),                      // 0: knot.v1.RouteKind
	(*AuthorizeConnectionRequest)(nil),  // 1: knot.v1.AuthorizeConnectionRequest
	(*AuthorizeConnectionResponse)(nil), // 2: knot.v1.AuthorizeConnectionResponse
	(*DeviceEnvelope)(nil),              // 3: knot.v1.DeviceEnvelope
	(*RouteMessageRequest)(nil),         // 4: knot.v1.RouteMessageRequest
	(*DeviceRoute)(nil),                 // 5: knot.v1.DeviceRoute
	(*RouteMessageResponse)(nil),        // 6: knot.v1.RouteMessageResponse
	(*DeliveryEnvelope)(nil),            // 7: knot.v1.DeliveryEnvelope
	(*EnqueueRequest)(nil),              // 8: knot.v1.EnqueueRequest
	(*EnqueueResponse)(nil),             // 9: knot.v1.EnqueueResponse
	(*SyncRequest)(nil),                 // 10: knot.v1.SyncRequest
	(*SyncedEnvelope)(nil),              // 11: knot.v1.SyncedEnvelope
	(*SyncResponse)(nil),                // 12: knot.v1.SyncResponse
	(*Acknowledgement)(nil),             // 13: knot.v1.Acknowledgement
	(*AcknowledgeRequest)(nil),          // 14: knot.v1.AcknowledgeRequest
	(*AcknowledgeResponse)(nil),         // 15: knot.v1.AcknowledgeResponse
}
var file_knot_v1_messaging_proto_depIdxs = []int32{
	3,  // 0: knot.v1.RouteMessageRequest.envelopes:type_name -> knot.v1.DeviceEnvelope
	0,  // 1: knot.v1.DeviceRoute.kind:type_name -> knot.v1.RouteKind
	5,  // 2: knot.v1.RouteMessageResponse.routes:type_name -> knot.v1.DeviceRoute
	7,  // 3: knot.v1.EnqueueRequest.envelope:type_name -> knot.v1.DeliveryEnvelope
	7,  // 4: knot.v1.SyncedEnvelope.envelope:type_name -> knot.v1.DeliveryEnvelope
	11, // 5: knot.v1.SyncResponse.messages:type_name -> knot.v1.SyncedEnvelope
	13, // 6: knot.v1.AcknowledgeRequest.acknowledgements:type_name -> knot.v1.Acknowledgement
	1,  // 7: knot.v1.RouterService.AuthorizeConnection:input_type -> knot.v1.AuthorizeConnectionRequest
	4,  // 8: knot.v1.RouterService.RouteMessage:input_type -> knot.v1.RouteMessageRequest
	8,  // 9: knot.v1.DeliveryService.Enqueue:input_type -> knot.v1.EnqueueRequest
	10, // 10: knot.v1.DeliveryService.Sync:input_type -> knot.v1.SyncRequest
	14, // 11: knot.v1.DeliveryService.Acknowledge:input_type -> knot.v1.AcknowledgeRequest
	2,  // 12: knot.v1.RouterService.AuthorizeConnection:output_type -> knot.v1.AuthorizeConnectionResponse
	6,  // 13: knot.v1.RouterService.RouteMessage:output_type -> knot.v1.RouteMessageResponse
	9,  // 14: knot.v1.DeliveryService.Enqueue:output_type -> knot.v1.EnqueueResponse
	12, // 15: knot.v1.DeliveryService.Sync:output_type -> knot.v1.SyncResponse
	15, // 16: knot.v1.DeliveryService.Acknowledge:output_type -> knot.v1.AcknowledgeResponse
	12, // [12:17] is the sub-list for method output_type
	7,  // [7:12] is the sub-list for method input_type
	7,  // [7:7] is the sub-list for extension type_name
	7,  // [7:7] is the sub-list for extension extendee
	0,  // [0:7] is the sub-list for field type_name
}

func init() { file_knot_v1_messaging_proto_init() }
func file_knot_v1_messaging_proto_init() {
	if File_knot_v1_messaging_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_knot_v1_messaging_proto_rawDesc), len(file_knot_v1_messaging_proto_rawDesc)),
			NumEnums:      1,
			NumMessages:   15,
			NumExtensions: 0,
			NumServices:   2,
		},
		GoTypes:           file_knot_v1_messaging_proto_goTypes,
		DependencyIndexes: file_knot_v1_messaging_proto_depIdxs,
		EnumInfos:         file_knot_v1_messaging_proto_enumTypes,
		MessageInfos:      file_knot_v1_messaging_proto_msgTypes,
	}.Build()
	File_knot_v1_messaging_proto = out.File
	file_knot_v1_messaging_proto_goTypes = nil
	file_knot_v1_messaging_proto_depIdxs = nil
}
