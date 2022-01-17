package resolvers

import (
	"context"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/application"
)

type InfoServer struct {
	api.UnimplementedInfoServer
	*application.Core
	dataFile string
}

func NewInfoServer(core *application.Core, dataFile string) api.InfoServer {
	return &InfoServer{
		Core:     core,
		dataFile: dataFile,
	}
}

func (i *InfoServer) GetExchangeInfo(ctx context.Context, _ *emptypb.Empty) (*structpb.Struct, error) {
	info, err := i.Core.ExchangeInfo()
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return structpb.NewStruct(info)
}

func (i *InfoServer) GetPrice(ctx context.Context, req *api.GetPriceRequest) (*api.GetPriceResponse, error) {
	price, err := i.Core.GetPrice(req.GetSymbol())
	if err != nil {
		return nil, status.Convert(err).Err()
	}

	return &api.GetPriceResponse{
		Price: price.String(),
	}, nil
}
