#!/usr/bin/env bash
set -euo pipefail

DEFAULT_ENTERPRISE_START="2020/1/1"
DEFAULT_ENTERPRISE_END="2099/12/31"

cleanup() {
  if [[ -n "${ERR_FILE:-}" && -f "$ERR_FILE" ]]; then
    rm -f "$ERR_FILE"
  fi
}
trap cleanup EXIT

require_openssl() {
  if ! command -v openssl >/dev/null 2>&1; then
    echo "错误：未找到 openssl，请先安装 openssl。" >&2
    exit 1
  fi
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

read_required() {
  local prompt="$1"
  local value=""
  while [[ -z "$value" ]]; do
    read -r -p "$prompt" value
    value="$(trim "$value")"
    if [[ -z "$value" ]]; then
      echo "不能为空，请重新输入。" >&2
    fi
  done
  printf '%s' "$value"
}

read_secret() {
  local prompt="$1"
  local value=""
  while [[ -z "$value" ]]; do
    read -r -s -p "$prompt" value
    echo >&2
    value="$(trim "$value")"
    if [[ -z "$value" ]]; then
      echo "密钥不能为空，请重新输入。" >&2
    fi
  done
  printf '%s' "$value"
}

valid_date() {
  local value="$1"
  [[ "$value" =~ ^[0-9]{4}/[0-9]{1,2}/[0-9]{1,2}$ ]] || return 1
  date -d "${value//\//-}" >/dev/null 2>&1
}

read_date() {
  local prompt="$1"
  local value=""
  while true; do
    value="$(read_required "$prompt")"
    if valid_date "$value"; then
      printf '%s' "$value"
      return 0
    fi
    echo "日期格式不正确，请按示例输入：2016/1/1。" >&2
  done
}

read_license_type() {
  local choice=""
  while true; do
    echo "请选择授权版本：" >&2
    echo "  1) try" >&2
    echo "  2) year" >&2
    echo "  3) enterprise" >&2
    read -r -p "请输入 1/2/3 或版本名： " choice
    choice="$(trim "$choice")"
    case "${choice,,}" in
      1|try)
        printf 'try'
        return 0
        ;;
      2|year)
        printf 'year'
        return 0
        ;;
      3|enterprise)
        printf 'enterprise'
        return 0
        ;;
      *)
        echo "选择无效，请重新输入。" >&2
        ;;
    esac
  done
}

ensure_safe_field() {
  local label="$1"
  local value="$2"
  if [[ "$value" == *","* || "$value" == *";"* ]]; then
    echo "错误：${label} 不能包含英文逗号或分号。" >&2
    exit 1
  fi
}

openssl_decrypt() {
  local encoded="$1"
  local secret="$2"
  ERR_FILE="$(mktemp)"
  if ! printf '%s' "$encoded" | openssl enc -aes-256-cbc -d -a -A -md md5 -pass "pass:$secret" 2>"$ERR_FILE"; then
    echo "错误：机器码解密失败，请确认机器码和密钥是否匹配。" >&2
    cat "$ERR_FILE" >&2
    exit 1
  fi
  rm -f "$ERR_FILE"
  ERR_FILE=""
}

openssl_encrypt() {
  local payload="$1"
  local secret="$2"
  ERR_FILE="$(mktemp)"
  if ! printf '%s' "$payload" | openssl enc -aes-256-cbc -salt -a -A -md md5 -pass "pass:$secret" 2>"$ERR_FILE"; then
    echo "错误：授权 Key 加密失败。" >&2
    cat "$ERR_FILE" >&2
    exit 1
  fi
  rm -f "$ERR_FILE"
  ERR_FILE=""
}

field_value() {
  local payload="$1"
  local field="$2"
  local part key value
  IFS=',' read -r -a parts <<< "$payload"
  for part in "${parts[@]}"; do
    key="$(trim "${part%%=*}")"
    value="$(trim "${part#*=}")"
    if [[ "$key" == "$field" ]]; then
      printf '%s' "$value"
      return 0
    fi
  done
  return 1
}

require_openssl

echo "Cloink 授权 Key 生成器"
echo

machine_code="$(read_required "一、请输入机器码：")"
machine_code="$(printf '%s' "$machine_code" | tr -d '[:space:]')"

licensee="$(read_required "二、请输入授权使用方：")"
ensure_safe_field "授权使用方" "$licensee"

license_type="$(read_license_type)"
if [[ "$license_type" == "enterprise" ]]; then
  start_time="$DEFAULT_ENTERPRISE_START"
  end_time="$DEFAULT_ENTERPRISE_END"
  echo "已选择 enterprise，跳过开始/到期时间，授权有效期：${start_time} 至 ${end_time}。"
else
  start_time="$(read_date "四、请输入开始时间（格式示例：2016/1/1）：")"
  end_time="$(read_date "五、请输入到期时间（格式示例：2099/1/1）：")"
fi

secret="$(read_secret "六、请输入密钥：")"
ensure_safe_field "密钥" "$secret"

machine_payload="$(openssl_decrypt "$machine_code" "$secret")"
machine_payload="$(trim "${machine_payload%;}")"

server_url="$(field_value "$machine_payload" "server_url" || true)"
machine_key="$(field_value "$machine_payload" "key" || true)"
if [[ -z "$server_url" || -z "$machine_key" ]]; then
  echo "错误：机器码内容不完整，必须包含 server_url 和 key。" >&2
  exit 1
fi
if [[ "$machine_key" != "$secret" ]]; then
  echo "错误：机器码中的 key 与输入密钥不一致。" >&2
  exit 1
fi

license_payload="${machine_payload},license=${license_type},start_time=${start_time},end_time=${end_time},name=${licensee};"
license_key="$(openssl_encrypt "$license_payload" "$secret")"

echo
echo "授权信息："
echo "  授权 URL：${server_url}"
echo "  授权使用方：${licensee}"
echo "  授权版本：${license_type}"
if [[ "$license_type" == "enterprise" ]]; then
  echo "  前端显示：长期授权"
fi
echo "  有效期：${start_time} 至 ${end_time}"
echo
echo "授权 Key："
echo "$license_key"
