#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  DEVICE_APPROVAL_BASE_URL=https://cloink.example.com \
  DEVICE_APPROVAL_TOKEN=<api-token> \
  ./scripts/device-approval-smoke.sh [--approve] [--device <name-or-hostname>] [--user <name-or-email>]

Checks a deployed device approval rollout.

Required environment:
  DEVICE_APPROVAL_BASE_URL  Dashboard/management base URL, for example https://cloink.example.com
  DEVICE_APPROVAL_TOKEN     Dashboard API token or bearer token with account/peer/event access

Optional environment:
  DEVICE_APPROVAL_AUTH_HEADER  Authorization header value. Defaults to "Bearer ${DEVICE_APPROVAL_TOKEN}".

Options:
  --approve                 Approve the first matching pending device
  --device <query>          Expected pending device name/hostname/DNS label substring
  --user <query>            Expected pending device owner name/email/user ID substring
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

json_query() {
  jq -r "$1"
}

request() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  local tmp_body tmp_code
  tmp_body="$(mktemp)"
  tmp_code="$(mktemp)"

  if [[ -n "$data" ]]; then
    curl -fsS -X "$method" \
      -H "Authorization: ${AUTH_HEADER}" \
      -H "Content-Type: application/json" \
      --data "$data" \
      -o "$tmp_body" \
      -w "%{http_code}" \
      -- "${BASE_URL}${path}" >"$tmp_code"
  else
    curl -fsS -X "$method" \
      -H "Authorization: ${AUTH_HEADER}" \
      -o "$tmp_body" \
      -w "%{http_code}" \
      -- "${BASE_URL}${path}" >"$tmp_code"
  fi

  local code
  code="$(cat "$tmp_code")"
  if [[ "$code" -lt 200 || "$code" -ge 300 ]]; then
    echo "request failed: ${method} ${path} returned HTTP ${code}" >&2
    cat "$tmp_body" >&2 || true
    rm -f "$tmp_body" "$tmp_code"
    exit 1
  fi

  cat "$tmp_body"
  rm -f "$tmp_body" "$tmp_code"
}

contains_ci() {
  local haystack="$1"
  local needle="$2"
  [[ -z "$needle" ]] && return 0
  printf '%s' "$haystack" | tr '[:upper:]' '[:lower:]' | grep -Fq -- "$(printf '%s' "$needle" | tr '[:upper:]' '[:lower:]')"
}

approve=false
device_query=""
user_query=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --approve)
      approve=true
      shift
      ;;
    --device)
      device_query="${2:-}"
      shift 2
      ;;
    --user)
      user_query="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_cmd curl
require_cmd jq

BASE_URL="${DEVICE_APPROVAL_BASE_URL:-}"
DEVICE_APPROVAL_TOKEN="${DEVICE_APPROVAL_TOKEN:-}"
if [[ -z "$BASE_URL" || -z "$DEVICE_APPROVAL_TOKEN" ]]; then
  usage >&2
  exit 1
fi
BASE_URL="${BASE_URL%/}"
AUTH_HEADER="${DEVICE_APPROVAL_AUTH_HEADER:-Bearer ${DEVICE_APPROVAL_TOKEN}}"

echo "==> Checking public device approval page"
approval_page="$(curl -fsS -- "${BASE_URL}/device-approval?user=smoke@example.com&device=smoke-device&network=smoke-network")"
if ! contains_ci "$approval_page" "smoke-device" || ! contains_ci "$approval_page" "smoke-network"; then
  echo "device approval page did not render query parameters" >&2
  exit 1
fi

echo "==> Checking account setting"
account="$(request GET "/api/accounts")"
peer_approval_enabled="$(printf '%s' "$account" | json_query '.[0].settings.extra.peer_approval_enabled')"
echo "peer_approval_enabled=${peer_approval_enabled}"

if [[ "$peer_approval_enabled" != "true" ]]; then
  echo "warning: device access approval is not enabled on this account" >&2
fi

echo "==> Listing pending devices"
peers="$(request GET "/api/peers")"
users="$(request GET "/api/users?service_user=false")"
pending_devices="$(
  jq -n \
    --argjson peers "$peers" \
    --argjson users "$users" \
    --arg device "$device_query" \
    --arg user "$user_query" '
    def user_for($peer):
      ($users[]? | select(.id == ($peer.user_id // ""))) // {};
    [
      $peers[]
      | . as $peer
      | (user_for($peer)) as $owner
      | select(.approval_required == true)
      | select(
          ($device == "")
          or ((.name // "") | ascii_downcase | contains($device | ascii_downcase))
          or ((.hostname // "") | ascii_downcase | contains($device | ascii_downcase))
          or ((.dns_label // "") | ascii_downcase | contains($device | ascii_downcase))
        )
      | select(
          ($user == "")
          or (($owner.name // "") | ascii_downcase | contains($user | ascii_downcase))
          or (($owner.email // "") | ascii_downcase | contains($user | ascii_downcase))
          or (($owner.id // .user_id // "") | ascii_downcase | contains($user | ascii_downcase))
        )
      | . + {owner: $owner}
    ]'
)"
pending_count="$(printf '%s' "$pending_devices" | jq 'length')"
echo "matching_pending_devices=${pending_count}"

if [[ "$pending_count" -eq 0 ]]; then
  echo "no matching pending devices found"
  exit 0
fi

printf '%s' "$pending_devices" | jq -r '.[] | "- \(.id) \(.name // .hostname // "unknown") owner=\(.owner.email // .owner.name // .owner.id // .user_id // "unknown")"'

if [[ "$approve" != "true" ]]; then
  echo "read-only smoke complete; pass --approve to approve the first matching pending device"
  exit 0
fi

peer_id="$(printf '%s' "$pending_devices" | jq -r '.[0].id')"
peer_payload="$(
  printf '%s' "$pending_devices" | jq -c '.[0] | {
    name,
    ssh_enabled,
    login_expiration_enabled,
    approval_required: false
  }'
)"

echo "==> Approving pending device ${peer_id}"
request PUT "/api/peers/${peer_id}" "$peer_payload" >/dev/null

echo "==> Checking approval log"
events="$(request GET "/api/events/audit")"
event_count="$(
  printf '%s' "$events" | jq --arg peer_id "$peer_id" '
    [
      .[]
      | select(.activity_code == "peer.approve")
      | select((.target_id == $peer_id) or (.meta.peer_id == $peer_id))
    ] | length'
)"
if [[ "$event_count" -eq 0 ]]; then
  echo "approval event for peer ${peer_id} was not found" >&2
  exit 1
fi

echo "approval smoke complete"
