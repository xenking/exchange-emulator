package main

import (
	"context"
	"os"
	"time"

	"github.com/cloudflare/tableflip"
	"github.com/phuslu/log"
	"google.golang.org/grpc/grpclog"

	"github.com/xenking/exchange-emulator/config"
	"github.com/xenking/exchange-emulator/internal/app"
	"github.com/xenking/exchange-emulator/internal/server"
	"github.com/xenking/exchange-emulator/internal/server/notification"
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

	wsOrders := ws.New(ctx)
	wsPrices := ws.New(ctx)

	application, err := app.New(wsOrders.Users(), wsPrices.Users(), cfg)
	if err != nil {
		return err
	}

	srv, err := server.New(application, cfg.GRPC, cfg.Exchange.InfoFile)
	if err != nil {
		return err
	}

	srvNotifications, err := notification.New(application)
	if err != nil {
		return err
	}

	// Serve must be called before Ready
	wssOrdersListener, err := upg.Listen("tcp", cfg.WS.OrdersAddr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen ws orders")

		return err
	}

	// Serve must be called before Ready
	wssPricesListener, err := upg.Listen("tcp", cfg.WS.PricesAddr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen ws prices")

		return err
	}

	grpcListener, err := upg.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen grpc")

		return err
	}

	grpcNotificationListener, err := upg.Listen("tcp", cfg.GRPC.NotificationsAddr)
	if err != nil {
		log.Error().Err(err).Msg("can't listen grpc")

		return err
	}

	// run wss orders server
	go func() {
		defer wsOrders.Stop()
		log.Info().Msg("serving wss orders server")
		if serveErr := wsOrders.Serve(wssOrdersListener); serveErr != nil {
			log.Error().Err(serveErr).Msg("wss server")
		}
	}()

	// run wss prices server
	go func() {
		defer wsPrices.Stop()
		log.Info().Msg("serving wss prices server")
		if serveErr := wsPrices.Serve(wssPricesListener); serveErr != nil {
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

	// run grpc metrics server
	go func() {
		log.Info().Msg("serving grpc notifications server")
		if serveErr := srvNotifications.Serve(grpcNotificationListener); serveErr != nil {
			log.Error().Err(serveErr).Msg("grpc notifications server")
		}
	}()

	go application.Start(ctx)

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
