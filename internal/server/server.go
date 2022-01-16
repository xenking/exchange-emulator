package server

import (
	"google.golang.org/grpc"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/gen/proto/api"
	"github.com/xenking/exchange-emulator/internal/application"
	"github.com/xenking/exchange-emulator/internal/server/resolvers"
)

func New(core *application.Core, cfg config.GRPCConfig, dataFile string) *grpc.Server {
	var opts []grpc.ServerOption
	if !cfg.DisableAuth {
		auth := NewAuthenticator()
		opts = append(opts, grpc.UnaryInterceptor(auth.NewUnaryInterceptor),
			grpc.StreamInterceptor(auth.NewStreamInterceptor),
		)
	}
	s := grpc.NewServer(opts...)

	api.RegisterHealthServer(s, resolvers.NewHealthServer())
	api.RegisterInfoServer(s, resolvers.NewInfoServer(core, dataFile))
	api.RegisterUserServer(s, resolvers.NewUserServer(core))
	api.RegisterExchangeServer(s, resolvers.NewExchangeServer(core))

	return s
}
