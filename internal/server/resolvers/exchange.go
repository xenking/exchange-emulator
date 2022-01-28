package resolvers

import (
	"context"

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

func (e *ExchangeServer) StartExchange(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	e.Core.Exchange().Start()

	return &emptypb.Empty{}, nil
}

func (e *ExchangeServer) StopExchange(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	e.Core.Exchange().Stop()

	return &emptypb.Empty{}, nil
}

func (e *ExchangeServer) SetOffsetExchange(ctx context.Context, req *api.OffsetExchangeRequest) (*emptypb.Empty, error) {
	exchange := e.Core.Exchange()
	if offset := req.GetOffset(); offset > 0 {
		exchange.SetOffset(offset)
	}

	return &emptypb.Empty{}, nil
}
