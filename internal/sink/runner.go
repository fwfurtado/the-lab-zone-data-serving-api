package sink

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/config"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/exec"
)

type Runner struct {
	spec   contracts.SinkSpec
	mapper *Mapper
	client *kgo.Client
	rdb    *redis.Client
	log    *slog.Logger
}

func NewRunner(spec contracts.SinkSpec, files *protoregistry.Files, kafka config.KafkaConfig, rdb *redis.Client, log *slog.Logger) (*Runner, error) {
	desc, err := files.FindDescriptorByName(protoreflect.FullName(spec.Message))
	if err != nil {
		return nil, fmt.Errorf("message %s do sink %s não existe nos descriptors: %w", spec.Message, spec.Name, err)
	}
	msgDesc, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s não é um message", spec.Message)
	}
	mapper, err := NewMapper(msgDesc, spec)
	if err != nil {
		return nil, err
	}

	opts := []kgo.Opt{
		kgo.WithLogger(kgoSlog{log: log}),
		kgo.SeedBrokers(kafka.Brokers...),
		kgo.ConsumerGroup(spec.Group),
		kgo.ConsumeTopics(spec.Topic),
		// earliest: replay do tópico == reidratação do Valkey (keyed por PK,
		// ordem por partição => o último estado vence; DEL de tombstone idem)
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// commit só do que foi processado com sucesso (at-least-once)
		kgo.AutoCommitMarks(),
	}
	if kafka.Username != "" {
		opts = append(opts, kgo.SASL(scram.Auth{
			User: kafka.Username,
			Pass: kafka.Password,
		}.AsSha512Mechanism()))
	}
	if kafka.CAPath != "" {
		pem, err := os.ReadFile(kafka.CAPath)
		if err != nil {
			return nil, fmt.Errorf("lendo CA em %s: %w", kafka.CAPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA em %s inválida", kafka.CAPath)
		}
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{RootCAs: pool}))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("criando consumer kafka: %w", err)
	}

	return &Runner{spec: spec, mapper: mapper, client: client, rdb: rdb, log: log}, nil
}

func (r *Runner) Close() { r.client.Close() }

// Run processa até o contexto ser cancelado.
func (r *Runner) Run(ctx context.Context) error {
	// fail-fast: broker inalcançável, TLS ou SASL errados devem derrubar o
	// processo com erro claro, não travar o PollFetches em retry eterno
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := r.client.Ping(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("ping ao kafka falhou (broker/TLS/SASL — confira KAFKA_BROKERS, KAFKA_USERNAME, KAFKA_CA_PATH): %w", err)
	}
	r.log.Info("sink iniciado", "sink", r.spec.Name, "topic", r.spec.Topic, "group", r.spec.Group)
	for {
		fetches := r.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			// erros de fetch são por partição; loga e segue (retry do client)
			for _, e := range errs {
				r.log.Error("fetch", "topic", e.Topic, "partition", e.Partition, "err", e.Err)
			}
		}

		fetches.EachRecord(func(rec *kgo.Record) {
			// shutdown: não drena o batch com contexto morto — cada SET/DEL
			// falharia com "context canceled" e viraria spam de ERROR falso;
			// o não-marcado replay-a idempotente no próximo start
			if ctx.Err() != nil {
				return
			}
			if err := r.process(ctx, rec); err != nil {
				// v1: loga e NÃO marca — o registro volta no próximo ciclo do
				// grupo. Poison pill trava a partição: dead-letter é dívida.
				r.log.Error("processando registro",
					"sink", r.spec.Name, "partition", rec.Partition, "offset", rec.Offset, "err", err)
				return
			}
			r.client.MarkCommitRecords(rec)
		})
	}
}

func (r *Runner) process(ctx context.Context, rec *kgo.Record) error {
	r.log.Debug("processando", "record", string(rec.Value))
	msg, deleted, err := r.mapper.Map(rec.Value)
	if err != nil {
		return err
	}
	key, err := exec.RenderKey(r.spec.KeyTemplate, msg)
	if err != nil {
		return fmt.Errorf("montando chave: %w", err)
	}

	if deleted {
		if err := r.rdb.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("DEL %s: %w", key, err)
		}
		return nil
	}

	raw, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("serializando %s: %w", r.spec.Message, err)
	}
	if err := r.rdb.Set(ctx, key, raw, 0).Err(); err != nil {
		return fmt.Errorf("SET %s: %w", key, err)
	}
	return nil
}
