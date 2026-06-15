#!/bin/bash
# Cloink Linux AMD64/ARM64 GUI 构建脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NETBIRD_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${NETBIRD_DIR}"

echo "=== Cloink Linux GUI 构建脚本 ==="

# 显示帮助信息
show_help() {
    echo "用法: $0 [OPTIONS] [VERSION]"
    echo ""
    echo "选项:"
    echo "  -h, --help              显示此帮助信息"
    echo "  -a, --arch ARCH         构建架构: amd64, arm64, all (默认: all)"
    echo ""
    echo "示例:"
    echo "  $0                        # 使用默认版本 0.0.0.1，同时构建 amd64 和 arm64"
    echo "  $0 1.2.3                  # 编译版本 1.2.3，同时构建 amd64 和 arm64"
    echo "  $0 --arch amd64 1.2.3     # 只构建 amd64"
    echo "  $0 --arch arm64 1.2.3     # 只构建 arm64"
    echo "  APPVER=1.2.3 $0           # 通过环境变量设置版本"
}

# 解析命令行参数
ARCH="all"
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -a|--arch)
            if [[ -z "${2:-}" ]]; then
                echo "错误: --arch 需要指定 amd64、arm64 或 all"
                show_help
                exit 1
            fi
            ARCH="$2"
            shift 2
            ;;
        *)
            # 假设第一个非选项参数是版本号
            if [[ -z "$VERSION" ]]; then
                VERSION="$1"
            else
                echo "错误: 提供了多个版本号"
                show_help
                exit 1
            fi
            shift
            ;;
    esac
done

# 版本号
if [[ -z "$VERSION" ]]; then
    export APPVER="${APPVER:-0.0.0.1}"
else
    export APPVER="$VERSION"
fi
echo "版本号: $APPVER"

case "$ARCH" in
    amd64)
        ARCHES=("amd64")
        ;;
    arm64)
        ARCHES=("arm64")
        ;;
    all)
        ARCHES=("amd64" "arm64")
        ;;
    *)
        echo "错误: 不支持的架构 $ARCH，可选值: amd64, arm64, all"
        show_help
        exit 1
        ;;
esac

linux_cc_for_arch() {
    local arch="$1"

    case "$arch" in
        amd64)
            if command -v x86_64-linux-gnu-gcc >/dev/null 2>&1; then
                echo "x86_64-linux-gnu-gcc"
            elif command -v gcc >/dev/null 2>&1; then
                echo "gcc"
            else
                return 1
            fi
            ;;
        arm64)
            if command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
                echo "aarch64-linux-gnu-gcc"
            else
                return 1
            fi
            ;;
        *)
            return 1
            ;;
    esac
}

create_systemd_service() {
    local output_dir="$1"

    mkdir -p "${output_dir}/systemd"
    cat > "${output_dir}/systemd/cloink.service" << EOF
[Unit]
Description=Cloink VPN Client
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/cloink service run --daemon-addr unix:///var/run/cloink.sock --log-file /var/log/cloink/client.log
Restart=always
RestartSec=5
User=root
Environment=SYSTEMD_UNIT=cloink

[Install]
WantedBy=multi-user.target
EOF
}

build_arch() {
    local arch="$1"
    local output_dir="dist/cloink_linux_${arch}"
    local package_path="dist/cloink-linux-${arch}-${APPVER}.tar.gz"
    local cc

    echo ""
    echo "=== 构建 Linux ${arch} 版本 ==="

    cc="$(linux_cc_for_arch "$arch")" || {
        if [[ "$arch" == "arm64" ]]; then
            echo "错误: 构建 Linux arm64 GUI 需要 aarch64-linux-gnu-gcc"
            echo "安装示例: sudo apt-get install gcc-aarch64-linux-gnu"
            echo "如果是 Ubuntu amd64 机器交叉构建，还需要启用 arm64 源并安装 Fyne 相关 arm64 dev 包:"
            echo "  sudo dpkg --add-architecture arm64"
            echo "  # arm64 包通常需要 ports.ubuntu.com/ubuntu-ports 源"
            echo "  sudo apt-get update"
            echo "  sudo apt-get install gcc-aarch64-linux-gnu pkg-config libgl1-mesa-dev:arm64 libx11-dev:arm64 libxcursor-dev:arm64 libxrandr-dev:arm64 libxinerama-dev:arm64 libxi-dev:arm64 libxxf86vm-dev:arm64 libxkbcommon-dev:arm64 libwayland-dev:arm64"
        else
            echo "错误: 构建 Linux amd64 GUI 需要 gcc 或 x86_64-linux-gnu-gcc"
            echo "安装示例: sudo apt-get install gcc"
        fi
        exit 1
    }

    # 创建输出目录
    rm -rf "$output_dir"
    mkdir -p "$output_dir"
    echo "清理并创建输出目录: $output_dir"

    # 编译 UI 客户端
    echo "=== 编译 Linux ${arch} UI 客户端 ==="
    rm -f "${output_dir}/cloink-ui"
    echo "已删除旧的 cloink-ui"

    CC="$cc" CGO_ENABLED=1 GOOS=linux GOARCH="$arch" \
        go build -o "${output_dir}/cloink-ui" \
        -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
        ./client/ui

    echo "${arch} UI 客户端编译完成"

    # 编译 CLI 客户端
    echo "=== 编译 Linux ${arch} CLI 客户端 ==="
    rm -f "${output_dir}/cloink"
    echo "已删除旧的 cloink"

    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
        go build -o "${output_dir}/cloink" \
        -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
        ./client/
    echo "${arch} CLI 客户端编译完成"

    # 创建 systemd 服务文件
    echo "=== 创建 Linux ${arch} systemd 服务文件 ==="
    create_systemd_service "$output_dir"
    echo "systemd 服务文件创建完成"

    # 设置可执行权限
    chmod +x "${output_dir}/cloink-ui"
    chmod +x "${output_dir}/cloink"
    echo "设置可执行权限完成"

    # 检查文件
    echo "=== 检查 Linux ${arch} 输出文件 ==="
    ls -la "$output_dir/"

    # 打包文件
    echo "=== 打包 Linux ${arch} 文件 ==="
    tar -czf "$package_path" -C "$output_dir/" .
    echo "打包完成: $package_path"
}

for target_arch in "${ARCHES[@]}"; do
    build_arch "$target_arch"
done

if [[ "${#ARCHES[@]}" -gt 1 ]]; then
    bundle_dir="dist/cloink_linux_${APPVER}"
    rm -rf "$bundle_dir"
    mkdir -p "$bundle_dir"
    for target_arch in "${ARCHES[@]}"; do
        cp "dist/cloink-linux-${target_arch}-${APPVER}.tar.gz" "$bundle_dir/"
    done
    tar -czf "dist/cloink-linux-all-${APPVER}.tar.gz" -C "$bundle_dir" .
    echo ""
    echo "合集包: dist/cloink-linux-all-${APPVER}.tar.gz"
fi

echo "=== 编译完成 ==="
echo "输出目录:"
for target_arch in "${ARCHES[@]}"; do
    echo "- dist/cloink_linux_${target_arch}/"
done
echo ""
echo "文件列表:"
for target_arch in "${ARCHES[@]}"; do
    echo "Linux ${target_arch}:"
    ls -la "dist/cloink_linux_${target_arch}/"
done

echo ""
echo "Linux 发布包:"
for target_arch in "${ARCHES[@]}"; do
    echo "- dist/cloink-linux-${target_arch}-${APPVER}.tar.gz"
done
if [[ "${#ARCHES[@]}" -gt 1 ]]; then
    echo "- dist/cloink-linux-all-${APPVER}.tar.gz"
fi
echo "======================================"
echo "发布包包含以下文件:"
echo "- cloink-ui (GUI 客户端)"
echo "- cloink (CLI 客户端)"
echo "- systemd/cloink.service (systemd 服务文件)"
echo ""
echo "使用方法:"
echo "1. 按目标 CPU 架构解压发布包:"
echo "   tar -xzf cloink-linux-amd64-$APPVER.tar.gz  # x86_64/AMD64"
echo "   或"
echo "   tar -xzf cloink-linux-arm64-$APPVER.tar.gz  # ARM64/aarch64"
echo "2. 复制文件到系统目录:"
echo "   sudo cp cloink-ui /usr/bin/"
echo "   sudo cp cloink /usr/bin/"
echo "   sudo cp systemd/cloink.service /etc/systemd/system/"
echo "3. 启用并启动服务:"
echo "   sudo systemctl enable cloink.service"
echo "   sudo systemctl start cloink.service"
echo "4. 运行 GUI 客户端: cloink-ui"
