package main

import (
	"context"
	"os"
	"time"

	"github.com/cloudflare/tableflip"
	"github.com/phuslu/log"

	"github.com/xenking/exchange-emulator/api/server"
	"github.com/xenking/exchange-emulator/api/server/api"
	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/pkg/logger"
)

func serveCmd(ctx context.Context, flags cmdFlags) error {
	cfg, err := config.NewConfig(flags.Config)
	if err != nil {
		return err
	}
	log.Debug().Msgf("%+v", cfg)

	l := logger.New(cfg.Log)
	logger.SetGlobal(l)
	log.DefaultLogger = *logger.NewModule("global")


	return serve(ctx, cfg)
}

func serve(ctx context.Context, cfg *config.Config, bot *api.Bot) error {
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

	rs, err := serveREST(cfg, bot, upg)
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

	if rs != nil {
		listerErr = rs.Shutdown()
	}

	return listerErr
}

func serveREST(cfg *config.Config, bot *api.Bot, upg *tableflip.Upgrader) (*server.Server, error) {
	rs, err := server.New(bot)
	if err != nil {
		return nil, err
	}

	// Serve must be called before Ready
	restListener, listenErr := upg.Listen("tcp", cfg.REST.Addr)
	if listenErr != nil {
		log.Error().Err(listenErr).Msg("can't listen")

		return nil, listenErr
	}

	// run gateway server
	go func() {
		if serveErr := rs.Serve(restListener); serveErr != nil {
			log.Error().Err(serveErr).Msg("rest server")
		}
	}()
	log.Info().Msg("serving rest server")

	return rs, nil
}
