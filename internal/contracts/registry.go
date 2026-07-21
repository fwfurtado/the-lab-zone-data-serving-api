package contracts

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ResolvedPlan é um Plan já amarrado aos descriptors do método.
type ResolvedPlan struct {
	Plan
	Input  protoreflect.MessageDescriptor
	Output protoreflect.MessageDescriptor
}

// Registry carrega o FileDescriptorSet (descriptors.binpb — mesmo artefato que
// o futuro protoreg publica) e resolve os planos contra ele.
type Registry struct {
	files    *protoregistry.Files
	byMethod map[string]*ResolvedPlan
	services []protoreflect.ServiceDescriptor
}

func Load(descriptorPath, plansPath string) (*Registry, error) {
	raw, err := os.ReadFile(descriptorPath)
	if err != nil {
		return nil, fmt.Errorf("lendo descriptors em %s: %w", descriptorPath, err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		return nil, fmt.Errorf("parseando FileDescriptorSet: %w", err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("construindo registry de files: %w", err)
	}

	plans, err := LoadPlans(plansPath)
	if err != nil {
		return nil, err
	}

	r := &Registry{files: files, byMethod: make(map[string]*ResolvedPlan)}
	seenSvc := map[protoreflect.FullName]bool{}

	for _, p := range plans {
		svcName, methodName, err := splitFullMethod(p.Method)
		if err != nil {
			return nil, err
		}
		desc, err := files.FindDescriptorByName(svcName)
		if err != nil {
			return nil, fmt.Errorf("service %s do plano %s não existe nos descriptors: %w", svcName, p.Method, err)
		}
		svc, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("%s não é um service", svcName)
		}
		m := svc.Methods().ByName(methodName)
		if m == nil {
			return nil, fmt.Errorf("método %s não existe em %s", methodName, svcName)
		}
		if m.IsStreamingClient() || m.IsStreamingServer() {
			return nil, fmt.Errorf("%s: streaming não suportado na v1", p.Method)
		}
		r.byMethod[p.Method] = &ResolvedPlan{Plan: p, Input: m.Input(), Output: m.Output()}
		if !seenSvc[svc.FullName()] {
			seenSvc[svc.FullName()] = true
			r.services = append(r.services, svc)
		}
	}
	return r, nil
}

// PlanFor recebe o fullMethod do gRPC ("/pkg.Svc/Method").
func (r *Registry) PlanFor(fullMethod string) (*ResolvedPlan, bool) {
	p, ok := r.byMethod[fullMethod]
	return p, ok
}

// Files expõe o resolver para o server reflection.
func (r *Registry) Files() *protoregistry.Files { return r.files }

// Services lista os services com plano registrado (para o reflection).
func (r *Registry) Services() []protoreflect.ServiceDescriptor { return r.services }

func splitFullMethod(full string) (protoreflect.FullName, protoreflect.Name, error) {
	trimmed := strings.TrimPrefix(full, "/")
	svc, method, ok := strings.Cut(trimmed, "/")
	if !ok || svc == "" || method == "" {
		return "", "", fmt.Errorf("method inválido: %q (esperado /pkg.Service/Method)", full)
	}
	return protoreflect.FullName(svc), protoreflect.Name(method), nil
}
