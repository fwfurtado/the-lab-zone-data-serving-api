package config

import (
	"os"
	"time"
)

type Config struct {
	ListenAddr      string        // endereço gRPC
	DescriptorsPath string        // descriptors.binpb (mesmo artefato do futuro protoreg)
	PlansPath       string        // plans.yaml
	PinotBrokerURL  string        // REST do broker
	ValkeyAddr      string        // host:port
	RequestTimeout  time.Duration // timeout por request (teto; SLO por plano vem depois)
	OTLPEndpoint    string        // vazio = traces desligados
	ServiceName     string
}

func FromEnv() Config {
	return Config{
		ListenAddr:      getenv("LISTEN_ADDR", ":9090"),
		DescriptorsPath: getenv("DESCRIPTORS_PATH", "build/descriptors.binpb"),
		PlansPath:       getenv("PLANS_PATH", "config/plans.yaml"),
		PinotBrokerURL:  getenv("PINOT_BROKER_URL", "http://pinot-broker.data.svc.cluster.local:8099"),
		ValkeyAddr:      getenv("VALKEY_ADDR", "valkey.data.svc.cluster.local:6379"),
		RequestTimeout:  getduration("REQUEST_TIMEOUT", 2*time.Second),
		OTLPEndpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:     getenv("OTEL_SERVICE_NAME", "data-serving-api"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getduration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
