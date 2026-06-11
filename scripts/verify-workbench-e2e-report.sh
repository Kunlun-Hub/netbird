#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/verify-workbench-e2e-report.sh <workbench-evidence-dir>

Verifies that a workbench GUI E2E evidence directory is complete:
- summary.json and e2e-report.md exist
- all 11 Manual GUI E2E Checklist rows are marked PASS
- every PASS row has at least one evidence path
- local evidence paths exist under the evidence directory, or the path is an http(s) link
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 1
fi

evidence_dir="${1%/}"
summary_file="${evidence_dir}/summary.json"
report_file="${evidence_dir}/e2e-report.md"

if [[ ! -d "$evidence_dir" ]]; then
  echo "evidence directory does not exist: ${evidence_dir}" >&2
  exit 1
fi
if [[ ! -f "$summary_file" ]]; then
  echo "missing summary.json in ${evidence_dir}" >&2
  exit 1
fi
if [[ ! -f "$report_file" ]]; then
  echo "missing e2e-report.md in ${evidence_dir}" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "missing required command: jq" >&2
  exit 1
fi

if ! jq -e . "$summary_file" >/dev/null; then
  echo "summary.json is not valid JSON: ${summary_file}" >&2
  exit 1
fi

manual_required_count="$(
  jq -r 'if (.manualGuiEvidenceRequired | type) == "array" then (.manualGuiEvidenceRequired | length) else 0 end' "$summary_file"
)"
if [[ "$manual_required_count" -lt 8 ]]; then
  echo "summary.json does not list the expected manual GUI evidence requirements" >&2
  exit 1
fi

normalize_cell() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value="${value//\`/}"
  printf '%s' "$value"
}

row_count=0
missing=0

while IFS= read -r line; do
  [[ "$line" =~ ^\|[[:space:]]*([0-9]+)[[:space:]]*\| ]] || continue
  IFS='|' read -r _ item scenario status evidence notes _ <<<"$line"
  item="$(normalize_cell "$item")"
  scenario="$(normalize_cell "$scenario")"
  status="$(normalize_cell "$status")"
  evidence="$(normalize_cell "$evidence")"

  if [[ ! "$item" =~ ^[0-9]+$ || "$item" -lt 1 || "$item" -gt 11 ]]; then
    continue
  fi

  row_count=$((row_count + 1))
  if [[ "$status" != "PASS" ]]; then
    echo "manual GUI E2E item ${item} is not PASS: ${status:-<empty>} (${scenario})" >&2
    missing=1
    continue
  fi
  if [[ -z "$evidence" ]]; then
    echo "manual GUI E2E item ${item} has no evidence path (${scenario})" >&2
    missing=1
    continue
  fi

  evidence_lines="$(
    printf '%s' "$evidence" \
      | sed -E 's#<br[[:space:]]*/?>#\n#g; s#[,;]#\n#g'
  )"
  found_evidence=0
  while IFS= read -r evidence_path; do
    evidence_path="$(normalize_cell "$evidence_path")"
    [[ -z "$evidence_path" ]] && continue
    found_evidence=1
    if [[ "$evidence_path" =~ ^https?:// ]]; then
      continue
    fi
    evidence_path="${evidence_path#./}"
    if [[ "$evidence_path" = /* ]]; then
      candidate="$evidence_path"
    else
      candidate="${evidence_dir}/${evidence_path}"
    fi
    if [[ ! -e "$candidate" ]]; then
      echo "manual GUI E2E item ${item} evidence path does not exist: ${evidence_path}" >&2
      missing=1
    fi
  done <<<"$evidence_lines"

  if [[ "$found_evidence" -ne 1 ]]; then
    echo "manual GUI E2E item ${item} has no usable evidence path (${scenario})" >&2
    missing=1
  fi
done <"$report_file"

if [[ "$row_count" -ne 11 ]]; then
  echo "expected 11 manual GUI E2E checklist rows, found ${row_count}" >&2
  missing=1
fi

if [[ "$missing" -ne 0 ]]; then
  exit 1
fi

echo "workbench GUI E2E evidence is complete: ${evidence_dir}"
