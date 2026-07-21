package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
)

type PinotClient struct {
	brokerURL string // ex.: http://pinot-broker.data.svc.cluster.local:8099
	http      *http.Client
}

func NewPinotClient(brokerURL string, timeout time.Duration) *PinotClient {
	return &PinotClient{
		brokerURL: brokerURL,
		http:      &http.Client{Timeout: timeout},
	}
}

// Formato da resposta do broker (POST /query/sql).
type pinotResponse struct {
	ResultTable *struct {
		DataSchema struct {
			ColumnNames     []string `json:"columnNames"`
			ColumnDataTypes []string `json:"columnDataTypes"`
		} `json:"dataSchema"`
		Rows [][]json.RawMessage `json:"rows"`
	} `json:"resultTable"`
	Exceptions []struct {
		ErrorCode int    `json:"errorCode"`
		Message   string `json:"message"`
	} `json:"exceptions"`
}

func (c *PinotClient) Execute(ctx context.Context, plan *contracts.ResolvedPlan, req protoreflect.Message) (*dynamicpb.Message, error) {
	sql, err := BindSQL(plan.Pinot.SQL, req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "bind de parâmetros: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"sql": sql})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.brokerURL+"/query/sql", bytes.NewReader(body))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "montando request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "broker pinot: %v", err)
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "lendo resposta do broker: %v", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, status.Errorf(codes.Unavailable, "broker respondeu %d: %s", httpResp.StatusCode, truncate(raw, 300))
	}

	var pr pinotResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, status.Errorf(codes.Internal, "parseando resposta do broker: %v", err)
	}
	if len(pr.Exceptions) > 0 {
		return nil, status.Errorf(codes.Internal, "pinot exception %d: %s", pr.Exceptions[0].ErrorCode, pr.Exceptions[0].Message)
	}
	if pr.ResultTable == nil {
		pr.ResultTable = &struct {
			DataSchema struct {
				ColumnNames     []string `json:"columnNames"`
				ColumnDataTypes []string `json:"columnDataTypes"`
			} `json:"dataSchema"`
			Rows [][]json.RawMessage `json:"rows"`
		}{}
	}

	cols := pr.ResultTable.DataSchema.ColumnNames
	rows := pr.ResultTable.Rows

	switch plan.Pinot.Result {
	case contracts.ResultSingleRow:
		if len(rows) == 0 {
			return nil, status.Error(codes.NotFound, "registro não encontrado")
		}
		out := dynamicpb.NewMessage(plan.Output)
		if err := mapRow(out, cols, rows[0]); err != nil {
			return nil, status.Errorf(codes.Internal, "mapeando linha: %v", err)
		}
		return out, nil

	case contracts.ResultRows:
		out := dynamicpb.NewMessage(plan.Output)
		rowsField, err := repeatedMessageField(plan.Output)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		list := out.Mutable(rowsField).List()
		for _, r := range rows {
			item := dynamicpb.NewMessage(rowsField.Message())
			if err := mapRow(item, cols, r); err != nil {
				return nil, status.Errorf(codes.Internal, "mapeando linha: %v", err)
			}
			list.Append(protoreflect.ValueOfMessage(item))
		}
		return out, nil
	}
	return nil, status.Error(codes.Internal, "modo de resultado desconhecido")
}

// mapRow liga colunas do resultTable a campos do proto pelo NOME — por isso os
// aliases dos templates SQL devem casar com os nomes dos campos do contrato.
func mapRow(msg *dynamicpb.Message, cols []string, row []json.RawMessage) error {
	fields := msg.Descriptor().Fields()
	for i, col := range cols {
		if i >= len(row) {
			break
		}
		fd := fields.ByName(protoreflect.Name(col))
		if fd == nil {
			continue // coluna sem campo correspondente: ignorada de propósito
		}
		v, err := coerce(fd, row[i])
		if err != nil {
			return fmt.Errorf("coluna %s: %w", col, err)
		}
		msg.Set(fd, v)
	}
	return nil
}

func coerce(fd protoreflect.FieldDescriptor, raw json.RawMessage) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.Int64Kind, protoreflect.Int32Kind, protoreflect.Sint64Kind, protoreflect.Sint32Kind:
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			return protoreflect.Value{}, err
		}
		if fd.Kind() == protoreflect.Int32Kind || fd.Kind() == protoreflect.Sint32Kind {
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
		return protoreflect.Value{}, fmt.Errorf("kind %s não suportado no mapper", fd.Kind())
	}
}

func repeatedMessageField(desc protoreflect.MessageDescriptor) (protoreflect.FieldDescriptor, error) {
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.IsList() && fd.Kind() == protoreflect.MessageKind {
			return fd, nil
		}
	}
	return nil, fmt.Errorf("output %s não tem campo repeated de message (exigido por result: rows)", desc.FullName())
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
