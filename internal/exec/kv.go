package exec

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
)

// KVClient serve point lookups do Valkey. Convenção: o value armazenado é o
// PRÓPRIO proto de resposta serializado (o sink escreve exatamente o que a API
// devolve) — o handler valida e repassa, sem transformação.
type KVClient struct {
	rdb *redis.Client
}

func NewKVClient(addr string) *KVClient {
	return &KVClient{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

func (c *KVClient) Close() error { return c.rdb.Close() }

var keyTokenRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func (c *KVClient) Execute(ctx context.Context, plan *contracts.ResolvedPlan, req protoreflect.Message) (*dynamicpb.Message, error) {
	key, err := renderKey(plan.KV.KeyTemplate, req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "montando chave: %v", err)
	}

	raw, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, status.Error(codes.NotFound, "registro não encontrado")
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "valkey: %v", err)
	}

	out := dynamicpb.NewMessage(plan.Output)
	if err := proto.Unmarshal(raw, out); err != nil {
		return nil, status.Errorf(codes.Internal, "value em %s não é um %s válido: %v", key, plan.Output.FullName(), err)
	}
	return out, nil
}

func renderKey(template string, req protoreflect.Message) (string, error) {
	var bindErr error
	key := keyTokenRe.ReplaceAllStringFunc(template, func(tok string) string {
		if bindErr != nil {
			return tok
		}
		name := tok[1 : len(tok)-1]
		fd := req.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			bindErr = fmt.Errorf("token {%s} não existe na request", name)
			return tok
		}
		v := req.Get(fd)
		switch fd.Kind() {
		case protoreflect.Int64Kind, protoreflect.Int32Kind:
			return strconv.FormatInt(v.Int(), 10)
		case protoreflect.Uint64Kind, protoreflect.Uint32Kind:
			return strconv.FormatUint(v.Uint(), 10)
		case protoreflect.StringKind:
			return v.String()
		default:
			bindErr = fmt.Errorf("kind %s não suportado em chave", fd.Kind())
			return tok
		}
	})
	return key, bindErr
}
