package config

import "os"

type ValkeyConfig struct {
	Addr     string // host:port
	Password string // vazio = sem AUTH
}


func valkeyConfigFromEnv() ValkeyConfig {
	return ValkeyConfig{
		Addr:     getenv("VALKEY_ADDR", "valkey.data.svc.cluster.local:6379"),
		Password: os.Getenv("VALKEY_PASSWORD"),
	}
}
