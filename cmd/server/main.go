package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/config"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/exec"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/observability"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/server"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.FromEnv()
	ctx := context.Background()

	shutdownOTel, err := observability.Setup(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		log.Error("otel setup falhou", "err", err)
		os.Exit(1)
	}

	reg, err := contracts.Load(cfg.DescriptorsPath, cfg.PlansPath)
	if err != nil {
		log.Error("carregando contratos", "err", err)
		os.Exit(1)
	}
	log.Info("registry carregado",
		"descriptors", cfg.DescriptorsPath,
		"plans", cfg.PlansPath,
		"services", len(reg.Services()),
	)

	pinot := exec.NewPinotClient(cfg.PinotBrokerURL, cfg.RequestTimeout)
	kv := exec.NewKVClient(cfg.ValkeyAddr)
	defer kv.Close()

	dispatcher := server.NewDispatcher(reg, pinot, kv, log, cfg.RequestTimeout)

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.UnknownServiceHandler(dispatcher.Handler()),
	)

	// Services REGISTRADOS têm precedência sobre o UnknownServiceHandler —
	// health e reflection convivem com o dispatch dinâmico sem conflito.
	healthSrv := health.NewServer()
	healthgrpc.RegisterHealthServer(grpcServer, healthSrv)
	server.RegisterReflection(grpcServer, reg)
	healthSrv.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Error("listen falhou", "addr", cfg.ListenAddr, "err", err)
		os.Exit(1)
	}

	go func() {
		log.Info("data-serving-api no ar", "addr", cfg.ListenAddr, "pinot", cfg.PinotBrokerURL)
		if err := grpcServer.Serve(lis); err != nil {
			log.Error("serve encerrou com erro", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Info("desligando...")
	healthSrv.SetServingStatus("", healthgrpc.HealthCheckResponse_NOT_SERVING)
	grpcServer.GracefulStop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdownOTel(shutdownCtx); err != nil {
		log.Warn("shutdown do otel", "err", err)
	}
}
