set shell := ["bash", "-cu"]

image := "ghcr.io/fwfurtado/the-lab-zone-data-serving-api"

default:
    @just --list

# go mod tidy (resolve deps declaradas no go.mod)
tidy:
    go mod tidy

# compila descriptors.binpb a partir de proto/ (buf CLI, com imports transitivos)
descriptors:
    @ mkdir -p build
    @ buf build proto -o build/descriptors.binpb

# roda local apontando pro Pinot/Valkey via port-forward
run: descriptors
    PINOT_BROKER_URL="${PINOT_BROKER_URL:-http://localhost:8099}" \
    VALKEY_ADDR="${VALKEY_ADDR:-localhost:6379}" \
    go run ./cmd/server

test:
    go test ./...

lint:
    buf lint proto
    go vet ./...

# smoke via reflection (exige servidor rodando)
smoke:
    grpcurl -plaintext localhost:9090 list
    grpcurl -plaintext -d '{"account_id": 1}' localhost:9090 lab.serving.v1.AccountsService/GetAccount

ui:
    grpcui -plaintext localhost:9090

pull-cert:
    kubectl -n data get secret redpanda-default-cert -o jsonpath='{.data.ca\.crt}' | base64 -d > /tmp/redpanda-ca.crt

# roda o kv-sink local (port-forward do redpanda e do valkey)
run-sink: descriptors pull-cert
    KAFKA_BROKERS="redpanda-0.redpanda.data.svc.cluster.local:9093" \
    KAFKA_USERNAME=kv-sink \
    KAFKA_PASSWORD=(op read "op://the-lab-zone/redpanda-kv-sink/password") \
    KAFKA_CA_PATH=/tmp/redpanda-ca.crt \
    VALKEY_ADDR="localhost:6379" \
    VALKEY_PASSWORD=(op read "op://the-lab-zone/valkey/password") \
    go run ./cmd/kv-sink

docker-build tag="0.1.0":
    docker build -t {{image}}:{{tag}} .

docker-push tag="0.1.0": (docker-build tag)
    docker push {{image}}:{{tag}}
