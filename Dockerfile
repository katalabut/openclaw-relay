FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay ./cmd/relay

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
RUN mkdir -p /data && chown nobody:nobody /data
COPY --from=builder /app/relay /usr/local/bin/
EXPOSE 8080
USER nobody
ENTRYPOINT ["relay", "-config", "/etc/relay/config.yaml"]
