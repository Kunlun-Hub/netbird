#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="ohoimager/cloink-relay"
VERSION="latest"
PUSH=false

show_help() {
  cat <<'EOF'
用法:
  build-relay.sh -v <version> [-p]

选项:
  -v <version>   镜像版本标签，例如 flow-dev
  -p             构建完成后推送到 Docker Hub
  -h             显示帮助

示例:
  ./scripts/build-relay.sh -v flow-dev
  ./scripts/build-relay.sh -v flow-dev -p

部署时可通过 NB_RELAY_ID 标识具体节点，例如 NB_RELAY_ID=hk-01。
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NETBIRD_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
TAGGED_IMAGE="${IMAGE_NAME}:${VERSION}"
BUILD_DIR="$(mktemp -d -t cloink-relay.XXXXXX)"

cleanup() {
  rm -rf "${BUILD_DIR}"
}
trap cleanup EXIT

if ! command -v docker >/dev/null 2>&1; then
  echo "未找到 docker 命令" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "未找到 go 命令" >&2
  exit 1
fi

echo "==> 编译 Relay 二进制"
cd "${NETBIRD_DIR}"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "${BUILD_DIR}/netbird-relay" ./relay

echo "==> 构建 Relay 镜像 ${TAGGED_IMAGE}"
cp "${NETBIRD_DIR}/relay/Dockerfile" "${BUILD_DIR}/Dockerfile"
docker build -t "${TAGGED_IMAGE}" "${BUILD_DIR}"

if [[ "${PUSH}" == "true" ]]; then
  echo "==> 推送镜像 ${TAGGED_IMAGE}"
  docker push "${TAGGED_IMAGE}"
fi

echo "完成: ${TAGGED_IMAGE}"
