#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  WORKBENCH_BASE_URL=https://cloink.example.com \
  WORKBENCH_ADMIN_TOKEN=<admin-api-token> \
  WORKBENCH_USER_TOKEN=<user-api-token> \
  ./scripts/workbench-smoke.sh [--server-url <url>] [--personal-url <url>] [--icon-fetch-url <url>]

Checks a deployed workbench rollout through HTTP APIs.

Required environment:
  WORKBENCH_BASE_URL     Dashboard/management base URL, for example https://cloink.example.com
  WORKBENCH_ADMIN_TOKEN  Dashboard API token or bearer token with workbench admin access

Optional environment:
  WORKBENCH_USER_TOKEN        User token for personal workbench CRUD and client-side list checks.
                              Defaults to WORKBENCH_ADMIN_TOKEN.
  WORKBENCH_OTHER_USER_TOKEN  Optional second user token used to verify personal resource and
                              personal icon asset isolation.
  WORKBENCH_ADMIN_AUTH_HEADER Authorization header value for admin requests.
                              Defaults to "Bearer ${WORKBENCH_ADMIN_TOKEN}".
  WORKBENCH_USER_AUTH_HEADER  Authorization header value for user requests.
                              Defaults to "Bearer ${WORKBENCH_USER_TOKEN}".
  WORKBENCH_OTHER_USER_AUTH_HEADER
                              Authorization header value for second-user checks.
                              Defaults to "Bearer ${WORKBENCH_OTHER_USER_TOKEN}" when provided.
  WORKBENCH_EVIDENCE_DIR      Optional directory where JSON responses and status evidence are saved.
  WORKBENCH_USER_VISIBLE_GROUP_ID
                              Optional user-group ID visible to WORKBENCH_USER_TOKEN and not to
                              WORKBENCH_OTHER_USER_TOKEN. Enables restricted group visibility checks.
  WORKBENCH_USER_VISIBLE_USER_ID
                              Optional user ID for WORKBENCH_USER_TOKEN. Enables restricted user
                              visibility checks for the primary user.
  WORKBENCH_OTHER_VISIBLE_USER_ID
                              Optional user ID for WORKBENCH_OTHER_USER_TOKEN. Enables a negative
                              visibility check proving the primary user cannot see another user's
                              restricted server resource.
  WORKBENCH_KEEP_RESOURCES    Set to 1 to keep smoke-created server and personal resources for
                              manual GUI evidence collection. Defaults to deleting them.

Options:
  --server-url <url>      URL used when creating the smoke server resource
  --personal-url <url>    URL used when creating the smoke personal resource
  --icon-fetch-url <url>  Optional public HTTPS URL used to verify admin/personal fetch-icon flows
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

request() {
  local auth_header="$1"
  local method="$2"
  local path="$3"
  local data="${4:-}"
  local content_type="${5:-application/json}"
  local tmp_body tmp_code
  tmp_body="$(mktemp)"
  tmp_code="$(mktemp)"

  if [[ -n "$data" ]]; then
    curl -fsS -X "$method" \
      -H "Authorization: ${auth_header}" \
      -H "Content-Type: ${content_type}" \
      --data "$data" \
      -o "$tmp_body" \
      -w "%{http_code}" \
      -- "${BASE_URL}${path}" >"$tmp_code"
  else
    curl -fsS -X "$method" \
      -H "Authorization: ${auth_header}" \
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

save_evidence() {
  local name="$1"
  local content="$2"
  if [[ -z "${EVIDENCE_DIR:-}" ]]; then
    return
  fi
  local file="${EVIDENCE_DIR}/${name}"
  mkdir -p "$(dirname "$file")"
  if [[ "$name" == *.json ]]; then
    if printf '%s' "$content" | jq . >"$file" 2>/dev/null; then
      return
    fi
  fi
  printf '%s\n' "$content" >"$file"
}

save_status_evidence() {
  local name="$1"
  local method="$2"
  local path="$3"
  local code="$4"
  if [[ -z "${EVIDENCE_DIR:-}" ]]; then
    return
  fi
  jq -nc \
    --arg method "$method" \
    --arg path "$path" \
    --arg code "$code" \
    '{method: $method, path: $path, status: ($code | tonumber? // $code)}' \
    >"${EVIDENCE_DIR}/${name}"
}

write_evidence_summary() {
  if [[ -z "${EVIDENCE_DIR:-}" ]]; then
    return
  fi
  local evidence_files
  evidence_files="$(
    find "$EVIDENCE_DIR" -maxdepth 1 -type f -printf '%f\n' \
      | sort \
      | jq -R . \
      | jq -s .
  )"
  jq -n \
    --arg baseURL "$BASE_URL" \
    --arg completedAt "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
    --arg serverResourceID "${SERVER_RESOURCE_ID:-}" \
    --arg groupRestrictedServerResourceID "${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}" \
    --arg userRestrictedServerResourceID "${USER_RESTRICTED_SERVER_RESOURCE_ID:-}" \
    --arg otherRestrictedServerResourceID "${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}" \
    --arg disabledServerResourceID "${DISABLED_SERVER_RESOURCE_ID:-}" \
    --arg personalResourceID "${PERSONAL_RESOURCE_ID:-}" \
    --arg serverIconURL "$server_icon_url" \
    --arg personalIconURL "$personal_icon_url" \
    --arg userVisibleGroupID "$USER_VISIBLE_GROUP_ID" \
    --arg userVisibleUserID "$USER_VISIBLE_USER_ID" \
    --arg otherVisibleUserID "$OTHER_VISIBLE_USER_ID" \
    --arg hasSecondUser "$([[ -n "$OTHER_USER_AUTH_HEADER" ]] && echo true || echo false)" \
    --arg hasIconFetch "$([[ -n "$ICON_FETCH_URL" ]] && echo true || echo false)" \
    --arg keepResources "$KEEP_RESOURCES" \
    --argjson evidenceFiles "$evidence_files" \
    '{
      baseURL: $baseURL,
      completedAt: $completedAt,
      resources: {
        serverAllUsers: $serverResourceID,
        serverRestrictedGroup: $groupRestrictedServerResourceID,
        serverRestrictedUser: $userRestrictedServerResourceID,
        serverRestrictedOtherUser: $otherRestrictedServerResourceID,
        serverDisabled: $disabledServerResourceID,
        personal: $personalResourceID
      },
      assets: {
        serverIconURL: $serverIconURL,
        personalIconURL: $personalIconURL
      },
      optionalChecks: {
        secondUser: ($hasSecondUser == "true"),
        iconFetch: ($hasIconFetch == "true"),
        restrictedGroupVisibility: ($userVisibleGroupID != ""),
        restrictedUserVisibility: ($userVisibleUserID != ""),
        restrictedOtherUserVisibility: ($otherVisibleUserID != ""),
        keepResources: ($keepResources == "1")
      },
      restrictedInputs: {
        userVisibleGroupID: $userVisibleGroupID,
        userVisibleUserID: $userVisibleUserID,
        otherVisibleUserID: $otherVisibleUserID
      },
      e2eEvidenceCoverage: [
        {
          item: "2-3",
          area: "server resource visibility",
          evidence: ["03-created-server-resource.json", "05-user-resources-after-server-create.json", "05a-created-group-restricted-server-resource.json", "05b-user-resources-after-group-restricted-create.json", "05c-other-user-resources-after-group-restricted-create.json", "05d-created-user-restricted-server-resource.json", "05e-user-resources-after-user-restricted-create.json", "05g-created-other-user-restricted-server-resource.json", "05h-user-resources-after-other-user-restricted-create.json", "05i-other-user-resources-after-other-user-restricted-create.json"]
        },
        {
          item: "4-7",
          area: "personal resource CRUD and persistence",
          evidence: ["12-created-personal-resource.json", "13-user-resources-after-personal-create.json", "16-user-resources-after-personal-update.json", "17-user-resources-after-personal-delete.json"]
        },
        {
          item: "5,9",
          area: "personal icon asset isolation",
          evidence: ["11-personal-fetch-icon.json", "other-user-personal-asset-isolation-status.json"]
        },
        {
          item: "8,10",
          area: "launch audit, recent visits, disabled resource rejection",
          evidence: ["06-user-resources-after-server-launch.json", "14-user-resources-after-personal-launch.json", "09-user-resources-after-server-disable.json", "negative-request-POST-*_launch-status.json"]
        }
      ],
      manualGuiEvidenceRequired: [
        {
          item: "1",
          area: "management startup and database migration",
          requiredEvidence: ["management startup log", "database table list showing workbench tables"]
        },
        {
          item: "2",
          area: "dashboard server resource management UI",
          requiredEvidence: ["dashboard /workbench resource table screenshot", "resource edit dialog screenshot", "icon preview screenshot"]
        },
        {
          item: "3",
          area: "new client server resource visibility UI",
          requiredEvidence: ["user-a workbench screenshot", "category filter screenshot", "search result screenshot"]
        },
        {
          item: "4-7",
          area: "new client personal resource UI",
          requiredEvidence: ["create dialog screenshot", "saved personal card screenshot", "edit before/after screenshots", "delete before/after screenshots"]
        },
        {
          item: "8",
          area: "default browser launch",
          requiredEvidence: ["browser address bar screenshot", "client invalid URL error screenshot"]
        },
        {
          item: "9",
          area: "second-user client isolation UI",
          requiredEvidence: ["user-b workbench screenshot", "personal asset read failure status"]
        },
        {
          item: "10",
          area: "disabled server resource UI",
          requiredEvidence: ["dashboard disabled resource screenshot", "client list/category after disable screenshot"]
        },
        {
          item: "11",
          area: "legacy client and legacy network resource compatibility",
          requiredEvidence: ["legacy client connect/disconnect log", "/api/networks/resources response sample"]
        }
      ],
      fullGuiE2EComplete: false,
      cleanup: {
        smokeResourcesKept: ($keepResources == "1"),
        note: (if $keepResources == "1" then "Smoke-created resources were intentionally kept for manual GUI evidence collection. Delete them after E2E capture." else "Smoke-created resources were deleted by the script after verification." end)
      },
      evidenceFiles: $evidenceFiles
    }' >"${EVIDENCE_DIR}/summary.json"
}

write_e2e_report_template() {
  if [[ -z "${EVIDENCE_DIR:-}" ]]; then
    return
  fi
  cat >"${EVIDENCE_DIR}/e2e-report.md" <<EOF
# Workbench E2E Evidence Report

- Base URL: \`${BASE_URL}\`
- Completed at: \`$(date -u +"%Y-%m-%dT%H:%M:%SZ")\`
- HTTP smoke summary: \`summary.json\`
- Full GUI E2E complete: **No**
- Smoke resources kept for GUI capture: **$([[ "$KEEP_RESOURCES" == "1" ]] && echo Yes || echo No)**

## Smoke-Created Resource IDs

| Resource | ID |
| --- | --- |
| Server resource visible to all users | \`${SERVER_RESOURCE_ID:-}\` |
| Server resource restricted to primary user's group | \`${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}\` |
| Server resource restricted to primary user | \`${USER_RESTRICTED_SERVER_RESOURCE_ID:-}\` |
| Server resource restricted to second user | \`${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}\` |
| Disabled server resource | \`${DISABLED_SERVER_RESOURCE_ID:-}\` |
| Personal resource | \`${PERSONAL_RESOURCE_ID:-}\` |
| Server icon URL | \`${server_icon_url:-}\` |
| Personal icon URL | \`${personal_icon_url:-}\` |

$(if [[ "$KEEP_RESOURCES" == "1" ]]; then cat <<'KEEP_NOTE'
> Smoke-created resources were intentionally kept so dashboard and new-client screenshots can be captured. Delete these smoke resources after GUI evidence is collected.

### Cleanup After GUI Capture

Run the generated cleanup helper from this evidence directory after screenshots and logs are archived:

~~~bash
WORKBENCH_BASE_URL=<management-base-url> \
WORKBENCH_ADMIN_TOKEN=<admin-token> \
WORKBENCH_USER_TOKEN=<user-token> \
./cleanup-kept-workbench-resources.sh
~~~

You can also provide `WORKBENCH_ADMIN_AUTH_HEADER` or `WORKBENCH_USER_AUTH_HEADER` directly when bearer tokens are not appropriate.
KEEP_NOTE
else cat <<'DELETE_NOTE'
> Smoke-created resources were deleted after HTTP verification. Re-run with `WORKBENCH_KEEP_RESOURCES=1` when you need the same resources to remain available for GUI evidence capture.
DELETE_NOTE
fi)

## HTTP Evidence Generated

- Server resource visibility: \`03-created-server-resource.json\`, \`05-user-resources-after-server-create.json\`, \`05a\`-\`05i\` restricted visibility files when restricted inputs are provided.
- Personal resource CRUD: \`12-created-personal-resource.json\`, \`13-user-resources-after-personal-create.json\`, \`16-user-resources-after-personal-update.json\`, \`17-user-resources-after-personal-delete.json\`.
- Launch audit and recent visits: \`06-user-resources-after-server-launch.json\`, \`14-user-resources-after-personal-launch.json\`.
- Disabled resource rejection: \`09-user-resources-after-server-disable.json\`, \`negative-request-POST-*_launch-status.json\`.
- Personal icon isolation: \`11-personal-fetch-icon.json\`, \`other-user-personal-asset-isolation-status.json\`.

## Manual GUI E2E Checklist

| # | Scenario | Status | Evidence path | Notes |
| --- | --- | --- | --- | --- |
| 1 | Management starts and migrates \`workbench_*\` tables without removing old APIs | TODO |  |  |
| 2 | Dashboard admin creates all-users, group-restricted, user-a-restricted, and user-b/other-restricted server resources with icon evidence | TODO |  |  |
| 3 | User-a new client sees only expected server resources; server resources are read-only; category/search work | TODO |  |  |
| 4 | User-a creates a personal resource from the new client and icon fetch or letter fallback works | TODO |  |  |
| 5 | User-a uploads a personal icon; refresh/restart still displays protected asset; other user cannot read it | TODO |  |  |
| 6 | User-a edits personal resource name/category/tags/URL/sort/favorite; refresh/restart persists changes | TODO |  |  |
| 7 | User-a deletes personal resource; only current user's personal resource disappears; server resources remain | TODO |  |  |
| 8 | Server and personal resource clicks open the system default browser; invalid/blank URL shows client error | TODO |  |  |
| 9 | User-b new client does not see user-a personal resource and cannot read user-a personal icon asset | TODO |  |  |
| 10 | Dashboard disables a server resource; new client no longer shows it or its empty category and cannot launch it | TODO |  |  |
| 11 | Legacy client login/connect/disconnect/exit and \`/api/networks/resources\` remain compatible | TODO |  |  |

## Completion Rule

Mark the workbench E2E complete only after every row above is \`PASS\` and the referenced screenshots/logs/API samples are present in this evidence directory or a linked artifact store.

After filling the checklist, run this from the \`netbird\` repository root:

~~~bash
./scripts/verify-workbench-e2e-report.sh "${EVIDENCE_DIR}"
~~~
EOF
}

write_kept_resources_cleanup_script() {
  if [[ -z "${EVIDENCE_DIR:-}" || "$KEEP_RESOURCES" != "1" ]]; then
    return
  fi
  cat >"${EVIDENCE_DIR}/cleanup-kept-workbench-resources.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail

BASE_URL="\${WORKBENCH_BASE_URL:-${BASE_URL}}"
ADMIN_AUTH_HEADER="\${WORKBENCH_ADMIN_AUTH_HEADER:-Bearer \${WORKBENCH_ADMIN_TOKEN:-}}"
USER_AUTH_HEADER="\${WORKBENCH_USER_AUTH_HEADER:-Bearer \${WORKBENCH_USER_TOKEN:-\${WORKBENCH_ADMIN_TOKEN:-}}}"

if [[ -z "\$BASE_URL" || "\$ADMIN_AUTH_HEADER" == "Bearer " || "\$USER_AUTH_HEADER" == "Bearer " ]]; then
  echo "Set WORKBENCH_BASE_URL, WORKBENCH_ADMIN_TOKEN and WORKBENCH_USER_TOKEN, or explicit *_AUTH_HEADER variables." >&2
  exit 1
fi
for cmd in curl jq; do
  if ! command -v "\$cmd" >/dev/null 2>&1; then
    echo "missing required command: \$cmd" >&2
    exit 1
  fi
done
BASE_URL="\${BASE_URL%/}"
SERVER_RESOURCE_IDS=(
  "${DISABLED_SERVER_RESOURCE_ID:-}"
  "${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}"
  "${USER_RESTRICTED_SERVER_RESOURCE_ID:-}"
  "${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}"
  "${SERVER_RESOURCE_ID:-}"
)
PERSONAL_RESOURCE_IDS=(
  "${PERSONAL_RESOURCE_ID:-}"
)

url_encode() {
  jq -nr --arg value "\$1" '\$value | @uri'
}

delete_request() {
  local auth_header="\$1"
  local path="\$2"
  local label="\$3"
  local tmp_body tmp_code
  tmp_body="\$(mktemp)"
  tmp_code="\$(mktemp)"
  curl -sS -X DELETE \
    -H "Authorization: \${auth_header}" \
    -o "\$tmp_body" \
    -w "%{http_code}" \
    -- "\${BASE_URL}\${path}" >"\$tmp_code" || true
  local code
  code="\$(cat "\$tmp_code")"
  if [[ "\$code" != "200" && "\$code" != "204" && "\$code" != "404" ]]; then
    echo "Failed to delete \${label}: HTTP \${code}" >&2
    cat "\$tmp_body" >&2 || true
    rm -f "\$tmp_body" "\$tmp_code"
    exit 1
  fi
  rm -f "\$tmp_body" "\$tmp_code"
}

get_json() {
  local auth_header="\$1"
  local path="\$2"
  curl -fsS -H "Authorization: \${auth_header}" -- "\${BASE_URL}\${path}"
}

delete_admin_resource() {
  local id="\$1"
  [[ -z "\$id" ]] && return
  local encoded
  encoded="\$(url_encode "\$id")"
  echo "Deleting server resource \$id"
  delete_request "\$ADMIN_AUTH_HEADER" "/api/workbench/admin/resources/\${encoded}" "server resource \$id"
}

delete_personal_resource() {
  local id="\$1"
  [[ -z "\$id" ]] && return
  local encoded
  encoded="\$(url_encode "\$id")"
  echo "Deleting personal resource \$id"
  delete_request "\$USER_AUTH_HEADER" "/api/workbench/personal/resources/\${encoded}" "personal resource \$id"
}

for id in "\${SERVER_RESOURCE_IDS[@]}"; do
  delete_admin_resource "\$id"
done
for id in "\${PERSONAL_RESOURCE_IDS[@]}"; do
  delete_personal_resource "\$id"
done

admin_resources="\$(get_json "\$ADMIN_AUTH_HEADER" "/api/workbench/admin/resources")"
for id in "\${SERVER_RESOURCE_IDS[@]}"; do
  [[ -z "\$id" ]] && continue
  if printf '%s' "\$admin_resources" | jq -e --arg id "\$id" '.[]? | select(.id == \$id)' >/dev/null; then
    echo "Server resource \$id still exists after cleanup" >&2
    exit 1
  fi
done

user_resources="\$(get_json "\$USER_AUTH_HEADER" "/api/workbench/resources")"
for id in "\${PERSONAL_RESOURCE_IDS[@]}"; do
  [[ -z "\$id" ]] && continue
  if printf '%s' "\$user_resources" | jq -e --arg id "\$id" '.personalResources[]? | select(.id == \$id)' >/dev/null; then
    echo "Personal resource \$id still exists after cleanup" >&2
    exit 1
  fi
done

echo "kept workbench smoke resources cleanup complete"
EOF
  chmod +x "${EVIDENCE_DIR}/cleanup-kept-workbench-resources.sh"
}

assert_asset_fetchable() {
  local auth_header="$1"
  local asset_url="$2"
  local tmp_headers
  tmp_headers="$(mktemp)"
  curl -fsS \
    -H "Authorization: ${auth_header}" \
    -D "$tmp_headers" \
    -o /dev/null \
    -- "${BASE_URL}${asset_url}"
  local content_type
  content_type="$(awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {print $2}' "$tmp_headers" | tr -d '\r' | tail -n 1)"
  rm -f "$tmp_headers"
  if [[ "${content_type}" != image/* ]]; then
    echo "asset ${asset_url} returned unexpected content type: ${content_type}" >&2
    exit 1
  fi
}

assert_asset_not_fetchable() {
  local auth_header="$1"
  local asset_url="$2"
  local tmp_body tmp_code
  tmp_body="$(mktemp)"
  tmp_code="$(mktemp)"
  curl -sS \
    -H "Authorization: ${auth_header}" \
    -o "$tmp_body" \
    -w "%{http_code}" \
    -- "${BASE_URL}${asset_url}" >"$tmp_code" || true
  local code
  code="$(cat "$tmp_code")"
  rm -f "$tmp_body" "$tmp_code"
  save_status_evidence "other-user-personal-asset-isolation-status.json" GET "$asset_url" "$code"
  if [[ "$code" -ge 200 && "$code" -lt 300 ]]; then
    echo "asset ${asset_url} was unexpectedly fetchable by another user" >&2
    exit 1
  fi
  if [[ "$code" != "403" && "$code" != "404" ]]; then
    echo "asset ${asset_url} returned unexpected isolation status: HTTP ${code}" >&2
    exit 1
  fi
}

assert_request_not_successful() {
  local auth_header="$1"
  local method="$2"
  local path="$3"
  local data="${4:-}"
  local tmp_body tmp_code
  tmp_body="$(mktemp)"
  tmp_code="$(mktemp)"
  if [[ -n "$data" ]]; then
    curl -sS -X "$method" \
      -H "Authorization: ${auth_header}" \
      -H "Content-Type: application/json" \
      --data "$data" \
      -o "$tmp_body" \
      -w "%{http_code}" \
      -- "${BASE_URL}${path}" >"$tmp_code" || true
  else
    curl -sS -X "$method" \
      -H "Authorization: ${auth_header}" \
      -o "$tmp_body" \
      -w "%{http_code}" \
      -- "${BASE_URL}${path}" >"$tmp_code" || true
  fi
  local code
  code="$(cat "$tmp_code")"
  local response_body
  response_body="$(cat "$tmp_body" 2>/dev/null || true)"
  rm -f "$tmp_body" "$tmp_code"
  save_status_evidence "negative-request-${method}-${path//\//_}-status.json" "$method" "$path" "$code"
  if [[ -n "$response_body" ]]; then
    save_evidence "negative-request-${method}-${path//\//_}-body.json" "$response_body"
  fi
  if [[ "$code" -ge 200 && "$code" -lt 300 ]]; then
    echo "request unexpectedly succeeded: ${method} ${path}" >&2
    exit 1
  fi
}

resource_present() {
  local resources_json="$1"
  local collection="$2"
  local resource_id="$3"
  printf '%s' "$resources_json" | jq -e --arg id "$resource_id" ".${collection}[]? | select(.id == \$id)" >/dev/null
}

assert_resource_present() {
  local resources_json="$1"
  local collection="$2"
  local resource_id="$3"
  local label="$4"
  if ! resource_present "$resources_json" "$collection" "$resource_id"; then
    echo "${label} ${resource_id} not found in ${collection}" >&2
    exit 1
  fi
}

assert_resource_absent() {
  local resources_json="$1"
  local collection="$2"
  local resource_id="$3"
  local label="$4"
  if resource_present "$resources_json" "$collection" "$resource_id"; then
    echo "${label} ${resource_id} was unexpectedly present in ${collection}" >&2
    exit 1
  fi
}

resource_has_recent_visit() {
  local resources_json="$1"
  local collection="$2"
  local resource_id="$3"
  printf '%s' "$resources_json" | jq -e \
    --arg collection "$collection" \
    --arg id "$resource_id" \
    '.[$collection][]? | select(.id == $id) | .metadata.recentVisitedAt | strings | select(length > 0)' >/dev/null
}

category_present() {
  local resources_json="$1"
  local category_name="$2"
  printf '%s' "$resources_json" | jq -e --arg name "$category_name" '.categories[]? | select(.name == $name)' >/dev/null
}

resource_enabled() {
  local resources_json="$1"
  local collection="$2"
  local resource_id="$3"
  printf '%s' "$resources_json" | jq -e \
    --arg collection "$collection" \
    --arg id "$resource_id" \
    '.[$collection][]? | select(.id == $id) | select(.enabled != false)' >/dev/null
}

url_encode() {
  jq -nr --arg value "$1" '$value | @uri'
}

cleanup() {
  if [[ "${KEEP_RESOURCES:-0}" == "1" ]]; then
    return
  fi
  if [[ "${SMOKE_CLEANED:-0}" == "1" ]]; then
    return
  fi
  if [[ -n "${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    local other_restricted_server_resource_id
    other_restricted_server_resource_id="$(url_encode "$OTHER_RESTRICTED_SERVER_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${ADMIN_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/admin/resources/${other_restricted_server_resource_id}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${USER_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    local user_restricted_server_resource_id
    user_restricted_server_resource_id="$(url_encode "$USER_RESTRICTED_SERVER_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${ADMIN_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/admin/resources/${user_restricted_server_resource_id}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    local group_restricted_server_resource_id
    group_restricted_server_resource_id="$(url_encode "$GROUP_RESTRICTED_SERVER_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${ADMIN_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/admin/resources/${group_restricted_server_resource_id}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${DISABLED_SERVER_RESOURCE_ID:-}" ]]; then
    local disabled_server_resource_id
    disabled_server_resource_id="$(url_encode "$DISABLED_SERVER_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${ADMIN_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/admin/resources/${disabled_server_resource_id}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${PERSONAL_RESOURCE_ID:-}" ]]; then
    local personal_resource_id
    personal_resource_id="$(url_encode "$PERSONAL_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${USER_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/personal/resources/${personal_resource_id}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${SERVER_RESOURCE_ID:-}" ]]; then
    local server_resource_id
    server_resource_id="$(url_encode "$SERVER_RESOURCE_ID")"
    curl -fsS -X DELETE \
      -H "Authorization: ${ADMIN_AUTH_HEADER}" \
      -o /dev/null \
      -- "${BASE_URL}/api/workbench/admin/resources/${server_resource_id}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd curl
require_cmd jq

KEEP_RESOURCES="${WORKBENCH_KEEP_RESOURCES:-0}"
if [[ "$KEEP_RESOURCES" != "0" && "$KEEP_RESOURCES" != "1" ]]; then
  echo "WORKBENCH_KEEP_RESOURCES must be 0 or 1" >&2
  exit 1
fi
BASE_URL="${WORKBENCH_BASE_URL:-}"
WORKBENCH_ADMIN_TOKEN="${WORKBENCH_ADMIN_TOKEN:-}"
WORKBENCH_USER_TOKEN="${WORKBENCH_USER_TOKEN:-${WORKBENCH_ADMIN_TOKEN:-}}"
WORKBENCH_OTHER_USER_TOKEN="${WORKBENCH_OTHER_USER_TOKEN:-}"
if [[ -z "$BASE_URL" || -z "$WORKBENCH_ADMIN_TOKEN" ]]; then
  usage >&2
  exit 1
fi
BASE_URL="${BASE_URL%/}"
ADMIN_AUTH_HEADER="${WORKBENCH_ADMIN_AUTH_HEADER:-Bearer ${WORKBENCH_ADMIN_TOKEN}}"
USER_AUTH_HEADER="${WORKBENCH_USER_AUTH_HEADER:-Bearer ${WORKBENCH_USER_TOKEN}}"
OTHER_USER_AUTH_HEADER=""
if [[ -n "$WORKBENCH_OTHER_USER_TOKEN" ]]; then
  OTHER_USER_AUTH_HEADER="${WORKBENCH_OTHER_USER_AUTH_HEADER:-Bearer ${WORKBENCH_OTHER_USER_TOKEN}}"
fi
USER_VISIBLE_GROUP_ID="${WORKBENCH_USER_VISIBLE_GROUP_ID:-}"
USER_VISIBLE_USER_ID="${WORKBENCH_USER_VISIBLE_USER_ID:-}"
OTHER_VISIBLE_USER_ID="${WORKBENCH_OTHER_VISIBLE_USER_ID:-}"
EVIDENCE_DIR="${WORKBENCH_EVIDENCE_DIR:-}"
if [[ -n "$EVIDENCE_DIR" ]]; then
  mkdir -p "$EVIDENCE_DIR"
  jq -nc \
    --arg baseURL "$BASE_URL" \
    --arg startedAt "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
    '{baseURL: $baseURL, startedAt: $startedAt}' \
    >"${EVIDENCE_DIR}/run.json"
fi

SERVER_URL="https://example.com"
PERSONAL_URL="https://example.com/personal"
ICON_FETCH_URL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-url)
      SERVER_URL="${2:-}"
      shift 2
      ;;
    --personal-url)
      PERSONAL_URL="${2:-}"
      shift 2
      ;;
    --icon-fetch-url)
      ICON_FETCH_URL="${2:-}"
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

timestamp="$(date +%s)"
server_name="workbench-smoke-server-${timestamp}"
group_restricted_server_name="workbench-smoke-group-${timestamp}"
user_restricted_server_name="workbench-smoke-user-${timestamp}"
other_restricted_server_name="workbench-smoke-other-user-${timestamp}"
personal_name="workbench-smoke-personal-${timestamp}"
server_icon_url=""
server_icon_mode="letter"
personal_icon_url=""
personal_icon_mode="letter"

echo "==> Checking admin workbench list"
admin_resources="$(request "$ADMIN_AUTH_HEADER" GET "/api/workbench/admin/resources")"
printf '%s' "$admin_resources" | jq 'length' >/dev/null
save_evidence "01-admin-resources-before.json" "$admin_resources"

if [[ -n "$ICON_FETCH_URL" ]]; then
  echo "==> Checking admin fetch-icon"
  admin_icon_payload="$(jq -nc --arg url "$ICON_FETCH_URL" '{url: $url}')"
  admin_icon_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/assets/fetch-icon" "$admin_icon_payload")"
  server_icon_url="$(printf '%s' "$admin_icon_response" | jq -r '.iconUrl // ""')"
  server_icon_mode="$(printf '%s' "$admin_icon_response" | jq -r '.iconMode // "letter"')"
  if [[ -z "$server_icon_url" ]]; then
    echo "admin fetch-icon did not return iconUrl" >&2
    exit 1
  fi
  save_evidence "02-admin-fetch-icon.json" "$admin_icon_response"
  assert_asset_fetchable "$ADMIN_AUTH_HEADER" "$server_icon_url"
fi

echo "==> Creating smoke server resource"
server_payload="$(
  jq -nc \
    --arg name "$server_name" \
    --arg category "Smoke" \
    --arg description "Workbench smoke server resource" \
    --arg icon_url "$server_icon_url" \
    --arg icon_mode "$server_icon_mode" \
    --arg url "$SERVER_URL" \
    '{
      name: $name,
      category: $category,
      description: $description,
      iconUrl: $icon_url,
      iconMode: $icon_mode,
      url: $url,
      tags: ["smoke", "server"],
      enabled: true,
      favorite: false,
      sort: 10,
      visibility: "all"
    }'
)"
server_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/resources" "$server_payload")"
SERVER_RESOURCE_ID="$(printf '%s' "$server_response" | jq -r '.id // ""')"
if [[ -z "$SERVER_RESOURCE_ID" ]]; then
  echo "server resource creation did not return an id" >&2
  exit 1
fi
save_evidence "03-created-server-resource.json" "$server_response"

echo "==> Verifying smoke server resource in admin and client lists"
admin_resources="$(request "$ADMIN_AUTH_HEADER" GET "/api/workbench/admin/resources")"
save_evidence "04-admin-resources-after-create.json" "$admin_resources"
if ! resource_present "$admin_resources" "" "$SERVER_RESOURCE_ID"; then
  if ! printf '%s' "$admin_resources" | jq -e --arg id "$SERVER_RESOURCE_ID" '.[]? | select(.id == $id)' >/dev/null; then
    echo "server resource ${SERVER_RESOURCE_ID} not found in admin list" >&2
    exit 1
  fi
fi
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "05-user-resources-after-server-create.json" "$client_resources"
if ! resource_present "$client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
  echo "server resource ${SERVER_RESOURCE_ID} not found in client list" >&2
  exit 1
fi

if [[ -n "$USER_VISIBLE_GROUP_ID" ]]; then
  if [[ -z "$OTHER_USER_AUTH_HEADER" ]]; then
    echo "WORKBENCH_USER_VISIBLE_GROUP_ID requires WORKBENCH_OTHER_USER_TOKEN for negative visibility checks" >&2
    exit 1
  fi
  echo "==> Creating smoke group-restricted server resource"
  group_restricted_payload="$(
    jq -nc \
      --arg name "$group_restricted_server_name" \
      --arg category "Smoke Restricted" \
      --arg description "Workbench smoke group-restricted server resource" \
      --arg url "$SERVER_URL" \
      --arg group_id "$USER_VISIBLE_GROUP_ID" \
      '{
        name: $name,
        category: $category,
        description: $description,
        iconUrl: "",
        iconMode: "letter",
        url: $url,
        tags: ["smoke", "restricted", "group"],
        enabled: true,
        favorite: false,
        sort: 11,
        visibility: "restricted",
        visibleGroups: [$group_id],
        visibleUsers: []
      }'
  )"
  group_restricted_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/resources" "$group_restricted_payload")"
  GROUP_RESTRICTED_SERVER_RESOURCE_ID="$(printf '%s' "$group_restricted_response" | jq -r '.id // ""')"
  if [[ -z "$GROUP_RESTRICTED_SERVER_RESOURCE_ID" ]]; then
    echo "group-restricted server resource creation did not return an id" >&2
    exit 1
  fi
  save_evidence "05a-created-group-restricted-server-resource.json" "$group_restricted_response"
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "05b-user-resources-after-group-restricted-create.json" "$client_resources"
  assert_resource_present "$client_resources" "serverResources" "$GROUP_RESTRICTED_SERVER_RESOURCE_ID" "group-restricted server resource"
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "05c-other-user-resources-after-group-restricted-create.json" "$other_client_resources"
  assert_resource_absent "$other_client_resources" "serverResources" "$GROUP_RESTRICTED_SERVER_RESOURCE_ID" "group-restricted server resource"
fi

if [[ -n "$USER_VISIBLE_USER_ID" ]]; then
  echo "==> Creating smoke user-restricted server resource"
  user_restricted_payload="$(
    jq -nc \
      --arg name "$user_restricted_server_name" \
      --arg category "Smoke Restricted" \
      --arg description "Workbench smoke user-restricted server resource" \
      --arg url "$SERVER_URL" \
      --arg user_id "$USER_VISIBLE_USER_ID" \
      '{
        name: $name,
        category: $category,
        description: $description,
        iconUrl: "",
        iconMode: "letter",
        url: $url,
        tags: ["smoke", "restricted", "user"],
        enabled: true,
        favorite: false,
        sort: 12,
        visibility: "restricted",
        visibleGroups: [],
        visibleUsers: [$user_id]
      }'
  )"
  user_restricted_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/resources" "$user_restricted_payload")"
  USER_RESTRICTED_SERVER_RESOURCE_ID="$(printf '%s' "$user_restricted_response" | jq -r '.id // ""')"
  if [[ -z "$USER_RESTRICTED_SERVER_RESOURCE_ID" ]]; then
    echo "user-restricted server resource creation did not return an id" >&2
    exit 1
  fi
  save_evidence "05d-created-user-restricted-server-resource.json" "$user_restricted_response"
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "05e-user-resources-after-user-restricted-create.json" "$client_resources"
  assert_resource_present "$client_resources" "serverResources" "$USER_RESTRICTED_SERVER_RESOURCE_ID" "user-restricted server resource"
  if [[ -n "$OTHER_USER_AUTH_HEADER" ]]; then
    other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
    save_evidence "05f-other-user-resources-after-user-restricted-create.json" "$other_client_resources"
    assert_resource_absent "$other_client_resources" "serverResources" "$USER_RESTRICTED_SERVER_RESOURCE_ID" "user-restricted server resource"
  fi
fi

if [[ -n "$OTHER_VISIBLE_USER_ID" ]]; then
  if [[ -z "$OTHER_USER_AUTH_HEADER" ]]; then
    echo "WORKBENCH_OTHER_VISIBLE_USER_ID requires WORKBENCH_OTHER_USER_TOKEN" >&2
    exit 1
  fi
  echo "==> Creating smoke other-user-restricted server resource"
  other_restricted_payload="$(
    jq -nc \
      --arg name "$other_restricted_server_name" \
      --arg category "Smoke Restricted" \
      --arg description "Workbench smoke other-user-restricted server resource" \
      --arg url "$SERVER_URL" \
      --arg user_id "$OTHER_VISIBLE_USER_ID" \
      '{
        name: $name,
        category: $category,
        description: $description,
        iconUrl: "",
        iconMode: "letter",
        url: $url,
        tags: ["smoke", "restricted", "other-user"],
        enabled: true,
        favorite: false,
        sort: 13,
        visibility: "restricted",
        visibleGroups: [],
        visibleUsers: [$user_id]
      }'
  )"
  other_restricted_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/resources" "$other_restricted_payload")"
  OTHER_RESTRICTED_SERVER_RESOURCE_ID="$(printf '%s' "$other_restricted_response" | jq -r '.id // ""')"
  if [[ -z "$OTHER_RESTRICTED_SERVER_RESOURCE_ID" ]]; then
    echo "other-user-restricted server resource creation did not return an id" >&2
    exit 1
  fi
  save_evidence "05g-created-other-user-restricted-server-resource.json" "$other_restricted_response"
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "05h-user-resources-after-other-user-restricted-create.json" "$client_resources"
  assert_resource_absent "$client_resources" "serverResources" "$OTHER_RESTRICTED_SERVER_RESOURCE_ID" "other-user-restricted server resource"
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "05i-other-user-resources-after-other-user-restricted-create.json" "$other_client_resources"
  assert_resource_present "$other_client_resources" "serverResources" "$OTHER_RESTRICTED_SERVER_RESOURCE_ID" "other-user-restricted server resource"
fi

echo "==> Recording smoke server resource launch"
SERVER_RESOURCE_PATH_ID="$(url_encode "$SERVER_RESOURCE_ID")"
server_launch_payload="$(jq -nc '{scope: "server"}')"
request "$USER_AUTH_HEADER" POST "/api/workbench/resources/${SERVER_RESOURCE_PATH_ID}/launch" "$server_launch_payload" >/dev/null
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "06-user-resources-after-server-launch.json" "$client_resources"
if ! resource_has_recent_visit "$client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
  echo "server resource ${SERVER_RESOURCE_ID} did not return metadata.recentVisitedAt after launch" >&2
  exit 1
fi

if [[ -n "$OTHER_USER_AUTH_HEADER" ]]; then
  echo "==> Verifying server recent visit is scoped per user"
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "07-other-user-resources-before-server-launch.json" "$other_client_resources"
  if ! resource_present "$other_client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
    echo "server resource ${SERVER_RESOURCE_ID} not found in second user client list" >&2
    exit 1
  fi
  if resource_has_recent_visit "$other_client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
    echo "server resource ${SERVER_RESOURCE_ID} recent visit leaked to another user" >&2
    exit 1
  fi
  request "$OTHER_USER_AUTH_HEADER" POST "/api/workbench/resources/${SERVER_RESOURCE_PATH_ID}/launch" "$server_launch_payload" >/dev/null
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "08-other-user-resources-after-server-launch.json" "$other_client_resources"
  if ! resource_has_recent_visit "$other_client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
    echo "server resource ${SERVER_RESOURCE_ID} did not return second user's metadata.recentVisitedAt after launch" >&2
    exit 1
  fi
fi

echo "==> Disabling smoke server resource"
disabled_server_name="${server_name}-disabled"
disabled_server_payload="$(
  jq -nc \
    --arg name "$disabled_server_name" \
    --arg category "Smoke Disabled" \
    --arg description "Workbench smoke disabled server resource" \
    --arg icon_url "$server_icon_url" \
    --arg icon_mode "$server_icon_mode" \
    --arg url "$SERVER_URL" \
    '{
      name: $name,
      category: $category,
      description: $description,
      iconUrl: $icon_url,
      iconMode: $icon_mode,
      url: $url,
      tags: ["smoke", "disabled"],
      enabled: false,
      favorite: false,
      sort: 10,
      visibility: "all"
    }'
)"
if [[ "$KEEP_RESOURCES" == "1" ]]; then
  disabled_server_response="$(request "$ADMIN_AUTH_HEADER" POST "/api/workbench/admin/resources" "$disabled_server_payload")"
  DISABLED_SERVER_RESOURCE_ID="$(printf '%s' "$disabled_server_response" | jq -r '.id // ""')"
  if [[ -z "$DISABLED_SERVER_RESOURCE_ID" ]]; then
    echo "disabled server resource creation did not return an id" >&2
    exit 1
  fi
  save_evidence "09a-created-disabled-server-resource.json" "$disabled_server_response"
else
  request "$ADMIN_AUTH_HEADER" PUT "/api/workbench/admin/resources/${SERVER_RESOURCE_PATH_ID}" "$disabled_server_payload" >/dev/null
  DISABLED_SERVER_RESOURCE_ID="$SERVER_RESOURCE_ID"
fi
DISABLED_SERVER_RESOURCE_PATH_ID="$(url_encode "$DISABLED_SERVER_RESOURCE_ID")"
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "09-user-resources-after-server-disable.json" "$client_resources"
if resource_present "$client_resources" "serverResources" "$DISABLED_SERVER_RESOURCE_ID"; then
  echo "disabled server resource ${DISABLED_SERVER_RESOURCE_ID} still present in client list" >&2
  exit 1
fi
if category_present "$client_resources" "Smoke Disabled"; then
  echo "disabled server resource category was unexpectedly returned in client categories" >&2
  exit 1
fi
assert_request_not_successful "$USER_AUTH_HEADER" POST "/api/workbench/resources/${DISABLED_SERVER_RESOURCE_PATH_ID}/launch" "$server_launch_payload"
if [[ -n "$OTHER_USER_AUTH_HEADER" ]]; then
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "10-other-user-resources-after-server-disable.json" "$other_client_resources"
  if resource_present "$other_client_resources" "serverResources" "$DISABLED_SERVER_RESOURCE_ID"; then
    echo "disabled server resource ${DISABLED_SERVER_RESOURCE_ID} still present in second user client list" >&2
    exit 1
  fi
  if category_present "$other_client_resources" "Smoke Disabled"; then
    echo "disabled server resource category was unexpectedly returned in second user client categories" >&2
    exit 1
  fi
fi

if [[ -n "$ICON_FETCH_URL" ]]; then
  echo "==> Checking personal fetch-icon"
  personal_icon_payload="$(jq -nc --arg url "$ICON_FETCH_URL" '{url: $url}')"
  personal_icon_response="$(request "$USER_AUTH_HEADER" POST "/api/workbench/personal/assets/fetch-icon" "$personal_icon_payload")"
  personal_icon_url="$(printf '%s' "$personal_icon_response" | jq -r '.iconUrl // ""')"
  personal_icon_mode="$(printf '%s' "$personal_icon_response" | jq -r '.iconMode // "letter"')"
  if [[ -z "$personal_icon_url" ]]; then
    echo "personal fetch-icon did not return iconUrl" >&2
    exit 1
  fi
  save_evidence "11-personal-fetch-icon.json" "$personal_icon_response"
  assert_asset_fetchable "$USER_AUTH_HEADER" "$personal_icon_url"
fi

echo "==> Creating smoke personal resource"
personal_payload="$(
  jq -nc \
    --arg name "$personal_name" \
    --arg category "Smoke Personal" \
    --arg description "Workbench smoke personal resource" \
    --arg icon_url "$personal_icon_url" \
    --arg icon_mode "$personal_icon_mode" \
    --arg url "$PERSONAL_URL" \
    '{
      name: $name,
      category: $category,
      description: $description,
      iconUrl: $icon_url,
      iconMode: $icon_mode,
      url: $url,
      tags: ["smoke", "personal"],
      enabled: true,
      favorite: false,
      sort: 20
    }'
)"
personal_response="$(request "$USER_AUTH_HEADER" POST "/api/workbench/personal/resources" "$personal_payload")"
PERSONAL_RESOURCE_ID="$(printf '%s' "$personal_response" | jq -r '.id // ""')"
if [[ -z "$PERSONAL_RESOURCE_ID" ]]; then
  echo "personal resource creation did not return an id" >&2
  exit 1
fi
save_evidence "12-created-personal-resource.json" "$personal_response"

echo "==> Verifying smoke personal resource in client list"
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "13-user-resources-after-personal-create.json" "$client_resources"
if ! resource_present "$client_resources" "personalResources" "$PERSONAL_RESOURCE_ID"; then
  echo "personal resource ${PERSONAL_RESOURCE_ID} not found in client list" >&2
  exit 1
fi

echo "==> Recording smoke personal resource launch"
PERSONAL_RESOURCE_PATH_ID="$(url_encode "$PERSONAL_RESOURCE_ID")"
personal_launch_payload="$(jq -nc '{scope: "personal"}')"
request "$USER_AUTH_HEADER" POST "/api/workbench/resources/${PERSONAL_RESOURCE_PATH_ID}/launch" "$personal_launch_payload" >/dev/null
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "14-user-resources-after-personal-launch.json" "$client_resources"
if ! resource_has_recent_visit "$client_resources" "personalResources" "$PERSONAL_RESOURCE_ID"; then
  echo "personal resource ${PERSONAL_RESOURCE_ID} did not return metadata.recentVisitedAt after launch" >&2
  exit 1
fi

if [[ -n "$OTHER_USER_AUTH_HEADER" ]]; then
  echo "==> Verifying personal resource isolation with another user"
  other_client_resources="$(request "$OTHER_USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "15-other-user-resources-after-personal-create.json" "$other_client_resources"
  if resource_present "$other_client_resources" "personalResources" "$PERSONAL_RESOURCE_ID"; then
    echo "personal resource ${PERSONAL_RESOURCE_ID} was visible to another user" >&2
    exit 1
  fi
  assert_request_not_successful "$OTHER_USER_AUTH_HEADER" POST "/api/workbench/resources/${PERSONAL_RESOURCE_PATH_ID}/launch" "$personal_launch_payload"
  if [[ -n "$personal_icon_url" ]]; then
    assert_asset_not_fetchable "$OTHER_USER_AUTH_HEADER" "$personal_icon_url"
  fi
fi

echo "==> Updating smoke personal resource"
updated_personal_name="${personal_name}-updated"
updated_personal_payload="$(
  jq -nc \
    --arg name "$updated_personal_name" \
    --arg category "Smoke Personal Updated" \
    --arg description "Workbench smoke personal resource updated" \
    --arg icon_url "$personal_icon_url" \
    --arg icon_mode "$personal_icon_mode" \
    --arg url "$PERSONAL_URL" \
    '{
      name: $name,
      category: $category,
      description: $description,
      iconUrl: $icon_url,
      iconMode: $icon_mode,
      url: $url,
      tags: ["smoke", "updated"],
      enabled: false,
      favorite: true,
      sort: 1
    }'
)"
request "$USER_AUTH_HEADER" PUT "/api/workbench/personal/resources/${PERSONAL_RESOURCE_PATH_ID}" "$updated_personal_payload" >/dev/null
client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
save_evidence "16-user-resources-after-personal-update.json" "$client_resources"
updated_name="$(printf '%s' "$client_resources" | jq -r --arg id "$PERSONAL_RESOURCE_ID" '.personalResources[]? | select(.id == $id) | .name')"
if [[ "$updated_name" != "$updated_personal_name" ]]; then
  echo "personal resource update did not persist expected name" >&2
  exit 1
fi
if ! resource_enabled "$client_resources" "personalResources" "$PERSONAL_RESOURCE_ID"; then
  echo "personal resource ${PERSONAL_RESOURCE_ID} was disabled by update payload, want forced enabled" >&2
  exit 1
fi

if [[ "$KEEP_RESOURCES" == "1" ]]; then
  echo "==> Keeping smoke resources for manual GUI evidence"
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "17-user-resources-kept-after-personal-update.json" "$client_resources"
  admin_resources="$(request "$ADMIN_AUTH_HEADER" GET "/api/workbench/admin/resources")"
  save_evidence "18-admin-resources-kept-after-smoke.json" "$admin_resources"
else
  echo "==> Deleting smoke personal resource"
  request "$USER_AUTH_HEADER" DELETE "/api/workbench/personal/resources/${PERSONAL_RESOURCE_PATH_ID}" >/dev/null
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "17-user-resources-after-personal-delete.json" "$client_resources"
  if resource_present "$client_resources" "personalResources" "$PERSONAL_RESOURCE_ID"; then
    echo "personal resource ${PERSONAL_RESOURCE_ID} still present after delete" >&2
    exit 1
  fi

  echo "==> Deleting smoke server resource"
  if [[ -n "${DISABLED_SERVER_RESOURCE_ID:-}" && "$DISABLED_SERVER_RESOURCE_ID" != "$SERVER_RESOURCE_ID" ]]; then
    request "$ADMIN_AUTH_HEADER" DELETE "/api/workbench/admin/resources/$(url_encode "$DISABLED_SERVER_RESOURCE_ID")" >/dev/null
  fi
  if [[ -n "${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    request "$ADMIN_AUTH_HEADER" DELETE "/api/workbench/admin/resources/$(url_encode "$OTHER_RESTRICTED_SERVER_RESOURCE_ID")" >/dev/null
  fi
  if [[ -n "${USER_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    request "$ADMIN_AUTH_HEADER" DELETE "/api/workbench/admin/resources/$(url_encode "$USER_RESTRICTED_SERVER_RESOURCE_ID")" >/dev/null
  fi
  if [[ -n "${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}" ]]; then
    request "$ADMIN_AUTH_HEADER" DELETE "/api/workbench/admin/resources/$(url_encode "$GROUP_RESTRICTED_SERVER_RESOURCE_ID")" >/dev/null
  fi
  request "$ADMIN_AUTH_HEADER" DELETE "/api/workbench/admin/resources/${SERVER_RESOURCE_PATH_ID}" >/dev/null
  admin_resources="$(request "$ADMIN_AUTH_HEADER" GET "/api/workbench/admin/resources")"
  save_evidence "18-admin-resources-after-server-delete.json" "$admin_resources"
  for deleted_id in "$SERVER_RESOURCE_ID" "${GROUP_RESTRICTED_SERVER_RESOURCE_ID:-}" "${USER_RESTRICTED_SERVER_RESOURCE_ID:-}" "${OTHER_RESTRICTED_SERVER_RESOURCE_ID:-}"; do
    if [[ -n "$deleted_id" ]] && printf '%s' "$admin_resources" | jq -e --arg id "$deleted_id" '.[]? | select(.id == $id)' >/dev/null; then
      echo "server resource ${deleted_id} still present in admin list after delete" >&2
      exit 1
    fi
  done
  client_resources="$(request "$USER_AUTH_HEADER" GET "/api/workbench/resources")"
  save_evidence "19-user-resources-after-server-delete.json" "$client_resources"
  if resource_present "$client_resources" "serverResources" "$SERVER_RESOURCE_ID"; then
    echo "server resource ${SERVER_RESOURCE_ID} still present in client list after delete" >&2
    exit 1
  fi
  SMOKE_CLEANED=1
fi

echo "workbench smoke complete"
if [[ -n "${EVIDENCE_DIR:-}" ]]; then
  jq -nc --arg completedAt "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" '{completedAt: $completedAt}' >"${EVIDENCE_DIR}/complete.json"
  write_evidence_summary
  write_e2e_report_template
  write_kept_resources_cleanup_script
  echo "workbench smoke evidence saved to ${EVIDENCE_DIR}"
fi
