package server

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	v1reflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/fwfurtado/the-lab-zone-data-serving-api/internal/contracts"
)

// Com UnknownServiceHandler não existem services registrados no *grpc.Server,
// então o reflection padrão listaria vazio. Aqui servimos o reflection (v1) a
// partir do MESMO descriptor set do registry — grpcurl e buf curl funcionam
// normalmente. Clientes muito antigos que só falam v1alpha não são suportados
// na v1 do serviço (grpcurl moderno negocia v1).
func RegisterReflection(s *grpc.Server, reg *contracts.Registry) {
	opts := reflection.ServerOptions{
		Services:           serviceInfo{reg: reg},
		DescriptorResolver: reg.Files(),
		ExtensionResolver:  protoregistry.GlobalTypes,
	}
	v1reflectiongrpc.RegisterServerReflectionServer(s, reflection.NewServerV1(opts))
}

// serviceInfo implementa reflection.ServiceInfoProvider listando os services
// que têm plano registrado no registry.
type serviceInfo struct{ reg *contracts.Registry }

func (p serviceInfo) GetServiceInfo() map[string]grpc.ServiceInfo {
	out := make(map[string]grpc.ServiceInfo)
	for _, svc := range p.reg.Services() {
		out[string(svc.FullName())] = grpc.ServiceInfo{}
	}
	return out
}
