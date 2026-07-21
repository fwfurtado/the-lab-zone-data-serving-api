# syntax=docker/dockerfile:1
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# descriptors são gerados no CI (buf) e copiados prontos; no build local,
# rode `just descriptors` antes do docker build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kv-sink ./cmd/kv-sink

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/kv-sink /app/kv-sink
COPY config/sinks.yaml /app/config/sinks.yaml
ENV SINKS_PATH=/app/config/sinks.yaml
COPY build/descriptors.binpb /app/build/descriptors.binpb
COPY config/plans.yaml /app/config/plans.yaml
ENV DESCRIPTORS_PATH=/app/build/descriptors.binpb \
    PLANS_PATH=/app/config/plans.yaml
EXPOSE 9090
ENTRYPOINT ["/app/server"]
