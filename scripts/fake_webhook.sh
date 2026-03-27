#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
DEFAULT_URL="http://localhost/webhook/access-control"
ENV_FILE="${ENV_FILE:-${REPO_ROOT}/.env}"
TARGET_URL="$DEFAULT_URL"
TIMESTAMP_OVERRIDE=""

for arg in "$@"; do
  if [[ "$arg" =~ ^[0-9]{2}:[0-9]{2}$ ]]; then
    if [[ -n "$TIMESTAMP_OVERRIDE" ]]; then
      echo "error: provide at most one time override in HH:MM format" >&2
      exit 1
    fi
    TIMESTAMP_OVERRIDE="$arg"
  elif [[ "$arg" =~ ^https?:// ]]; then
    if [[ "$TARGET_URL" != "$DEFAULT_URL" ]]; then
      echo "error: provide at most one target URL" >&2
      exit 1
    fi
    TARGET_URL="$arg"
  else
    echo "error: unsupported argument: $arg" >&2
    echo "usage: $0 [http://host/webhook/access-control] [HH:MM]" >&2
    exit 1
  fi
done

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: missing required command: $1" >&2
    exit 1
  fi
}

load_webhook_secret() {
  if [[ -n "${WEBHOOK_SECRET:-}" ]]; then
    printf '%s' "$WEBHOOK_SECRET"
    return
  fi

  if [[ ! -f "$ENV_FILE" ]]; then
    echo "error: WEBHOOK_SECRET is not set and env file was not found: $ENV_FILE" >&2
    exit 1
  fi

  local secret
  secret="$(awk -F= '/^WEBHOOK_SECRET=/{sub(/^[^=]*=/, ""); print; exit}' "$ENV_FILE")"
  if [[ -z "$secret" ]]; then
    echo "error: WEBHOOK_SECRET was not found in $ENV_FILE" >&2
    exit 1
  fi

  printf '%s' "$secret"
}

require_command curl
require_command openssl
require_command xxd

if [[ -n "$TIMESTAMP_OVERRIDE" ]]; then
  NOW="$(
    TZ=Europe/Paris date -d "$(TZ=Europe/Paris date '+%Y-%m-%d') ${TIMESTAMP_OVERRIDE}:00" '+%Y-%m-%d %H:%M:%S' 2>/dev/null
  )"
  if [[ -z "$NOW" ]]; then
    echo "error: invalid time override: $TIMESTAMP_OVERRIDE" >&2
    exit 1
  fi
else
  NOW="$(TZ=Europe/Paris date '+%Y-%m-%d %H:%M:%S')"
fi

WEBHOOK_SECRET_VALUE="$(load_webhook_secret)"

PAYLOAD="$(cat <<EOF
{
  "ibox_ip": "62.129.8.172",
  "ibox_sn": "200702",
  "datetime": "${NOW}",
  "category": "events",
  "action": "new-event",
  "operator": null,
  "data": {
    "url": "/api/events/2535217/",
    "code": 48,
    "name": "Acc\\u00e8s autoris\\u00e9",
    "level": "low",
    "date_time": "${NOW}",
    "user": 1135,
    "company": "",
    "device": "0001",
    "badge": 19561536,
    "data": {
      "device_name": "ControllerBocalSDV",
      "door_name": "Bocal",
      "badge_number": "19561536",
      "user_name": "[STAFF] Mathieu Agostini Heinz",
      "entry_number": null
    }
  }
}
EOF
)"

SIGNATURE="$(
  printf '%s' "$PAYLOAD" \
    | openssl dgst -sha512 -hmac "$WEBHOOK_SECRET_VALUE" -binary \
    | xxd -p -c 256
)"

echo "POST ${TARGET_URL}"
echo "datetime=${NOW}"

curl \
  --silent \
  --show-error \
  --include \
  --request POST \
  --url "$TARGET_URL" \
  --header 'Content-Type: application/json' \
  --header "x-webhook-signature: ${SIGNATURE}" \
  --data "$PAYLOAD"
