FROM golang:1.25-alpine AS builder

RUN apk update && apk add --no-cache git tzdata ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o api-gateway ./cmd/gateway

FROM alpine:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

WORKDIR /app

COPY --from=builder /app/api-gateway .

COPY config.yaml .
COPY .env .

EXPOSE 8080

CMD ["./api-gateway"]