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

docker-build tag="0.1.0":
    docker build -t {{image}}:{{tag}} .

docker-push tag="0.1.0": (docker-build tag)
    docker push {{image}}:{{tag}}
