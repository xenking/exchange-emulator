package resolvers

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/application"
)

type ExchangeServer struct {
	api.UnimplementedExchangeServer
	*application.Core
}

func NewExchangeServer(core *application.Core) api.ExchangeServer {
	return &ExchangeServer{
		Core: core,
	}
}

var ErrExchangeNotFound = status.Error(codes.NotFound, "user exchange not found")

func (e *ExchangeServer) StartExchange(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	exchange := e.Core.UserExchange(user)
	if exchange == nil {
		return nil, ErrExchangeNotFound
	}
	exchange.Start()

	return nil, nil
}

func (e *ExchangeServer) StopExchange(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	exchange := e.Core.UserExchange(user)
	if exchange == nil {
		return nil, ErrExchangeNotFound
	}
	exchange.Stop()

	return nil, nil
}

func (e *ExchangeServer) SetOffsetExchange(ctx context.Context, req *api.OffsetExchangeRequest) (*emptypb.Empty, error) {
	user, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	exchange := e.Core.UserExchange(user)
	if exchange == nil {
		return nil, ErrExchangeNotFound
	}
	if offset := req.GetOffset(); offset > 0 {
		exchange.SetOffset(offset)
	}

	return nil, nil
}
