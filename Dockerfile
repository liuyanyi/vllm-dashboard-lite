# syntax=docker/dockerfile:1
# Frontend dependencies and compilation are cached independently.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm test && npm run build

# Reusable build target, containing the compiled Linux executable and Go sources.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY *.go ./
COPY --from=web /src/web/dist ./web/dist
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go test ./... && \
    mkdir -p /out && \
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/vllm-monitor .

# Default production target: no compiler, Node.js, source or build dependencies.
FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 monitor && \
    mkdir -p /data /etc/monitor && chown monitor /data
COPY --from=build /out/vllm-monitor /usr/local/bin/vllm-monitor
USER monitor
WORKDIR /data
ENV VLLM_DASHBOARD_CONFIG=/etc/monitor/monitor.yaml
EXPOSE 9090
ENTRYPOINT ["vllm-monitor"]
CMD ["-listen", "0.0.0.0:9090", "-db", "/data/monitor.db"]
