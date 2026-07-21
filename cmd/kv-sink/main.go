package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/config"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/sink"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.SinkConfigFromEnv()

	files, err := contracts.LoadDescriptors(cfg.DescriptorsPath)
	if err != nil {
		log.Error("carregando descriptors", "err", err)
		os.Exit(1)
	}
	specs, err := contracts.LoadSinks(cfg.SinksPath)
	if err != nil {
		log.Error("carregando sinks", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Valkey.Addr, Password: cfg.Valkey.Password})
	defer rdb.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	done := make(chan error, len(specs))
	for _, spec := range specs {
		runner, err := sink.NewRunner(spec, files, cfg.Kafka, rdb, log)
		if err != nil {
			log.Error("criando runner", "sink", spec.Name, "err", err)
			os.Exit(1)
		}
		defer runner.Close()
		go func() { done <- runner.Run(ctx) }()
	}

	// encerra no primeiro erro fatal ou no sinal
	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			log.Error("sink encerrou com erro", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}
	log.Info("desligando...")
}
