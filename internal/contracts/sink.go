package contracts

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SinkSpec declara um consumidor tópico -> Valkey. Como plans.yaml, é escrito
// à mão por enquanto e gerado do DatasetContract no desenho final.
type SinkSpec struct {
	Name            string     `yaml:"name"`
	Topic           string     `yaml:"topic"`
	Group           string     `yaml:"group"`
	Message         string     `yaml:"message"`           // FQN do proto de resposta (ex.: lab.serving.v1.Account)
	KeyTemplate     string     `yaml:"key_template"`      // mesma sintaxe do kv_get
	DeleteFlagField string     `yaml:"delete_flag_field"` // campo JSON booleano que vira DEL
	Fields          []SinkFieldMap `yaml:"fields,omitempty"`  // só overrides; default: json == proto
}

// SinkFieldMap mapeia um campo do evento JSON para um campo do proto.
type SinkFieldMap struct {
	Proto     string `yaml:"proto"`
	JSON      string `yaml:"json"`
	Transform string `yaml:"transform,omitempty"` // "" | iso_millis
}

type sinkFile struct {
	Sinks []SinkSpec `yaml:"sinks"`
}

func LoadSinks(path string) ([]SinkSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lendo sinks em %s: %w", path, err)
	}
	var sf sinkFile
	if err := yaml.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("parseando sinks: %w", err)
	}
	for i, s := range sf.Sinks {
		if s.Name == "" || s.Topic == "" || s.Group == "" || s.Message == "" || s.KeyTemplate == "" {
			return nil, fmt.Errorf("sink %d: name, topic, group, message e key_template são obrigatórios", i)
		}
		for _, f := range s.Fields {
			if f.Transform != "" && f.Transform != "iso_millis" {
				return nil, fmt.Errorf("sink %s: transform desconhecida %q", s.Name, f.Transform)
			}
		}
	}
	return sf.Sinks, nil
}
