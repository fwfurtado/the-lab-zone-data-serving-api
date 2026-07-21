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

# inicia port-forwards compartilhados; idempotente enquanto houver sessões ativas
_start-port-forward session:
    #!/usr/bin/env bash
    set -euo pipefail

    state_dir="${PORT_FORWARD_STATE_DIR:-/tmp/the-lab-zone-data-serving-api-port-forward}"
    sessions_dir="$state_dir/sessions"
    logs_dir="$state_dir/logs"
    pids_file="$state_dir/pids"
    mutex_file="$state_dir/mutex"
    session="{{session}}"

    mkdir -p "$sessions_dir" "$logs_dir"

    prune_sessions() {
        local file pid
        for file in "$sessions_dir"/session.*; do
            [[ -e "$file" ]] || continue
            pid="$(cat "$file" 2>/dev/null || true)"
            if [[ ! "$pid" =~ ^[0-9]+$ ]] || ! kill -0 "$pid" 2>/dev/null; then
                rm -f "$file"
            fi
        done
    }

    forwards_alive() {
        local pid
        [[ -s "$pids_file" ]] || return 1
        while read -r pid; do
            [[ -n "$pid" ]] || continue
            kill -0 "$pid" 2>/dev/null || return 1
        done < "$pids_file"
    }

    ports_ready() {
        : > /dev/tcp/127.0.0.1/8099 &&
        : > /dev/tcp/127.0.0.1/6379 &&
        : > /dev/tcp/127.0.0.1/9093
    }

    stop_forwards() {
        local pid
        if [[ -s "$pids_file" ]]; then
            while read -r pid; do
                [[ -n "$pid" ]] || continue
                kill "$pid" 2>/dev/null || true
            done < "$pids_file"
        fi
    }

    start_forwards() {
        : > "$pids_file"
        kubectl -n data port-forward svc/pinot-broker 8099:8099 > "$logs_dir/pinot-broker.log" 2>&1 &
        echo "$!" >> "$pids_file"
        kubectl -n data port-forward svc/valkey 6379:6379 > "$logs_dir/valkey.log" 2>&1 &
        echo "$!" >> "$pids_file"
        kubectl -n data port-forward pod/redpanda-0 9093:9093 > "$logs_dir/redpanda-0.log" 2>&1 &
        echo "$!" >> "$pids_file"
    }

    (
        flock 9
        prune_sessions

        if ! forwards_alive; then
            stop_forwards
            start_forwards

            for _ in {1..50}; do
                if forwards_alive && ports_ready 2>/dev/null; then
                    exit 0
                fi
                sleep 0.2
            done

            echo "port-forward não ficou pronto" >&2
            for log in "$logs_dir"/*.log; do
                [[ -f "$log" ]] || continue
                echo "--- $log ---" >&2
                cat "$log" >&2
            done
            stop_forwards
            rm -f "$pids_file" "$session"
            exit 1
        fi
    ) 9>"$mutex_file"

# encerra port-forwards compartilhados quando a última sessão termina
_stop-port-forward session:
    #!/usr/bin/env bash
    set -euo pipefail

    state_dir="${PORT_FORWARD_STATE_DIR:-/tmp/the-lab-zone-data-serving-api-port-forward}"
    sessions_dir="$state_dir/sessions"
    logs_dir="$state_dir/logs"
    pids_file="$state_dir/pids"
    mutex_file="$state_dir/mutex"
    session="{{session}}"

    mkdir -p "$sessions_dir"

    prune_sessions() {
        local file pid
        for file in "$sessions_dir"/session.*; do
            [[ -e "$file" ]] || continue
            pid="$(cat "$file" 2>/dev/null || true)"
            if [[ ! "$pid" =~ ^[0-9]+$ ]] || ! kill -0 "$pid" 2>/dev/null; then
                rm -f "$file"
            fi
        done
    }

    stop_forwards() {
        local pid
        if [[ -s "$pids_file" ]]; then
            while read -r pid; do
                [[ -n "$pid" ]] || continue
                kill "$pid" 2>/dev/null || true
            done < "$pids_file"
        fi
    }

    (
        flock 9
        rm -f "$session"
        prune_sessions

        if ! compgen -G "$sessions_dir/session.*" > /dev/null; then
            stop_forwards
            rm -f "$pids_file"
            rm -rf "$sessions_dir" "$logs_dir"
        fi
    ) 9>"$mutex_file"

# roda local apontando pro Pinot/Valkey via port-forward
run: descriptors
    #!/usr/bin/env bash
    set -euo pipefail

    state_dir="${PORT_FORWARD_STATE_DIR:-/tmp/the-lab-zone-data-serving-api-port-forward}"
    sessions_dir="$state_dir/sessions"
    mkdir -p "$sessions_dir"

    session="$(mktemp "$sessions_dir/session.XXXXXX")"
    printf '%s\n' "$$" > "$session"

    cleanup() {
        just _stop-port-forward "$session"
    }
    trap cleanup EXIT

    just _start-port-forward "$session"

    PINOT_BROKER_URL="${PINOT_BROKER_URL:-http://localhost:8099}" \
    VALKEY_ADDR="${VALKEY_ADDR:-localhost:6379}" \
    VALKEY_PASSWORD=$(op read "op://the-lab-zone/valkey/password") \
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

add-etc-host-entry:
    grep -qxF "127.0.0.1 redpanda-0.redpanda.data.svc.cluster.local" /etc/hosts || echo "127.0.0.1 redpanda-0.redpanda.data.svc.cluster.local" | sudo tee -a /etc/hosts

# roda o kv-sink local (port-forward do redpanda e do valkey)
run-sink: descriptors pull-cert add-etc-host-entry
    #!/usr/bin/env bash
    set -euo pipefail

    state_dir="${PORT_FORWARD_STATE_DIR:-/tmp/the-lab-zone-data-serving-api-port-forward}"
    sessions_dir="$state_dir/sessions"
    mkdir -p "$sessions_dir"

    session="$(mktemp "$sessions_dir/session.XXXXXX")"
    printf '%s\n' "$$" > "$session"

    cleanup() {
        just _stop-port-forward "$session"
    }
    trap cleanup EXIT

    just _start-port-forward "$session"

    KAFKA_BROKERS="${KAFKA_BROKERS:-redpanda-0.redpanda.data.svc.cluster.local:9093}" \
    KAFKA_USERNAME="${KAFKA_USERNAME:-kv-sink}" \
    KAFKA_PASSWORD=$(op read "op://the-lab-zone/redpanda-kv-sink/password") \
    KAFKA_CA_PATH=/tmp/redpanda-ca.crt \
    VALKEY_ADDR="${VALKEY_ADDR:-localhost:6379}" \
    VALKEY_PASSWORD=$(op read "op://the-lab-zone/valkey/password") \
    go run ./cmd/kv-sink

docker-build tag="0.1.0":
    docker build -t {{image}}:{{tag}} .

docker-push tag="0.1.0": (docker-build tag)
    docker push {{image}}:{{tag}}
