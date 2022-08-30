package metrics

import (
	"github.com/phuslu/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/app"
	"github.com/xenking/exchange-emulator/internal/parser"
)

func New(a *app.App) (*grpc.Server, error) {
	s := grpc.NewServer()

	api.RegisterMetricsSubscriberServer(s, NewServer(a))

	return s, nil
}

func NewServer(a *app.App) api.MetricsSubscriberServer {
	return &Server{
		app: a,
	}
}

type Server struct {
	api.UnimplementedMetricsSubscriberServer
	app *app.App
}

func (s *Server) Subscribe(req *api.MetricsRequest, stream api.MetricsSubscriber_SubscribeServer) error {
	client, err := s.app.GetClient(req.User)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	log.Info().Str("user", req.User).Msg("metrics subscribe")

	done := make(chan struct{})
	client.SetCancelHandler(func(state parser.ExchangeState) {
		bal := client.Balance.List()
		balances := make([]*api.Balance, len(bal))
		for i, asset := range bal {
			balances[i] = &api.Balance{
				Asset:  asset.Name,
				Free:   asset.Free.String(),
				Locked: asset.Locked.String(),
			}
		}
		resp := &api.MetricsResponse{
			User:     req.User,
			Price:    state.Close.String(),
			Balances: balances,
		}
		err = stream.Send(resp)
		close(done)
	})
	<-done

	return err
}
