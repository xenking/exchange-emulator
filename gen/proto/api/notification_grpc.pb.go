// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package api

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// NotificationSubscriberClient is the client API for NotificationSubscriber service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type NotificationSubscriberClient interface {
	Subscribe(ctx context.Context, in *NotificationRequest, opts ...grpc.CallOption) (NotificationSubscriber_SubscribeClient, error)
}

type notificationSubscriberClient struct {
	cc grpc.ClientConnInterface
}

func NewNotificationSubscriberClient(cc grpc.ClientConnInterface) NotificationSubscriberClient {
	return &notificationSubscriberClient{cc}
}

func (c *notificationSubscriberClient) Subscribe(ctx context.Context, in *NotificationRequest, opts ...grpc.CallOption) (NotificationSubscriber_SubscribeClient, error) {
	stream, err := c.cc.NewStream(ctx, &NotificationSubscriber_ServiceDesc.Streams[0], "/server.notification.NotificationSubscriber/Subscribe", opts...)
	if err != nil {
		return nil, err
	}
	x := &notificationSubscriberSubscribeClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type NotificationSubscriber_SubscribeClient interface {
	Recv() (*NotificationResponse, error)
	grpc.ClientStream
}

type notificationSubscriberSubscribeClient struct {
	grpc.ClientStream
}

func (x *notificationSubscriberSubscribeClient) Recv() (*NotificationResponse, error) {
	m := new(NotificationResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NotificationSubscriberServer is the server API for NotificationSubscriber service.
// All implementations must embed UnimplementedNotificationSubscriberServer
// for forward compatibility
type NotificationSubscriberServer interface {
	Subscribe(*NotificationRequest, NotificationSubscriber_SubscribeServer) error
	mustEmbedUnimplementedNotificationSubscriberServer()
}

// UnimplementedNotificationSubscriberServer must be embedded to have forward compatible implementations.
type UnimplementedNotificationSubscriberServer struct {
}

func (UnimplementedNotificationSubscriberServer) Subscribe(*NotificationRequest, NotificationSubscriber_SubscribeServer) error {
	return status.Errorf(codes.Unimplemented, "method Subscribe not implemented")
}
func (UnimplementedNotificationSubscriberServer) mustEmbedUnimplementedNotificationSubscriberServer() {
}

// UnsafeNotificationSubscriberServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to NotificationSubscriberServer will
// result in compilation errors.
type UnsafeNotificationSubscriberServer interface {
	mustEmbedUnimplementedNotificationSubscriberServer()
}

func RegisterNotificationSubscriberServer(s grpc.ServiceRegistrar, srv NotificationSubscriberServer) {
	s.RegisterService(&NotificationSubscriber_ServiceDesc, srv)
}

func _NotificationSubscriber_Subscribe_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(NotificationRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(NotificationSubscriberServer).Subscribe(m, &notificationSubscriberSubscribeServer{stream})
}

type NotificationSubscriber_SubscribeServer interface {
	Send(*NotificationResponse) error
	grpc.ServerStream
}

type notificationSubscriberSubscribeServer struct {
	grpc.ServerStream
}

func (x *notificationSubscriberSubscribeServer) Send(m *NotificationResponse) error {
	return x.ServerStream.SendMsg(m)
}

// NotificationSubscriber_ServiceDesc is the grpc.ServiceDesc for NotificationSubscriber service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var NotificationSubscriber_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "server.notification.NotificationSubscriber",
	HandlerType: (*NotificationSubscriberServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Subscribe",
			Handler:       _NotificationSubscriber_Subscribe_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "notification.proto",
}