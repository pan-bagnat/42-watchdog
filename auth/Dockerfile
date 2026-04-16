# 1. Build the Go binary
FROM golang:1.25 AS builder

WORKDIR /app

COPY srcs/go.mod srcs/go.sum ./
RUN go mod download

COPY ./srcs .

RUN CGO_ENABLED=0 GOOS=linux go build -o main .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
  && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1001 app \
 && useradd -u 1001 -g app -m -s /bin/sh app \
 && mkdir -p /app \
 && chown -R app:app /app

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/docs /app/docs

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

USER app

EXPOSE 8080

ENTRYPOINT ["sh", "/app/entrypoint.sh"]
CMD []
