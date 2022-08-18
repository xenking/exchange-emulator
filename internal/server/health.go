package server

import (
	"context"

	"github.com/xenking/exchange-emulator/gen/proto/api"
)

type HealthServer struct {
	api.UnimplementedHealthServer
}

func NewHealthServer() api.HealthServer {
	return &HealthServer{}
}

func (s *HealthServer) Check(ctx context.Context, req *api.HealthCheckRequest) (*api.HealthCheckResponse, error) {
	return &api.HealthCheckResponse{
		Status: api.HealthCheckResponse_SERVING,
	}, nil
}

func (s *HealthServer) Watch(req *api.HealthCheckRequest, server api.Health_WatchServer) error {
	return server.Send(&api.HealthCheckResponse{
		Status: api.HealthCheckResponse_SERVING,
	})
}
