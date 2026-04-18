#!/bin/bash
# Cloink Docker 客户端镜像构建脚本
# 支持自定义镜像名称、标签和架构

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印彩色消息
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 默认配置
IMAGE_NAME="${IMAGE_NAME:-ohoimager/cloink}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
VERSION="${VERSION:-0.68.3}"
REGISTRY="${REGISTRY:-}"
PUSH="${PUSH:-false}"
CLEANUP="${CLEANUP:-true}"
# 自动检测架构，或使用默认值
if [ -z "$ARCH" ]; then
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64)
            ARCH="arm64"
            ;;
        armv7*)
            ARCH="arm/v7"
            ;;
        *)
            ARCH="amd64"
            ;;
    esac
fi
MULTIARCH="${MULTIARCH:-false}"
MULTISTAGE="${MULTISTAGE:-false}"  # 使用多阶段构建（在容器内编译）

# 显示帮助信息
show_help() {
    echo "Cloink Docker 客户端镜像构建脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -n, --name NAME      镜像名称 (默认: ohoimager/cloink)"
    echo "  -t, --tag TAG        镜像标签 (默认: latest)"
    echo "  -v, --version VER    版本号 (默认: 0.68.3)"
    echo "  -r, --registry REG   镜像仓库地址 (可选)"
    echo "  -a, --arch ARCH      架构: amd64, arm64, arm/v7 (默认: amd64)"
    echo "  -m, --multiarch      构建多架构镜像 (需要 buildx)"
    echo "  -s, --multistage     使用多阶段构建（在容器内编译）"
    echo "  -p, --push           构建后推送镜像"
    echo "  -c, --no-cleanup     不清理临时文件"
    echo "  -h, --help           显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                                       # 构建默认镜像"
    echo "  $0 -t v1.0.0 -p                         # 构建并推送 v1.0.0"
    echo "  $0 -n myrepo/cloink -t dev -p          # 自定义仓库和标签"
    echo "  $0 -a arm64 -p                         # 构建 arm64 镜像"
    echo "  $0 -s -p                                # 使用多阶段构建"
    echo "  $0 -m -p                                # 构建多架构并推送"
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--name)
            IMAGE_NAME="$2"
            shift 2
            ;;
        -t|--tag)
            IMAGE_TAG="$2"
            shift 2
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -a|--arch)
            ARCH="$2"
            shift 2
            ;;
        -m|--multiarch)
            MULTIARCH=true
            shift
            ;;
        -s|--multistage)
            MULTISTAGE=true
            shift
            ;;
        -p|--push)
            PUSH=true
            shift
            ;;
        -c|--no-cleanup)
            CLEANUP=false
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            print_error "未知选项: $1"
            show_help
            exit 1
            ;;
    esac
done

# 构建完整镜像名
FULL_IMAGE_NAME="${IMAGE_NAME}:${IMAGE_TAG}"
if [ -n "$REGISTRY" ]; then
    FULL_IMAGE_NAME="${REGISTRY}/${FULL_IMAGE_NAME}"
fi

echo "=========================================="
echo "   Cloink Docker 客户端镜像构建"
echo "=========================================="
echo "镜像名称: $FULL_IMAGE_NAME"
echo "版本号:   $VERSION"
echo "架构:     $ARCH"
echo "多架构:   $MULTIARCH"
echo "多阶段:   $MULTISTAGE"
echo "推送:     $PUSH"
echo "=========================================="
echo ""

# 检查 Docker 是否安装
if ! command -v docker &> /dev/null; then
    print_error "Docker 未安装，请先安装 Docker"
    exit 1
fi

print_info "检查 Docker..."
docker version

# 创建输出目录
rm -rf dist/docker
mkdir -p dist/docker
print_info "输出目录: dist/docker/"

# 编译客户端二进制
rm -f cloink

if [ "$MULTISTAGE" != "true" ] && [ "$MULTIARCH" != "true" ]; then
    print_info "编译 Cloink 客户端..."
    
    # 规范化架构名称
    case $ARCH in
        amd64|x86_64)
            GOARCH=amd64
            ;;
        arm64|aarch64)
            GOARCH=arm64
            ;;
        arm/v7|armhf)
            GOARCH=arm
            GOARM=7
            ;;
        *)
            print_error "不支持的架构: $ARCH"
            exit 1
            ;;
    esac
    
    # 编译 Go 二进制
    print_info "编译架构: $GOARCH"
    export CGO_ENABLED=0
    export GOOS=linux
    export GOARCH=${GOARCH}
    if [ -n "${GOARM:-}" ]; then
        export GOARM=${GOARM}
    fi
    
    go build -o cloink \
        -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=${VERSION}" \
        ./client/
    
    print_success "客户端编译完成"
    
    # 检查二进制文件
    if [ ! -f "cloink" ]; then
        print_error "编译失败: 未找到 cloink 二进制文件"
        exit 1
    fi
    
    ls -lh cloink
    file cloink
else
    if [ "$MULTISTAGE" = "true" ]; then
        print_info "多阶段构建模式，在 Docker 容器内编译"
    else
        print_info "多架构构建模式，使用 Dockerfile 内建编译"
    fi
fi

# 准备 Docker 构建上下文
print_info "准备 Docker 构建上下文..."

if [ "$MULTISTAGE" = "true" ]; then
    # 多阶段构建 - 使用当前目录
    cp client/Dockerfile.multistage Dockerfile
    print_info "使用多阶段 Dockerfile"
    BUILD_DIR=$(pwd)
else
    # 创建临时目录
    BUILD_DIR=$(mktemp -d -t cloink-docker.XXXXXX)
    trap 'if [ "$CLEANUP" = "true" ]; then rm -rf "$BUILD_DIR"; fi' EXIT
    
    # 复制必要文件，保持目录结构
    mkdir -p "$BUILD_DIR/client"
    cp client/Dockerfile "$BUILD_DIR/Dockerfile"
    cp client/cloink-entrypoint.sh "$BUILD_DIR/client/cloink-entrypoint.sh"
    if [ -f "cloink" ]; then
        cp cloink "$BUILD_DIR/cloink"
    fi
    cd "$BUILD_DIR"
fi

print_info "构建目录结构:"
ls -la
if [ -d "client" ]; then
    ls -la client
fi

# 构建 Docker 镜像
if [ "$MULTIARCH" = "true" ]; then
    print_info "构建多架构镜像..."
    
    # 检查 buildx 是否可用
    if ! docker buildx version &> /dev/null; then
        print_error "Docker buildx 未安装或不可用"
        exit 1
    fi
    
    # 创建或使用 builder
    BUILDER_NAME="cloink-builder"
    if ! docker buildx inspect "$BUILDER_NAME" &> /dev/null; then
        print_info "创建新的 buildx builder..."
        docker buildx create --name "$BUILDER_NAME" --use
    else
        docker buildx use "$BUILDER_NAME"
    fi
    
    # 构建并推送（如果启用）
    BUILD_ARGS=(
        --platform linux/amd64,linux/arm64,linux/arm/v7
        -t "$FULL_IMAGE_NAME"
        --build-arg VERSION="$VERSION"
    )
    
    if [ "$PUSH" = "true" ]; then
        BUILD_ARGS+=(--push)
    else
        BUILD_ARGS+=(--load)
    fi
    
    docker buildx build "${BUILD_ARGS[@]}" .
elif [ "$MULTISTAGE" = "true" ]; then
    print_info "使用多阶段构建镜像..."
    
    # 多阶段构建
    docker build -t "$FULL_IMAGE_NAME" --build-arg VERSION="$VERSION" .
    
    # 推送镜像
    if [ "$PUSH" = "true" ]; then
        print_info "推送镜像: $FULL_IMAGE_NAME"
        docker push "$FULL_IMAGE_NAME"
    fi
else
    # 单架构构建
    print_info "构建单架构镜像: $ARCH"
    
    docker build -t "$FULL_IMAGE_NAME" .
    
    # 推送镜像
    if [ "$PUSH" = "true" ]; then
        print_info "推送镜像: $FULL_IMAGE_NAME"
        docker push "$FULL_IMAGE_NAME"
    fi
fi

print_success "Docker 镜像构建完成"
print_info "镜像名称: $FULL_IMAGE_NAME"

# 验证镜像
print_info "验证镜像..."
if [ "$MULTIARCH" != "true" ] && [ "$PUSH" != "true" ]; then
    docker images | grep "${IMAGE_NAME}" | grep "${IMAGE_TAG}"
fi

# 输出使用说明
echo ""
echo "=========================================="
echo "   构建完成!"
echo "=========================================="
echo ""
echo "镜像信息:"
echo "  名称: $FULL_IMAGE_NAME"
echo "  版本: $VERSION"
echo ""
echo "使用方法:"
echo "  docker run --rm -it --cap-add=NET_ADMIN \\\"
echo "    -e CL_SETUP_KEY=your_setup_key \\\"
echo "    -e CL_MANAGEMENT_URL=https://your-server \\\"
echo "    -v cloink-data:/var/lib/cloink \\\"
echo "    $FULL_IMAGE_NAME"
echo ""
echo "或使用 CLI:"
echo "  docker run --rm -it --cap-add=NET_ADMIN \\\"
echo "    $FULL_IMAGE_NAME \\\"
echo "    cloink --help"
echo ""
if [ "$PUSH" = "true" ]; then
    echo "✓ 镜像已推送到仓库"
else
    echo "要推送镜像，使用: docker push $FULL_IMAGE_NAME"
fi
echo ""

