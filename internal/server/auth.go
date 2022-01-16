package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AuthInterceptor struct{}

func NewAuthenticator() *AuthInterceptor {
	return &AuthInterceptor{}
}

// NewUnaryInterceptor authorization unary interceptor function to handle authorize per RPC call
func (i *AuthInterceptor) NewUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if err := i.authorize(ctx); err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

func (i *AuthInterceptor) NewStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := i.authorize(ss.Context()); err != nil {
		return err
	}

	return handler(srv, ss)
}

func (i *AuthInterceptor) authorize(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "Failed to retrieve metadata")
	}

	user, ok := md["user"]
	if !ok || len(user) == 0 || user[0] == "" {
		return status.Errorf(codes.Unauthenticated, "UserID are required")
	}

	return nil
}
