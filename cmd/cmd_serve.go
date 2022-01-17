package main

import (
	"context"
	"os"
	"time"

	"github.com/cloudflare/tableflip"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"
	"google.golang.org/grpc/grpclog"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/internal/application"
	"github.com/xenking/exchange-emulator/internal/server"
	"github.com/xenking/exchange-emulator/internal/ws"
	"github.com/xenking/exchange-emulator/pkg/logger"
)

func serveCmd(ctx context.Context, flags cmdFlags) error {
	cfg, err := config.NewConfig(flags.Config)
	if err != nil {
		return err
	}
	log.Debug().Msgf("%+v", cfg)

	l := logger.New(&cfg.Log)
	logger.SetGlobal(l)
	log.DefaultLogger = *logger.NewModule("global")
	grpclog.SetLoggerV2(l.Grpc(log.NewContext(nil).Str("module", "grpc").Value()))

	return serve(ctx, cfg)
}

func serve(ctx context.Context, cfg *config.Config) error {
	upg, listerErr := tableflip.New(tableflip.Options{
		UpgradeTimeout: cfg.GracefulShutdownDelay,
	})
	if listerErr != nil {
		return listerErr
	}
	defer upg.Stop()

	// waiting for ctrl+c
	go func() {
		<-ctx.Done()
		upg.Stop()
	}()

	core := application.NewCore(cfg.ExchangeInfoFile, decimal.NewFromFloat(cfg.Commission))
	exchange := application.NewExchange()
	err := core.SetExchange(ctx, exchange, cfg.ExchangeDataFile)
	if err != nil {
		log.Error().Err(err).Msg("can't init exchange")

		return err
	}

	wss := ws.New(ctx)
	core.OnOrderUpdate(wss.OnOrderUpdate)
	srv := server.New(core, cfg.GRPC, cfg.ExchangeDataFile)

	// Serve must be called before Ready
	wssListener, err := upg.Listen("tcp", cfg.WS.Addr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen")

		return err
	}

	grpcListener, err := upg.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen")

		return err
	}

	// run wss server
	go func() {
		log.Info().Msg("serving wss server")
		if serveErr := wss.Serve(wssListener); serveErr != nil {
			log.Error().Err(serveErr).Msg("wss server")
		}
	}()

	// run grpc server
	go func() {
		log.Info().Msg("serving grpc server")
		if serveErr := srv.Serve(grpcListener); serveErr != nil {
			log.Error().Err(serveErr).Msg("grpc server")
		}
	}()

	log.Info().Msg("service ready")
	if upgErr := upg.Ready(); upgErr != nil {
		return upgErr
	}

	<-upg.Exit()
	log.Info().Msg("shutting down")

	time.AfterFunc(cfg.GracefulShutdownDelay, func() {
		log.Warn().Msg("graceful shutdown timed out")
		os.Exit(1)
	})

	return err
}
