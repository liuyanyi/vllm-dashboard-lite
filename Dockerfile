FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -o /monitor .
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 monitor && mkdir /data && chown monitor /data
USER monitor
COPY --from=build /monitor /usr/local/bin/vllm-monitor
WORKDIR /data
EXPOSE 9090
ENTRYPOINT ["vllm-monitor"]
CMD ["-listen", "0.0.0.0:9090", "-db", "/data/monitor.db"]
