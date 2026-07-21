package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/config"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/sink"
)

func main() {
	level := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			level = slog.LevelInfo
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	done := make(chan error, len(specs))
	var wg sync.WaitGroup
	runners := make([]*sink.Runner, 0, len(specs))
	for _, spec := range specs {
		runner, err := sink.NewRunner(spec, files, cfg.Kafka, rdb, log)
		if err != nil {
			log.Error("criando runner", "sink", spec.Name, "err", err)
			os.Exit(1)
		}
		runners = append(runners, runner)
		wg.Add(1)
		go func() {
			defer wg.Done()
			done <- runner.Run(ctx)
		}()
	}

	// encerra no primeiro erro fatal ou no sinal
	fatal := false
	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			log.Error("sink encerrou com erro", "err", err)
			fatal = true
		}
	case <-ctx.Done():
	}

	// shutdown ordenado: cancela, ESPERA os runners drenarem, e só então
	// fecha kafka/redis — fechar client debaixo de goroutine viva é o que
	// gerava o spam de "redis: client is closed" no desligamento
	log.Info("desligando...")
	cancel()
	wg.Wait()
	for _, r := range runners {
		r.Close()
	}
	if err := rdb.Close(); err != nil {
		log.Warn("fechando valkey", "err", err)
	}
	if fatal {
		os.Exit(1)
	}
}
