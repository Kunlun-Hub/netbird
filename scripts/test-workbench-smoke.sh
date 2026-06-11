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
import re

server_resources = []
personal_resources = []
assets = {}
recent_visits = {}
server_counter = 1
personal_counter = 1
asset_counter = 1

TOKENS = {
    "Bearer admin-token": {"account_id": "account-1", "user_id": "admin-1", "role": "admin", "groups": ["group-a"]},
    "Bearer user-token": {"account_id": "account-1", "user_id": "user-1", "role": "user", "groups": ["group-a"]},
    "Bearer other-user-token": {"account_id": "account-1", "user_id": "user-2", "role": "user", "groups": []},
}

def user_auth(handler):
    auth = handler.headers.get("Authorization", "")
    return TOKENS.get(auth)

def with_recent_metadata(resource, auth):
    resource = dict(resource)
    key = (auth["user_id"], resource.get("scope", ""), resource.get("id", ""))
    visited_at = recent_visits.get(key)
    if visited_at:
        metadata = dict(resource.get("metadata") or {})
        metadata["recentVisitedAt"] = visited_at
        resource["metadata"] = metadata
    return resource

def server_resource_visible(resource, auth):
    if resource.get("enabled") is False:
        return False
    visibility = resource.get("visibility") or "all"
    if visibility == "all":
        return True
    visible_groups = resource.get("visibleGroups") or []
    visible_users = resource.get("visibleUsers") or []
    if auth["user_id"] in visible_users:
        return True
    return any(group_id in auth.get("groups", []) for group_id in visible_groups)

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

    def read_json(self):
        length = int(self.headers.get("Content-Length", "0"))
        if length == 0:
            return {}
        return json.loads(self.rfile.read(length))

    def ensure_auth(self):
        auth = user_auth(self)
        if not auth:
            self.write_json({"error": "bad auth"}, 401)
            return None
        return auth

    def do_GET(self):
        auth = self.ensure_auth()
        if not auth:
            return

        if self.path == "/api/workbench/admin/resources":
            if auth["role"] != "admin":
                self.write_json({"error": "forbidden"}, 403)
                return
            self.write_json(server_resources)
            return

        if self.path == "/api/workbench/resources":
            self.write_json({
                "serverResources": [with_recent_metadata(resource, auth) for resource in server_resources if server_resource_visible(resource, auth)],
                "personalResources": [with_recent_metadata(resource, auth) for resource in personal_resources if resource.get("userId") == auth["user_id"] and resource.get("enabled") is not False],
                "categories": [],
                "version": 1,
            })
            return

        asset_match = re.fullmatch(r"/api/workbench/assets/([^/]+)", self.path)
        if asset_match:
            asset_id = asset_match.group(1)
            asset = assets.get(asset_id)
            if not asset:
                self.write_json({"error": "not found"}, 404)
                return
            if asset["account_id"] != auth["account_id"]:
                self.write_json({"error": "not found"}, 404)
                return
            if asset["owner_user_id"] and asset["owner_user_id"] != auth["user_id"]:
                self.write_json({"error": "not found"}, 404)
                return
            content = asset["content"].encode()
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)
            return

        self.write_json({"error": "not found", "path": self.path}, 404)

    def do_POST(self):
        global server_counter, personal_counter, asset_counter

        auth = self.ensure_auth()
        if not auth:
            return

        if self.path == "/api/workbench/admin/assets/fetch-icon":
            if auth["role"] != "admin":
                self.write_json({"error": "forbidden"}, 403)
                return
            asset_id = f"admin-asset-{asset_counter}"
            asset_counter += 1
            assets[asset_id] = {
                "id": asset_id,
                "account_id": auth["account_id"],
                "owner_user_id": "",
                "content": "admin-icon",
            }
            self.write_json({"iconUrl": f"/api/workbench/assets/{asset_id}", "iconMode": "fetched"})
            return

        if self.path == "/api/workbench/personal/assets/fetch-icon":
            asset_id = f"personal-asset-{asset_counter}"
            asset_counter += 1
            assets[asset_id] = {
                "id": asset_id,
                "account_id": auth["account_id"],
                "owner_user_id": auth["user_id"],
                "content": "personal-icon",
            }
            self.write_json({"iconUrl": f"/api/workbench/assets/{asset_id}", "iconMode": "fetched"})
            return

        if self.path == "/api/workbench/admin/resources":
            if auth["role"] != "admin":
                self.write_json({"error": "forbidden"}, 403)
                return
            payload = self.read_json()
            resource_id = f"server-{server_counter}"
            server_counter += 1
            resource = {
                "id": resource_id,
                "scope": "server",
                "source": "admin",
                "name": payload.get("name", ""),
                "category": payload.get("category", ""),
                "description": payload.get("description", ""),
                "iconUrl": payload.get("iconUrl", ""),
                "iconMode": payload.get("iconMode", "letter"),
                "url": payload.get("url", ""),
                "tags": payload.get("tags", []),
                "enabled": payload.get("enabled", True),
                "favorite": payload.get("favorite", False),
                "sort": payload.get("sort", 0),
                "visibility": payload.get("visibility", "all"),
                "visibleGroups": payload.get("visibleGroups", []),
                "visibleUsers": payload.get("visibleUsers", []),
            }
            server_resources.append(resource)
            self.write_json(resource)
            return

        if self.path == "/api/workbench/personal/resources":
            payload = self.read_json()
            resource_id = f"personal-{personal_counter}"
            personal_counter += 1
            resource = {
                "id": resource_id,
                "userId": auth["user_id"],
                "scope": "personal",
                "source": "user",
                "name": payload.get("name", ""),
                "category": payload.get("category", ""),
                "description": payload.get("description", ""),
                "iconUrl": payload.get("iconUrl", ""),
                "iconMode": payload.get("iconMode", "letter"),
                "url": payload.get("url", ""),
                "tags": payload.get("tags", []),
                "enabled": payload.get("enabled", True),
                "favorite": payload.get("favorite", False),
                "sort": payload.get("sort", 0),
            }
            personal_resources.append(resource)
            self.write_json(resource)
            return

        launch_match = re.fullmatch(r"/api/workbench/resources/([^/]+)/launch", self.path)
        if launch_match:
            payload = self.read_json()
            resource_id = launch_match.group(1)
            scope = payload.get("scope", "")
            visible = False
            if scope == "server":
                visible = any(resource["id"] == resource_id and server_resource_visible(resource, auth) for resource in server_resources)
            elif scope == "personal":
                visible = any(
                    resource["id"] == resource_id and
                    resource.get("userId") == auth["user_id"] and
                    resource.get("enabled") is not False
                    for resource in personal_resources
                )
            if not visible:
                self.write_json({"error": "not found"}, 404)
                return
            recent_visits[(auth["user_id"], scope, resource_id)] = "2026-06-11T00:00:00Z"
            self.write_json({})
            return

        self.write_json({"error": "not found", "path": self.path}, 404)

    def do_PUT(self):
        auth = self.ensure_auth()
        if not auth:
            return

        admin_match = re.fullmatch(r"/api/workbench/admin/resources/([^/]+)", self.path)
        if admin_match:
            if auth["role"] != "admin":
                self.write_json({"error": "forbidden"}, 403)
                return
            payload = self.read_json()
            resource_id = admin_match.group(1)
            for resource in server_resources:
                if resource["id"] == resource_id:
                    resource.update({
                        "name": payload.get("name", resource["name"]),
                        "category": payload.get("category", resource["category"]),
                        "description": payload.get("description", resource["description"]),
                        "iconUrl": payload.get("iconUrl", resource["iconUrl"]),
                        "iconMode": payload.get("iconMode", resource["iconMode"]),
                        "url": payload.get("url", resource["url"]),
                        "tags": payload.get("tags", resource["tags"]),
                        "enabled": payload.get("enabled", resource["enabled"]),
                        "favorite": payload.get("favorite", resource["favorite"]),
                        "sort": payload.get("sort", resource["sort"]),
                        "visibility": payload.get("visibility", resource["visibility"]),
                        "visibleGroups": payload.get("visibleGroups", resource["visibleGroups"]),
                        "visibleUsers": payload.get("visibleUsers", resource["visibleUsers"]),
                    })
                    self.write_json(resource)
                    return
            self.write_json({"error": "not found"}, 404)
            return

        personal_match = re.fullmatch(r"/api/workbench/personal/resources/([^/]+)", self.path)
        if personal_match:
            payload = self.read_json()
            resource_id = personal_match.group(1)
            for resource in personal_resources:
                if resource["id"] == resource_id and resource["userId"] == auth["user_id"]:
                    resource.update({
                        "name": payload.get("name", resource["name"]),
                        "category": payload.get("category", resource["category"]),
                        "description": payload.get("description", resource["description"]),
                        "iconUrl": payload.get("iconUrl", resource["iconUrl"]),
                        "iconMode": payload.get("iconMode", resource["iconMode"]),
                        "url": payload.get("url", resource["url"]),
                        "tags": payload.get("tags", resource["tags"]),
                        "enabled": True,
                        "favorite": payload.get("favorite", resource["favorite"]),
                        "sort": payload.get("sort", resource["sort"]),
                    })
                    self.write_json(resource)
                    return
            self.write_json({"error": "not found"}, 404)
            return

        self.write_json({"error": "not found", "path": self.path}, 404)

    def do_DELETE(self):
        auth = self.ensure_auth()
        if not auth:
            return

        admin_match = re.fullmatch(r"/api/workbench/admin/resources/([^/]+)", self.path)
        if admin_match:
            if auth["role"] != "admin":
                self.write_json({"error": "forbidden"}, 403)
                return
            resource_id = admin_match.group(1)
            for index, resource in enumerate(server_resources):
                if resource["id"] == resource_id:
                    del server_resources[index]
                    self.write_json({})
                    return
            self.write_json({"error": "not found"}, 404)
            return

        personal_match = re.fullmatch(r"/api/workbench/personal/resources/([^/]+)", self.path)
        if personal_match:
            resource_id = personal_match.group(1)
            for index, resource in enumerate(personal_resources):
                if resource["id"] == resource_id and resource["userId"] == auth["user_id"]:
                    del personal_resources[index]
                    self.write_json({})
                    return
            self.write_json({"error": "not found"}, 404)
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
EVIDENCE_DIR="${WORKBENCH_EVIDENCE_DIR:-}"
KEEP_RESOURCES="${WORKBENCH_KEEP_RESOURCES:-0}"

output="$(
  WORKBENCH_BASE_URL="$BASE_URL" \
  WORKBENCH_ADMIN_TOKEN="admin-token" \
  WORKBENCH_USER_TOKEN="user-token" \
  WORKBENCH_OTHER_USER_TOKEN="other-user-token" \
  WORKBENCH_USER_VISIBLE_GROUP_ID="group-a" \
  WORKBENCH_USER_VISIBLE_USER_ID="user-1" \
  WORKBENCH_OTHER_VISIBLE_USER_ID="user-2" \
  "$ROOT_DIR/scripts/workbench-smoke.sh" \
    --server-url "https://server.example.test" \
    --personal-url "https://personal.example.test" \
    --icon-fetch-url "https://icons.example.test/app"
)"
printf '%s\n' "$output"

if ! grep -Fq "workbench smoke complete" <<<"$output"; then
  echo "workbench smoke did not complete" >&2
  exit 1
fi

if [[ "$KEEP_RESOURCES" == "1" && -n "$EVIDENCE_DIR" ]]; then
  cleanup_script="${EVIDENCE_DIR}/cleanup-kept-workbench-resources.sh"
  if [[ ! -x "$cleanup_script" ]]; then
    echo "expected cleanup script at ${cleanup_script}" >&2
    exit 1
  fi
  WORKBENCH_BASE_URL="$BASE_URL" \
  WORKBENCH_ADMIN_TOKEN="admin-token" \
  WORKBENCH_USER_TOKEN="user-token" \
  "$cleanup_script"

  admin_resources="$(
    curl -fsS \
      -H "Authorization: Bearer admin-token" \
      -- "${BASE_URL}/api/workbench/admin/resources"
  )"
  if [[ "$(printf '%s' "$admin_resources" | jq 'length')" != "0" ]]; then
    echo "cleanup script left server resources behind" >&2
    printf '%s\n' "$admin_resources" >&2
    exit 1
  fi

  user_resources="$(
    curl -fsS \
      -H "Authorization: Bearer user-token" \
      -- "${BASE_URL}/api/workbench/resources"
  )"
  if printf '%s' "$user_resources" | jq -e '.personalResources[]?' >/dev/null; then
    echo "cleanup script left personal resources behind" >&2
    printf '%s\n' "$user_resources" >&2
    exit 1
  fi
fi

if [[ -n "$EVIDENCE_DIR" ]]; then
  verify_script="${ROOT_DIR}/scripts/verify-workbench-e2e-report.sh"
  if ! grep -Fq "Manual GUI E2E Checklist" "${EVIDENCE_DIR}/e2e-report.md"; then
    echo "generated E2E report is missing the manual GUI checklist" >&2
    exit 1
  fi
  if ! grep -Fq "verify-workbench-e2e-report.sh" "${EVIDENCE_DIR}/e2e-report.md"; then
    echo "generated E2E report is missing the verification command" >&2
    exit 1
  fi
  if "$verify_script" "$EVIDENCE_DIR" >/tmp/workbench-e2e-verify-unexpected.log 2>&1; then
    echo "unfinished GUI E2E report unexpectedly passed verification" >&2
    cat /tmp/workbench-e2e-verify-unexpected.log >&2 || true
    exit 1
  fi
  rm -f /tmp/workbench-e2e-verify-unexpected.log

  for item in $(seq 1 11); do
    printf 'stub GUI evidence for checklist item %s\n' "$item" >"${EVIDENCE_DIR}/gui-item-${item}.txt"
  done
  cat >"${EVIDENCE_DIR}/e2e-report.md" <<'EOF'
# Workbench E2E Evidence Report

## Manual GUI E2E Checklist

| # | Scenario | Status | Evidence path | Notes |
| --- | --- | --- | --- | --- |
| 1 | Management starts and migrates workbench tables without removing old APIs | PASS | gui-item-1.txt |  |
| 2 | Dashboard admin creates all required server resources with icon evidence | PASS | gui-item-2.txt |  |
| 3 | User-a new client sees only expected server resources | PASS | gui-item-3.txt |  |
| 4 | User-a creates a personal resource from the new client | PASS | gui-item-4.txt |  |
| 5 | User-a uploads a personal icon and other user cannot read it | PASS | gui-item-5.txt |  |
| 6 | User-a edits personal resource fields and changes persist | PASS | gui-item-6.txt |  |
| 7 | User-a deletes personal resource without changing server resources | PASS | gui-item-7.txt |  |
| 8 | Resource clicks open the system default browser and invalid URL shows an error | PASS | gui-item-8.txt |  |
| 9 | User-b cannot see user-a personal resource or icon asset | PASS | gui-item-9.txt |  |
| 10 | Disabled server resource is hidden and cannot launch | PASS | gui-item-10.txt |  |
| 11 | Legacy client and networks resources API remain compatible | PASS | gui-item-11.txt |  |
EOF
  "$verify_script" "$EVIDENCE_DIR" >/dev/null
fi
