package server

import (
	"context"
	"io"
	"os"

	"github.com/phuslu/log"
	"github.com/segmentio/encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/app"
)

func New(a *app.App, cfg config.GRPCConfig, dataFile string) (*grpc.Server, error) {
	var opts []grpc.ServerOption
	if !cfg.DisableAuth {
		auth := NewAuthenticator()
		opts = append(opts, grpc.StreamInterceptor(auth.NewStreamInterceptor))
	}
	s := grpc.NewServer(opts...)

	exchangeInfo, err := loadExchangeInfo(dataFile)
	if err != nil {
		return nil, err
	}

	api.RegisterHealthServer(s, NewHealthServer())
	api.RegisterMultiplexServer(s, NewServer(a, exchangeInfo))

	return s, nil
}

func NewServer(a *app.App, exchangeInfo *structpb.Struct) api.MultiplexServer {
	return &Server{
		app:          a,
		exchangeInfo: exchangeInfo,
	}
}

type Server struct {
	api.UnimplementedMultiplexServer
	app          *app.App
	exchangeInfo *structpb.Struct
}

func (s *Server) StartExchange(stream api.Multiplex_StartExchangeServer) error {
	userID, uErr := getUserID(stream.Context())
	if uErr != nil {
		return uErr
	}

	for {
		ctx := stream.Context()
		if err := contextError(ctx); err != nil {
			return err
		}

		r, err := stream.Recv()
		if err == io.EOF {
			log.Info().Msg("connection closed")
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unavailable, "can't receive request: %v", err)
		}

		resp := &api.Response{}
		var appErr error

		switch req := r.GetRequest().(type) {
		case *api.Request_CreateOrder:
			var order *api.Order
			order, appErr = s.app.CreateOrder(ctx, userID, req.CreateOrder)
			resp.Response = &api.Response_CreateOrder{CreateOrder: order}
		case *api.Request_CreateOrders:
			var orders []*api.Order
			orders, appErr = s.app.CreateOrders(ctx, userID, req.CreateOrders.GetOrders())
			resp.Response = &api.Response_CreateOrders{CreateOrders: &api.Orders{Orders: orders}}
		case *api.Request_GetOrder:
			var order *api.Order
			order, appErr = s.app.GetOrder(ctx, userID, req.GetOrder.GetId())
			resp.Response = &api.Response_GetOrder{GetOrder: order}
		case *api.Request_CancelOrder:
			appErr = s.app.CancelOrder(ctx, userID, req.CancelOrder.GetId())
			resp.Response = &api.Response_CancelOrder{CancelOrder: &emptypb.Empty{}}
		case *api.Request_CancelOrders:
			appErr = s.app.CancelOrders(ctx, userID, req.CancelOrders.GetIds())
			resp.Response = &api.Response_CancelOrder{CancelOrder: &emptypb.Empty{}}
		case *api.Request_GetBalances:
			var balances *api.Balances
			balances, appErr = s.app.GetBalances(ctx, userID)
			resp.Response = &api.Response_GetBalances{GetBalances: balances}
		case *api.Request_SetBalances:
			appErr = s.app.SetBalances(ctx, userID, req.SetBalances)
			resp.Response = &api.Response_SetBalances{SetBalances: &emptypb.Empty{}}
		case *api.Request_GetPrice:
			var price *api.Price
			price, appErr = s.app.GetPrice(ctx, userID, req.GetPrice.GetSymbol())
			resp.Response = &api.Response_GetPrice{GetPrice: price}
		case *api.Request_GetExchangeInfo:
			resp.Response = &api.Response_GetExchangeInfo{GetExchangeInfo: s.exchangeInfo}
		}

		if appErr != nil {
			resp.Response = &api.Response_Error{Error: &api.Error{Message: appErr.Error()}}
		}
		if err = stream.Send(resp); err != nil {
			return status.Errorf(codes.Internal, "can't send response: %v", err)
		}
	}
}

func contextError(ctx context.Context) error {
	switch ctx.Err() {
	case context.Canceled:
		return status.Error(codes.Canceled, "request is canceled")
	case context.DeadlineExceeded:
		return status.Error(codes.DeadlineExceeded, "deadline is exceeded")
	default:
		return nil
	}
}

func loadExchangeInfo(filename string) (*structpb.Struct, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	info := make(map[string]interface{})
	err = json.NewDecoder(f).Decode(&info)

	return structpb.NewStruct(info)
}
