package resolvers

import (
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/application"
)

type UserServer struct {
	api.UnimplementedUserServer
	*application.Core
}

func NewUserServer(core *application.Core) api.UserServer {
	return &UserServer{
		Core: core,
	}
}

func (u *UserServer) GetBalance(ctx context.Context, _ *emptypb.Empty) (*api.Balances, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	balances, err := u.Core.GetBalance(user)
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return balances, nil
}

func (u *UserServer) SetBalance(ctx context.Context, balances *api.Balances) (*emptypb.Empty, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	u.Core.SetBalance(user, balances)

	return &emptypb.Empty{}, nil
}

func (u *UserServer) CreateOrder(ctx context.Context, order *api.Order) (*api.Order, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	order, err = u.Core.CreateOrder(user, order)
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return order, nil
}

func (u *UserServer) GetOrder(ctx context.Context, req *api.OrderRequest) (*api.Order, error) {
	order, err := u.Core.GetOrder(req.GetId())
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return order, nil
}

func (u *UserServer) DeleteOrder(ctx context.Context, req *api.OrderRequest) (*api.Order, error) {
	order, err := u.Core.CancelOrder(req.GetId())
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return order, nil
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
