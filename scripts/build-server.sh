#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="ohoimager/cloink-server"
VERSION="latest"
PUSH=false

show_help() {
  cat <<'EOF'
用法:
  build-server.sh -v <version> [-p]

选项:
  -v <version>   镜像版本标签，例如 2.38.0
  -p             构建完成后推送到 Docker Hub
  -h             显示帮助

示例:
  ./scripts/build-server.sh -v 2.38.0
  ./scripts/build-server.sh -v 2.38.0 -p
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
SERVER_DIR="${ROOT_DIR}/netbird"
TAGGED_IMAGE="${IMAGE_NAME}:${VERSION}"

if ! command -v docker >/dev/null 2>&1; then
  echo "未找到 docker 命令" >&2
  exit 1
fi

echo "==> 构建 server 镜像 ${TAGGED_IMAGE}"
cd "${SERVER_DIR}"

docker build \
  -f "${SERVER_DIR}/combined/Dockerfile.multistage" \
  -t "${TAGGED_IMAGE}" \
  "${SERVER_DIR}"

if [[ "${PUSH}" == "true" ]]; then
  echo "==> 推送镜像 ${TAGGED_IMAGE}"
  docker push "${TAGGED_IMAGE}"
fi

echo "完成: ${TAGGED_IMAGE}"
