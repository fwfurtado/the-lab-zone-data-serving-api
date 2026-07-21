package contracts

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type PlanType string

const (
	PlanKVGet      PlanType = "kv_get"
	PlanPinotQuery PlanType = "pinot_query"
)

type PinotResult string

const (
	ResultSingleRow PinotResult = "single_row"
	ResultRows      PinotResult = "rows"
)

// Plan é a forma declarada em plans.yaml (futuramente gerada do DatasetContract).
type Plan struct {
	Method string   `yaml:"method"` // "/pkg.Service/Method"
	Type   PlanType `yaml:"type"`

	Pinot *PinotPlan `yaml:"pinot,omitempty"`
	KV    *KVPlan    `yaml:"kv,omitempty"`
}

type PinotPlan struct {
	SQL    string      `yaml:"sql"`
	Result PinotResult `yaml:"result"`
}

type KVPlan struct {
	KeyTemplate string `yaml:"key_template"`
}

type planFile struct {
	Plans []Plan `yaml:"plans"`
}

func LoadPlans(path string) ([]Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lendo plans em %s: %w", path, err)
	}
	var pf planFile
	if err := yaml.Unmarshal(raw, &pf); err != nil {
		return nil, fmt.Errorf("parseando plans: %w", err)
	}
	for i, p := range pf.Plans {
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("plano %d (%s): %w", i, p.Method, err)
		}
	}
	return pf.Plans, nil
}

func (p Plan) validate() error {
	if p.Method == "" {
		return fmt.Errorf("method vazio")
	}
	switch p.Type {
	case PlanPinotQuery:
		if p.Pinot == nil || p.Pinot.SQL == "" {
			return fmt.Errorf("pinot_query exige pinot.sql")
		}
		if p.Pinot.Result != ResultSingleRow && p.Pinot.Result != ResultRows {
			return fmt.Errorf("pinot.result deve ser single_row ou rows")
		}
	case PlanKVGet:
		if p.KV == nil || p.KV.KeyTemplate == "" {
			return fmt.Errorf("kv_get exige kv.key_template")
		}
	default:
		return fmt.Errorf("type desconhecido: %q", p.Type)
	}
	return nil
}
