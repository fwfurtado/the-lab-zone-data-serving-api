package config

import (
	"os"
	"time"
)

type ServerConfig struct {
	ListenAddr      string        // endereço gRPC
	MetricsAddr     string        // HTTP /metrics (prometheus)
	DescriptorsPath string        // descriptors.binpb (mesmo artefato do futuro protoreg)
	PlansPath       string        // plans.yaml
	RequestTimeout  time.Duration // timeout por request (teto; SLO por plano vem depois)
	OTLPEndpoint    string        // vazio = traces desligados
	ServiceName     string
	Valkey          ValkeyConfig // infos de conexão com o Valkey
	Pinot           PinotConfig  // infos de conexão com o Pinot
}

type PinotConfig struct {
	BrokerURL string // REST do broker
}

func ServerConfigFromEnv() ServerConfig {
	pinotConfig := PinotConfig{
		BrokerURL: getenv("PINOT_BROKER_URL", "http://pinot-broker.data.svc.cluster.local:8099"),
	}

	return ServerConfig{
		ListenAddr:      getenv("LISTEN_ADDR", ":9090"),
		MetricsAddr:     getenv("METRICS_ADDR", ":9091"),
		DescriptorsPath: getenv("DESCRIPTORS_PATH", "build/descriptors.binpb"),
		PlansPath:       getenv("PLANS_PATH", "config/plans.yaml"),
		RequestTimeout:  getduration("REQUEST_TIMEOUT", 2*time.Second),
		OTLPEndpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:     getenv("OTEL_SERVICE_NAME", "data-serving-api"),
		Valkey:          valkeyConfigFromEnv(),
		Pinot:           pinotConfig,
	}
}
