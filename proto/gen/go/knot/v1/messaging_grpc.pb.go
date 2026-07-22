package knotv1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const _ = grpc.SupportPackageIsVersion9

const (
	RouterService_AuthorizeConnection_FullMethodName = "/knot.v1.RouterService/AuthorizeConnection"
	RouterService_RouteMessage_FullMethodName        = "/knot.v1.RouterService/RouteMessage"
)

type RouterServiceClient interface {
	AuthorizeConnection(ctx context.Context, in *AuthorizeConnectionRequest, opts ...grpc.CallOption) (*AuthorizeConnectionResponse, error)
	RouteMessage(ctx context.Context, in *RouteMessageRequest, opts ...grpc.CallOption) (*RouteMessageResponse, error)
}

type routerServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewRouterServiceClient(cc grpc.ClientConnInterface) RouterServiceClient {
	return &routerServiceClient{cc}
}

func (c *routerServiceClient) AuthorizeConnection(ctx context.Context, in *AuthorizeConnectionRequest, opts ...grpc.CallOption) (*AuthorizeConnectionResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(AuthorizeConnectionResponse)
	err := c.cc.Invoke(ctx, RouterService_AuthorizeConnection_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *routerServiceClient) RouteMessage(ctx context.Context, in *RouteMessageRequest, opts ...grpc.CallOption) (*RouteMessageResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(RouteMessageResponse)
	err := c.cc.Invoke(ctx, RouterService_RouteMessage_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type RouterServiceServer interface {
	AuthorizeConnection(context.Context, *AuthorizeConnectionRequest) (*AuthorizeConnectionResponse, error)
	RouteMessage(context.Context, *RouteMessageRequest) (*RouteMessageResponse, error)
	mustEmbedUnimplementedRouterServiceServer()
}

type UnimplementedRouterServiceServer struct{}

func (UnimplementedRouterServiceServer) AuthorizeConnection(context.Context, *AuthorizeConnectionRequest) (*AuthorizeConnectionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method AuthorizeConnection not implemented")
}
func (UnimplementedRouterServiceServer) RouteMessage(context.Context, *RouteMessageRequest) (*RouteMessageResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method RouteMessage not implemented")
}
func (UnimplementedRouterServiceServer) mustEmbedUnimplementedRouterServiceServer() {}
func (UnimplementedRouterServiceServer) testEmbeddedByValue()                       {}

type UnsafeRouterServiceServer interface {
	mustEmbedUnimplementedRouterServiceServer()
}

func RegisterRouterServiceServer(s grpc.ServiceRegistrar, srv RouterServiceServer) {
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&RouterService_ServiceDesc, srv)
}

func _RouterService_AuthorizeConnection_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AuthorizeConnectionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RouterServiceServer).AuthorizeConnection(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RouterService_AuthorizeConnection_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RouterServiceServer).AuthorizeConnection(ctx, req.(*AuthorizeConnectionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RouterService_RouteMessage_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RouteMessageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RouterServiceServer).RouteMessage(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: RouterService_RouteMessage_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RouterServiceServer).RouteMessage(ctx, req.(*RouteMessageRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var RouterService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "knot.v1.RouterService",
	HandlerType: (*RouterServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "AuthorizeConnection",
			Handler:    _RouterService_AuthorizeConnection_Handler,
		},
		{
			MethodName: "RouteMessage",
			Handler:    _RouterService_RouteMessage_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "knot/v1/messaging.proto",
}

const (
	DeliveryService_Enqueue_FullMethodName     = "/knot.v1.DeliveryService/Enqueue"
	DeliveryService_Sync_FullMethodName        = "/knot.v1.DeliveryService/Sync"
	DeliveryService_Acknowledge_FullMethodName = "/knot.v1.DeliveryService/Acknowledge"
)

type DeliveryServiceClient interface {
	Enqueue(ctx context.Context, in *EnqueueRequest, opts ...grpc.CallOption) (*EnqueueResponse, error)
	Sync(ctx context.Context, in *SyncRequest, opts ...grpc.CallOption) (*SyncResponse, error)
	Acknowledge(ctx context.Context, in *AcknowledgeRequest, opts ...grpc.CallOption) (*AcknowledgeResponse, error)
}

type deliveryServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDeliveryServiceClient(cc grpc.ClientConnInterface) DeliveryServiceClient {
	return &deliveryServiceClient{cc}
}

func (c *deliveryServiceClient) Enqueue(ctx context.Context, in *EnqueueRequest, opts ...grpc.CallOption) (*EnqueueResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(EnqueueResponse)
	err := c.cc.Invoke(ctx, DeliveryService_Enqueue_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deliveryServiceClient) Sync(ctx context.Context, in *SyncRequest, opts ...grpc.CallOption) (*SyncResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(SyncResponse)
	err := c.cc.Invoke(ctx, DeliveryService_Sync_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deliveryServiceClient) Acknowledge(ctx context.Context, in *AcknowledgeRequest, opts ...grpc.CallOption) (*AcknowledgeResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(AcknowledgeResponse)
	err := c.cc.Invoke(ctx, DeliveryService_Acknowledge_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type DeliveryServiceServer interface {
	Enqueue(context.Context, *EnqueueRequest) (*EnqueueResponse, error)
	Sync(context.Context, *SyncRequest) (*SyncResponse, error)
	Acknowledge(context.Context, *AcknowledgeRequest) (*AcknowledgeResponse, error)
	mustEmbedUnimplementedDeliveryServiceServer()
}

type UnimplementedDeliveryServiceServer struct{}

func (UnimplementedDeliveryServiceServer) Enqueue(context.Context, *EnqueueRequest) (*EnqueueResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Enqueue not implemented")
}
func (UnimplementedDeliveryServiceServer) Sync(context.Context, *SyncRequest) (*SyncResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Sync not implemented")
}
func (UnimplementedDeliveryServiceServer) Acknowledge(context.Context, *AcknowledgeRequest) (*AcknowledgeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Acknowledge not implemented")
}
func (UnimplementedDeliveryServiceServer) mustEmbedUnimplementedDeliveryServiceServer() {}
func (UnimplementedDeliveryServiceServer) testEmbeddedByValue()                         {}

type UnsafeDeliveryServiceServer interface {
	mustEmbedUnimplementedDeliveryServiceServer()
}

func RegisterDeliveryServiceServer(s grpc.ServiceRegistrar, srv DeliveryServiceServer) {
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&DeliveryService_ServiceDesc, srv)
}

func _DeliveryService_Enqueue_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EnqueueRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeliveryServiceServer).Enqueue(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DeliveryService_Enqueue_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeliveryServiceServer).Enqueue(ctx, req.(*EnqueueRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeliveryService_Sync_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SyncRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeliveryServiceServer).Sync(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DeliveryService_Sync_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeliveryServiceServer).Sync(ctx, req.(*SyncRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeliveryService_Acknowledge_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AcknowledgeRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeliveryServiceServer).Acknowledge(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DeliveryService_Acknowledge_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeliveryServiceServer).Acknowledge(ctx, req.(*AcknowledgeRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var DeliveryService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "knot.v1.DeliveryService",
	HandlerType: (*DeliveryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Enqueue",
			Handler:    _DeliveryService_Enqueue_Handler,
		},
		{
			MethodName: "Sync",
			Handler:    _DeliveryService_Sync_Handler,
		},
		{
			MethodName: "Acknowledge",
			Handler:    _DeliveryService_Acknowledge_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "knot/v1/messaging.proto",
}
