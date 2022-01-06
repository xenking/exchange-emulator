package main

import (
	"context"
	"os"
	"time"

	"github.com/cloudflare/tableflip"
	"github.com/phuslu/log"
	"github.com/xenking/decimal"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/internal/application"
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

	core := application.NewCore(cfg.ExchangeFile, decimal.NewFromFloat(cfg.Commission))

	_, err := serveWS(cfg, core, upg)
	if err != nil {
		return err
	}

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

func serveWS(cfg *config.Config, core *application.Core, upg *tableflip.Upgrader) (*application.Server, error) {
	ws := application.NewServer(core)

	// Serve must be called before Ready
	listener, err := upg.Listen("tcp", cfg.WS.Addr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen")

		return nil, err
	}

	// run ws server
	go func() {
		if serveErr := ws.Serve(listener); serveErr != nil {
			log.Error().Err(serveErr).Msg("ws server")
		}
	}()
	log.Info().Msg("serving ws server")

	return ws, nil
}
