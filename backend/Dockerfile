FROM golang:1.24.4-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/watchdog-server ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
	&& mkdir -p /app /var/log/watchdog

WORKDIR /app

COPY --from=build /out/watchdog-server /usr/local/bin/watchdog-server

EXPOSE 8042

CMD ["watchdog-server", "/app/config.yml", "/var/log/watchdog/watchdog.log"]
