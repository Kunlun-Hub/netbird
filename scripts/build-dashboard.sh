#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="ohoimager/cloink-dashboard"
VERSION="latest"
PUSH=false

show_help() {
  cat <<'EOF'
用法:
  build-dashboard.sh -v <version> [-p]

选项:
  -v <version>   镜像版本标签，例如 2.38.0
  -p             构建完成后推送到 Docker Hub
  -h             显示帮助

示例:
  ./scripts/build-dashboard.sh -v 2.38.0
  ./scripts/build-dashboard.sh -v 2.38.0 -p
EOF
}

while getopts ":v:ph" opt; do
  case "${opt}" in
    v)
      VERSION="${OPTARG}"
      ;;
    p)
      PUSH=true
      ;;
    h)
      show_help
      exit 0
      ;;
    :)
      echo "缺少参数: -${OPTARG}" >&2
      exit 1
      ;;
    \?)
      echo "未知参数: -${OPTARG}" >&2
      exit 1
      ;;
  esac
done

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DASHBOARD_DIR="${ROOT_DIR}/dashboard"
TAGGED_IMAGE="${IMAGE_NAME}:${VERSION}"

copy_dashboard_assets() {
  local oidc_worker_src=""
  local candidate
  local oidc_worker_dst="${DASHBOARD_DIR}/public/OidcServiceWorker.js"
  local trusted_src="${DASHBOARD_DIR}/public/local/OidcTrustedDomains.js"
  local trusted_dst="${DASHBOARD_DIR}/public/OidcTrustedDomains.js"

  for candidate in \
    "${DASHBOARD_DIR}/node_modules/@axa-fr/oidc-client-service-worker/dist/OidcServiceWorker.js" \
    "${DASHBOARD_DIR}/node_modules/@axa-fr/react-oidc/dist/OidcServiceWorker.js"; do
    if [[ -f "${candidate}" ]]; then
      oidc_worker_src="${candidate}"
      break
    fi
  done

  if [[ -z "${oidc_worker_src}" ]]; then
    echo "缺少 OidcServiceWorker.js，请确认 dashboard 依赖已完整安装" >&2
    exit 1
  fi

  if [[ ! -f "${trusted_src}" ]]; then
    echo "缺少文件: ${trusted_src}" >&2
    exit 1
  fi

  cp "${oidc_worker_src}" "${oidc_worker_dst}"
  cp "${trusted_src}" "${trusted_dst}"
}

clean_dashboard_build_artifacts() {
  echo "==> 清理旧的 dashboard 构建产物"
  rm -rf "${DASHBOARD_DIR}/out" "${DASHBOARD_DIR}/.next"
}

if ! command -v docker >/dev/null 2>&1; then
  echo "未找到 docker 命令" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "未找到 npm 命令" >&2
  exit 1
fi

echo "==> 构建 dashboard 静态文件"
cd "${DASHBOARD_DIR}"

if [[ ! -d node_modules ]]; then
  echo "==> 安装 dashboard 依赖"
  npm ci
fi

clean_dashboard_build_artifacts
echo "==> 准备 dashboard 静态资源"
copy_dashboard_assets
NEXT_PUBLIC_DASHBOARD_VERSION="${VERSION}" npm run build

if [[ ! -d out ]]; then
  echo "dashboard 构建失败，未生成 out 目录" >&2
  exit 1
fi

echo "==> 构建镜像 ${TAGGED_IMAGE}"
docker build \
  -f "${DASHBOARD_DIR}/docker/Dockerfile" \
  -t "${TAGGED_IMAGE}" \
  "${DASHBOARD_DIR}"

if [[ "${PUSH}" == "true" ]]; then
  echo "==> 推送镜像 ${TAGGED_IMAGE}"
  docker push "${TAGGED_IMAGE}"
fi

echo "完成: ${TAGGED_IMAGE}"
