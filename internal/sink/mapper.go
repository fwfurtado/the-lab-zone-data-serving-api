package sink

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Mapper transforma o evento JSON do tópico CDF no proto de resposta da API —
// o value gravado no Valkey é exatamente o que o kv_get devolve.
type Mapper struct {
	desc       protoreflect.MessageDescriptor
	jsonByName map[string]contracts.SinkFieldMap // proto field name -> mapping efetivo
	deleteFlag string
}

func NewMapper(desc protoreflect.MessageDescriptor, spec contracts.SinkSpec) (*Mapper, error) {
	overrides := make(map[string]contracts.SinkFieldMap, len(spec.Fields))
	for _, f := range spec.Fields {
		overrides[f.Proto] = f
	}

	m := &Mapper{desc: desc, jsonByName: map[string]contracts.SinkFieldMap{}, deleteFlag: spec.DeleteFlagField}
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())
		if ov, ok := overrides[name]; ok {
			m.jsonByName[name] = ov
			continue
		}
		m.jsonByName[name] = contracts.SinkFieldMap{Proto: name, JSON: name}
	}
	// valida overrides órfãos (typo no sinks.yaml falha no boot, não em runtime)
	for proto := range overrides {
		if fields.ByName(protoreflect.Name(proto)) == nil {
			return nil, fmt.Errorf("override para campo inexistente %q em %s", proto, desc.FullName())
		}
	}
	return m, nil
}

// Map devolve (mensagem, deleted, erro). deleted=true significa DEL da chave.
func (m *Mapper) Map(raw []byte) (*dynamicpb.Message, bool, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, false, fmt.Errorf("evento não é JSON: %w", err)
	}

	deleted := false
	if m.deleteFlag != "" {
		if rawFlag, ok := event[m.deleteFlag]; ok {
			if err := json.Unmarshal(rawFlag, &deleted); err != nil {
				return nil, false, fmt.Errorf("campo %s não é booleano: %w", m.deleteFlag, err)
			}
		}
	}

	msg := dynamicpb.NewMessage(m.desc)
	fields := m.desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fm := m.jsonByName[string(fd.Name())]
		rawVal, ok := event[fm.JSON]
		if !ok || string(rawVal) == "null" {
			continue // campo ausente fica no zero value do proto
		}
		v, err := coerceJSON(fd, rawVal, fm.Transform)
		if err != nil {
			return nil, false, fmt.Errorf("campo %s (json %s): %w", fd.Name(), fm.JSON, err)
		}
		msg.Set(fd, v)
	}
	return msg, deleted, nil
}

func coerceJSON(fd protoreflect.FieldDescriptor, raw json.RawMessage, transform string) (protoreflect.Value, error) {
	if transform == "iso_millis" {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return protoreflect.Value{}, err
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("timestamp inválido %q: %w", s, err)
		}
		if fd.Kind() != protoreflect.Int64Kind {
			return protoreflect.Value{}, fmt.Errorf("iso_millis exige campo int64")
		}
		return protoreflect.ValueOfInt64(t.UnixMilli()), nil
	}

	switch fd.Kind() {
	case protoreflect.Int64Kind, protoreflect.Int32Kind:
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			return protoreflect.Value{}, err
		}
		if fd.Kind() == protoreflect.Int32Kind {
			return protoreflect.ValueOfInt32(int32(n)), nil
		}
		return protoreflect.ValueOfInt64(n), nil
	case protoreflect.DoubleKind, protoreflect.FloatKind:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return protoreflect.Value{}, err
		}
		if fd.Kind() == protoreflect.FloatKind {
			return protoreflect.ValueOfFloat32(float32(f)), nil
		}
		return protoreflect.ValueOfFloat64(f), nil
	case protoreflect.BoolKind:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfBool(b), nil
	case protoreflect.StringKind:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfString(s), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("kind %s não suportado no sink", fd.Kind())
	}
}
