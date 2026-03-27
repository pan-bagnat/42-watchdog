#!/usr/bin/env bash

set -euo pipefail

PORT="${1:-8081}"
READ_TIMEOUT="${READ_TIMEOUT:-1}"

if ! [[ "$PORT" =~ ^[0-9]+$ ]] || (( PORT < 1 || PORT > 65535 )); then
  echo "usage: $0 [port]" >&2
  exit 1
fi

if ! [[ "$READ_TIMEOUT" =~ ^[0-9]+$ ]] || (( READ_TIMEOUT < 1 )); then
  echo "error: READ_TIMEOUT must be a positive integer" >&2
  exit 1
fi

serve_with_ncat() {
  export PORT READ_TIMEOUT
  ncat -lk "$PORT" --sh-exec '
    payload="$(timeout "${READ_TIMEOUT}" cat || true)"
    printf "\n[%s] connection on port %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "$PORT" >&2
    printf "%s\n" "----------------------------------------" >&2
    printf "%s" "$payload" >&2
    printf "\n%s\n" "========================================" >&2
    printf "HTTP/1.1 200 OK\r\n"
    printf "Content-Type: text/plain\r\n"
    printf "Content-Length: 3\r\n"
    printf "Connection: close\r\n"
    printf "\r\nok\n"
  '
}

serve_with_socat() {
  export PORT READ_TIMEOUT
  socat -T 0 "TCP-LISTEN:${PORT},reuseaddr,fork" SYSTEM:'
    payload="$(timeout "${READ_TIMEOUT}" cat || true)"
    printf "\n[%s] connection on port %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "$PORT" >&2
    printf "%s\n" "----------------------------------------" >&2
    printf "%s" "$payload" >&2
    printf "\n%s\n" "========================================" >&2
    printf "HTTP/1.1 200 OK\r\n"
    printf "Content-Type: text/plain\r\n"
    printf "Content-Length: 3\r\n"
    printf "Connection: close\r\n"
    printf "\r\nok\n"
  '
}

if command -v ncat >/dev/null 2>&1; then
  echo "Listening on port ${PORT} with ncat"
  serve_with_ncat
fi

if command -v socat >/dev/null 2>&1; then
  echo "Listening on port ${PORT} with socat"
  serve_with_socat
fi

echo "error: install one of: ncat, socat" >&2
exit 1
