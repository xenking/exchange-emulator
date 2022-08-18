package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AuthInterceptor struct{}

func NewAuthenticator() AuthInterceptor {
	return AuthInterceptor{}
}

func (i AuthInterceptor) NewStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := i.authorize(ss.Context()); err != nil {
		return err
	}

	return handler(srv, ss)
}

func (i AuthInterceptor) authorize(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "failed to retrieve metadata")
	}

	user, ok := md["user"]
	if !ok || len(user) == 0 || user[0] == "" {
		return status.Errorf(codes.Unauthenticated, "user id are required")
	}

	return nil
}

func getUserID(ctx context.Context) (string, error) {
	md, exist := metadata.FromIncomingContext(ctx)
	if !exist {
		return "", status.Errorf(codes.Unauthenticated, "Context metadata not found in request")
	}

	userID, ok := md["user"]
	if !ok || len(userID) == 0 || userID[0] == "" {
		return "", status.Errorf(codes.Unauthenticated, "UserID are required")
	}

	return userID[0], nil
}
