FROM golang:1.24.4-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/watchdog-server ./cmd/server \
	&& CGO_ENABLED=0 GOOS=linux go build -o /out/watchdog-client ./cmd/client

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
	&& mkdir -p /app /var/log/watchdog

WORKDIR /app

COPY --from=build /out/watchdog-server /usr/local/bin/watchdog-server
COPY --from=build /out/watchdog-client /usr/local/bin/watchdog-client

EXPOSE 8042

CMD ["watchdog-server", "/app/config.yml", "/var/log/watchdog/watchdog.log"]
