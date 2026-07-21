package server

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/status"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/debugx"
	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/observability"
)

// Executor é implementado pelos backends (Pinot, Valkey).
type Executor interface {
	Execute(ctx context.Context, plan *contracts.ResolvedPlan, req protoreflect.Message) (*dynamicpb.Message, error)
}

type Dispatcher struct {
	registry *contracts.Registry
	pinot    Executor
	kv       Executor
	logger   *slog.Logger
	metrics  *observability.Metrics
	timeout  time.Duration
}

func NewDispatcher(reg *contracts.Registry, pinot, kv Executor, logger *slog.Logger, metrics *observability.Metrics, timeout time.Duration) *Dispatcher {
	return &Dispatcher{registry: reg, pinot: pinot, kv: kv, logger: logger, metrics: metrics, timeout: timeout}
}

// Handler é o catch-all do gRPC: nenhum código gerado no servidor. A request é
// decodificada dinamicamente a partir do descriptor do contrato, roteada pelo
// plano e a resposta volta como dynamicpb — wire format idêntico ao de um
// servidor "normal".
func (d *Dispatcher) Handler() grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		fullMethod, ok := grpc.MethodFromServerStream(stream)
		if !ok {
			return status.Error(codes.Internal, "sem método no stream")
		}

		// RED por método/plano: toda saída do handler (sucesso ou erro)
		// passa por aqui — o código gRPC vira o label de erro
		start := time.Now()
		planType := "none"
		var handlerErr error
		defer func() {
			d.metrics.Observe(fullMethod, planType, status.Code(handlerErr).String(), time.Since(start).Seconds())
		}()

		plan, ok := d.registry.PlanFor(fullMethod)
		if !ok {
			handlerErr = status.Errorf(codes.Unimplemented, "sem contrato registrado para %s", fullMethod)
			return handlerErr
		}
		planType = string(plan.Type)

		req := dynamicpb.NewMessage(plan.Input)
		if err := stream.RecvMsg(req); err != nil {
			handlerErr = status.Errorf(codes.InvalidArgument, "decodificando request: %v", err)
			return handlerErr
		}
		if d.logger.Enabled(stream.Context(), slog.LevelDebug) {
			// Info por request distorce benchmark e enche log em QPS alto
			d.logger.Debug("request decodificada", "method", fullMethod, "req", debugx.JSON(req))
		}

		ctx, cancel := context.WithTimeout(stream.Context(), d.timeout)
		defer cancel()

		var (
			resp *dynamicpb.Message
			err  error
		)
		switch plan.Type {
		case contracts.PlanPinotQuery:
			resp, err = d.pinot.Execute(ctx, plan, req)
		case contracts.PlanKVGet:
			resp, err = d.kv.Execute(ctx, plan, req)
		default:
			err = status.Errorf(codes.Internal, "plano de tipo desconhecido: %s", plan.Type)
		}
		if err != nil {
			handlerErr = err
			return handlerErr
		}
		handlerErr = stream.SendMsg(resp)
		return handlerErr
	}
}
