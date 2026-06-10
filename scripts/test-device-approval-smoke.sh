#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_SCRIPT="$(mktemp)"
SERVER_LOG="$(mktemp)"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  rm -f "$SERVER_SCRIPT" "$SERVER_LOG"
}
trap cleanup EXIT

cat >"$SERVER_SCRIPT" <<'PY'
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import sys

approved = False

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def write_json(self, data, status=200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        global approved
        auth = self.headers.get("Authorization", "")
        if self.path.startswith("/api/") and auth != "Bearer test-token":
            self.write_json({"error": "bad auth"}, 401)
            return

        if self.path.startswith("/device-approval"):
            body = b"<main>smoke-device smoke-network smoke@example.com</main>"
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == "/api/accounts":
            self.write_json([{"settings": {"extra": {"peer_approval_enabled": True}}}])
            return

        if self.path == "/api/peers":
            self.write_json([
                {
                    "id": "peer-1",
                    "name": "smoke-device",
                    "hostname": "smoke-host",
                    "dns_label": "smoke-device.example.test",
                    "user_id": "user-1",
                    "ssh_enabled": False,
                    "login_expiration_enabled": True,
                    "approval_required": not approved,
                }
            ])
            return

        if self.path == "/api/users?service_user=false":
            self.write_json([
                {"id": "user-1", "name": "Smoke User", "email": "smoke@example.com"}
            ])
            return

        if self.path == "/api/events/audit":
            events = []
            if approved:
                events.append({
                    "activity_code": "peer.approve",
                    "target_id": "peer-1",
                    "meta": {"peer_id": "peer-1"},
                })
            self.write_json(events)
            return

        self.write_json({"error": "not found", "path": self.path}, 404)

    def do_PUT(self):
        global approved
        auth = self.headers.get("Authorization", "")
        if auth != "Bearer test-token":
            self.write_json({"error": "bad auth"}, 401)
            return

        if self.path == "/api/peers/peer-1":
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length) or b"{}")
            if payload.get("approval_required") is not False:
                self.write_json({"error": "approval_required must be false"}, 400)
                return
            approved = True
            self.write_json({"id": "peer-1", "approval_required": False})
            return

        self.write_json({"error": "not found", "path": self.path}, 404)

server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
print(server.server_address[1], flush=True)
server.serve_forever()
PY

python3 "$SERVER_SCRIPT" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  if [[ -s "$SERVER_LOG" ]]; then
    break
  fi
  sleep 0.1
done

PORT="$(head -n 1 "$SERVER_LOG")"
if [[ -z "$PORT" ]]; then
  echo "stub server did not start" >&2
  cat "$SERVER_LOG" >&2 || true
  exit 1
fi

BASE_URL="http://127.0.0.1:${PORT}"

readonly_output="$(
  DEVICE_APPROVAL_BASE_URL="$BASE_URL" \
  DEVICE_APPROVAL_TOKEN="test-token" \
  "$ROOT_DIR/scripts/device-approval-smoke.sh" \
    --device smoke-device \
    --user smoke@example.com
)"
printf '%s\n' "$readonly_output"

if ! grep -Fq "matching_pending_devices=1" <<<"$readonly_output"; then
  echo "read-only smoke did not find the pending device" >&2
  exit 1
fi
if ! grep -Fq "read-only smoke complete" <<<"$readonly_output"; then
  echo "read-only smoke did not finish in read-only mode" >&2
  exit 1
fi

approve_output="$(
  DEVICE_APPROVAL_BASE_URL="$BASE_URL" \
  DEVICE_APPROVAL_TOKEN="test-token" \
  "$ROOT_DIR/scripts/device-approval-smoke.sh" \
    --device smoke-device \
    --user smoke@example.com \
    --approve
)"
printf '%s\n' "$approve_output"

if ! grep -Fq "approval smoke complete" <<<"$approve_output"; then
  echo "approve smoke did not complete" >&2
  exit 1
fi
