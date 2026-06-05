#!/usr/bin/env bash

set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-ohoimager/cloink-relay}"
VERSION="${VERSION:-latest}"
PUSH="${PUSH:-false}"
ARCH="${ARCH:-}"
MULTIARCH="${MULTIARCH:-false}"
MULTISTAGE="${MULTISTAGE:-false}"

show_help() {
  cat <<'EOF'
用法:
  build-relay.sh -v <version> [选项]

选项:
  -v <version>   镜像版本标签，例如 flow-dev
  -a <arch>      架构: amd64, arm64, arm/v7
  -m             构建多架构镜像 (amd64, arm64, arm/v7)
  -s             使用多阶段构建，在 Docker 内编译
  -p             构建完成后推送到 Docker Hub
  -h             显示帮助

示例:
  ./scripts/build-relay.sh -v flow-dev
  ./scripts/build-relay.sh -v flow-dev -a arm64
  ./scripts/build-relay.sh -v flow-dev -m -s -p
  ./scripts/build-relay.sh -v flow-dev -p

部署时可通过 NB_RELAY_ID 标识具体节点，例如 NB_RELAY_ID=hk-01。
EOF
}

while getopts ":v:a:msph" opt; do
  case "${opt}" in
    v)
      VERSION="${OPTARG}"
      ;;
    a)
      ARCH="${OPTARG}"
      ;;
    m)
      MULTIARCH=true
      ;;
    s)
      MULTISTAGE=true
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

if [[ -z "${ARCH}" ]]; then
  case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7*) ARCH="arm/v7" ;;
    *) ARCH="amd64" ;;
  esac
fi

normalize_arch() {
  case "$1" in
    amd64|x86_64)
      echo "amd64"
      ;;
    arm64|aarch64)
      echo "arm64"
      ;;
    arm/v7|armhf)
      echo "arm/v7"
      ;;
    *)
      return 1
      ;;
  esac
}

ARCH="$(normalize_arch "${ARCH}")" || {
  echo "不支持的架构: ${ARCH}" >&2
  exit 1
}

if [[ "${MULTIARCH}" == "true" ]]; then
  if ! docker buildx version >/dev/null 2>&1; then
    echo "docker buildx 不可用，无法构建多架构 relay 镜像" >&2
    exit 1
  fi

  if [[ "${PUSH}" != "true" ]]; then
    echo "多架构构建需要配合 -p 推送到镜像仓库，buildx 不能直接 --load 多平台镜像" >&2
    exit 1
  fi

  echo "==> 使用 buildx 构建多架构 Relay 镜像 ${TAGGED_IMAGE}"
  BUILDER_NAME="cloink-relay-builder"
  if ! docker buildx inspect "${BUILDER_NAME}" >/dev/null 2>&1; then
    docker buildx create --name "${BUILDER_NAME}" --use
  else
    docker buildx use "${BUILDER_NAME}"
  fi

  BUILD_ARGS=(
    --platform linux/amd64,linux/arm64,linux/arm/v7
    --build-arg "VERSION=${VERSION}"
    -f "${NETBIRD_DIR}/relay/Dockerfile.multistage"
    -t "${TAGGED_IMAGE}"
    --push
  )

  docker buildx build "${BUILD_ARGS[@]}" "${NETBIRD_DIR}"
else
  if [[ "${MULTISTAGE}" == "true" ]]; then
    if ! docker buildx version >/dev/null 2>&1; then
      echo "docker buildx 不可用，无法进行跨架构多阶段 relay 构建" >&2
      exit 1
    fi

    echo "==> 使用多阶段构建 Relay 镜像 ${TAGGED_IMAGE} (${ARCH})"
    BUILD_ARGS=(
      --platform "linux/${ARCH}"
      --build-arg "VERSION=${VERSION}"
      -f "${NETBIRD_DIR}/relay/Dockerfile.multistage"
      -t "${TAGGED_IMAGE}"
    )

    if [[ "${PUSH}" == "true" ]]; then
      BUILD_ARGS+=(--push)
    else
      BUILD_ARGS+=(--load)
    fi

    docker buildx build "${BUILD_ARGS[@]}" "${NETBIRD_DIR}"
  else
    case "${ARCH}" in
      amd64)
        GOARCH=amd64
        unset GOARM || true
        ;;
      arm64)
        GOARCH=arm64
        unset GOARM || true
        ;;
      arm/v7)
        GOARCH=arm
        export GOARM=7
        ;;
    esac

    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
      go build -trimpath \
      -ldflags="-s -w -X github.com/netbirdio/netbird/version.version=${VERSION}" \
      -o "${BUILD_DIR}/netbird-relay" ./relay

    echo "==> 构建 Relay 镜像 ${TAGGED_IMAGE} (${ARCH})"
    cp "${NETBIRD_DIR}/relay/Dockerfile" "${BUILD_DIR}/Dockerfile"
    docker build --platform "linux/${ARCH}" -t "${TAGGED_IMAGE}" "${BUILD_DIR}"

    if [[ "${PUSH}" == "true" ]]; then
      echo "==> 推送镜像 ${TAGGED_IMAGE}"
      docker push "${TAGGED_IMAGE}"
    fi
  fi
fi

echo "完成: ${TAGGED_IMAGE}"
