package config

import (
	"os"
	"strings"
)

type SinkConfig struct {
	DescriptorsPath string       // descriptors.binpb (mesmo artefato do futuro protoreg)
	SinksPath       string       // sink.yaml
	Kafka           KafkaConfig  // infos de conexão com Kafka
	Valkey          ValkeyConfig // info de conexão com o Valkey
}

type KafkaConfig struct {
	Brokers  []string
	Username string // vazio = sem SASL
	Password string
	CAPath   string // vazio = sem TLS
}

func SinkConfigFromEnv() SinkConfig {
	kafkaConfig := KafkaConfig{
		Brokers:  strings.Split(getenv("KAFKA_BROKERS", "redpanda.data.svc.cluster.local:9093"), ","),
		Username: getenv("KAFKA_USERNAME", "data_serving"),
		Password: os.Getenv("KAFKA_PASSWORD"),
		CAPath:   os.Getenv("KAFKA_CA_PATH"),
	}

	return SinkConfig{
		DescriptorsPath: getenv("DESCRIPTORS_PATH", "build/descriptors.binpb"),
		SinksPath:       getenv("SINKS_PATH", "config/sinks.yaml"),
		Kafka:           kafkaConfig,
		Valkey:          valkeyConfigFromEnv(),
	}
}
