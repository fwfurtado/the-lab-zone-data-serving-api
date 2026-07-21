# the-lab-zone-data-serving-api

Data API gRPC da plataforma de serving do homelab: a última milha do pipeline
**Delta (CDF) → Redpanda → Pinot upsert / Valkey → esta API → consumidores**.

## Modelo de execução

Um único servidor genérico, **zero código gerado no servidor**:

1. O CI compila os contratos (`proto/`) num `FileDescriptorSet`
   (`build/descriptors.binpb`) via `buf build` — o mesmo artefato que o futuro
   `protoreg` publicará.
2. `config/plans.yaml` liga cada método gRPC a um **plano de execução**
   (`pinot_query` ou `kv_get`). Hoje escrito à mão; no desenho final é gerado a
   partir do DatasetContract.
3. Em runtime, o `grpc.UnknownServiceHandler` recebe qualquer método, decodifica
   a request via `dynamicpb` usando o descriptor, executa o plano e responde.
   O consumidor nunca envia SQL — só valores para slots tipados de templates
   pré-aprovados.

Onboarding de dataset novo = proto novo + entrada no plans.yaml + rebuild dos
descriptors. Sem deploy de código.

## Layout

```
cmd/server/            main: wiring, health, reflection, graceful shutdown
internal/config/       config por env
internal/contracts/    plans.yaml + FileDescriptorSet -> registry resolvido
internal/server/       dispatch dinâmico + reflection servido dos descriptors
internal/exec/         executors: pinot (broker REST) e kv (Valkey), binder SQL
internal/observability otel traces (OTLP -> collector -> Tempo)
proto/lab/serving/v1/  contrato de exemplo: accounts (schema real do Pinot)
config/plans.yaml      planos de exemplo (GetAccount, AggAccountsByRegion)
```

## Rodando

Pré-requisitos: Go 1.24+, buf CLI, e port-forwards para o cluster:

```bash
kubectl -n data port-forward svc/pinot-broker 8099:8099 &
kubectl -n data port-forward svc/valkey 6379:6379 &   # só quando houver plano kv_get

just tidy         # primeira vez
just run          # buf build + go run
just smoke        # grpcurl via reflection
```

Variáveis: `LISTEN_ADDR`, `DESCRIPTORS_PATH`, `PLANS_PATH`, `PINOT_BROKER_URL`,
`VALKEY_ADDR`, `REQUEST_TIMEOUT`, `OTEL_EXPORTER_OTLP_ENDPOINT`,
`OTEL_SERVICE_NAME` (defaults em `internal/config/config.go`, já apontando para
os services do namespace `data`).

## Convenções que o código assume

- **Aliases SQL == nomes de campo proto**: o mapper liga coluna a campo pelo
  nome (`CAST(updated_at AS LONG) AS updated_at_ms` → campo `updated_at_ms`).
- **`single_row`**: 0 linhas → `NOT_FOUND`; o output é o próprio message.
- **`rows`**: o output precisa ter um campo `repeated` de message (ex.:
  `rows`), preenchido linha a linha.
- **`kv_get`**: o value no Valkey é o **proto de resposta já serializado** —
  o sink escreve exatamente o que a API devolve.
- Timestamps trafegam como epoch millis (`int64`) na v1.

## Dívidas conscientes da v1 (em ordem de dor)

1. **Hot-reload do registry**: descriptors e plans só carregam no boot.
2. **SLO por plano**: timeout hoje é global (`REQUEST_TIMEOUT`); o contrato
   prevê p99/QPS por access pattern (deadline + circuit breaker por plano).
3. **Métricas RED por método**: só traces por enquanto (span_metrics do
   collector cobre parte); falta exemplar de p99 por plano vs SLO declarado.
4. **Sink KV inexistente no pipeline**: `kv_get` está implementado, mas nada
   escreve no Valkey ainda — GetAccount serve via Pinot até lá.
5. **Multi-get / batch** (`kv_multi_get`, `MGET`).
6. **Well-known types** no mapper (Timestamp em vez de epoch millis).
7. **v1alpha reflection** não suportado (grpcurl moderno usa v1).

## Deploy (fora deste repo)

Manifests, HTTPRoute/GRPCRoute, ExternalSecret e as CiliumNetworkPolicies
(api→pinot-broker:8099, api→valkey:6379, ingress Gateway→api:9090) seguem o
padrão do `the-lab-zone` em `apps/data/`.
