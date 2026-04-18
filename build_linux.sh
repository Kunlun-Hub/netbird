#!/bin/bash
# NetBird Linux AMD64 GUI 构建脚本

set -e

echo "=== NetBird Linux AMD64 GUI 构建脚本 ==="

# 显示帮助信息
show_help() {
    echo "用法: $0 [OPTIONS] [VERSION]"
    echo ""
    echo "选项:"
    echo "  -h, --help    显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                  # 使用默认版本 0.0.0.1"
    echo "  $0 1.2.3            # 编译版本 1.2.3"
    echo "  APPVER=1.2.3 $0     # 通过环境变量设置版本"
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
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

# 创建输出目录
rm -rf dist/netbird_linux_amd64
mkdir -p dist/netbird_linux_amd64
echo "清理并创建输出目录: dist/netbird_linux_amd64"

# 编译 UI 客户端
echo "=== 编译 Linux UI 客户端 ==="
# 删除旧的可执行文件
rm -f dist/netbird_linux_amd64/cloink-ui
echo "已删除旧的 cloink-ui"

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -o dist/netbird_linux_amd64/cloink-ui \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/ui

echo "UI 客户端编译完成"

# 编译 CLI 客户端
echo "=== 编译 Linux CLI 客户端 ==="
# 删除旧的可执行文件
rm -f dist/netbird_linux_amd64/cloink
echo "已删除旧的 cloink"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o dist/netbird_linux_amd64/cloink \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/
echo "CLI 客户端编译完成"

# 创建 systemd 服务文件
echo "=== 创建 systemd 服务文件 ==="
mkdir -p dist/netbird_linux_amd64/systemd
cat > dist/netbird_linux_amd64/systemd/cloink.service << EOF
[Unit]
Description=Cloink VPN Client
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/cloink up
Restart=always
RestartSec=5
User=%i

[Install]
WantedBy=multi-user.target
EOF

echo "systemd 服务文件创建完成"

# 设置可执行权限
chmod +x dist/netbird_linux_amd64/cloink-ui
chmod +x dist/netbird_linux_amd64/cloink

echo "设置可执行权限完成"

# 检查文件
echo "=== 检查输出文件 ==="
ls -la dist/netbird_linux_amd64/

# 打包文件
echo "=== 打包文件 ==="
tar -czf dist/cloink-linux-amd64-$APPVER.tar.gz -C dist/netbird_linux_amd64/ .
echo "打包完成: dist/cloink-linux-amd64-$APPVER.tar.gz"

echo "=== 编译完成 ==="
echo "输出目录: dist/netbird_linux_amd64/"
echo ""
echo "文件列表:"
ls -la dist/netbird_linux_amd64/

echo ""
echo "Linux 发布包: dist/cloink-linux-amd64-$APPVER.tar.gz"
echo "======================================"
echo "发布包包含以下文件:"
echo "- cloink-ui (GUI 客户端)"
echo "- cloink (CLI 客户端)"
echo "- systemd/cloink.service (systemd 服务文件)"
echo ""
echo "使用方法:"
echo "1. 解压发布包: tar -xzf cloink-linux-amd64-$APPVER.tar.gz"
echo "2. 复制文件到系统目录:"
echo "   sudo cp cloink-ui /usr/bin/"
echo "   sudo cp cloink /usr/bin/"
echo "   sudo cp systemd/cloink.service /etc/systemd/system/"
echo "3. 启用并启动服务:"
echo "   sudo systemctl enable cloink.service"
echo "   sudo systemctl start cloink.service"
echo "4. 运行 GUI 客户端: cloink-ui"
