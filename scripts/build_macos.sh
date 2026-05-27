#!/bin/bash
# NetBird macOS AMD64/ARM64 GUI 构建脚本

set -e

echo "=== NetBird macOS GUI 构建脚本 ==="

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
rm -rf dist/netbird_macos_amd64
rm -rf dist/netbird_macos_arm64
mkdir -p dist/netbird_macos_amd64
mkdir -p dist/netbird_macos_arm64
echo "清理并创建输出目录"

# 编译 AMD64 版本
echo "=== 编译 macOS AMD64 版本 ==="
# 删除旧的可执行文件
rm -f dist/netbird_macos_amd64/cloink-ui
rm -f dist/netbird_macos_amd64/cloink
echo "已删除旧的可执行文件"

# 编译 UI 客户端
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
    go build -o dist/netbird_macos_amd64/cloink-ui \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/ui

echo "AMD64 UI 客户端编译完成"

# 编译 CLI 客户端
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
    go build -o dist/netbird_macos_amd64/cloink \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/
echo "AMD64 CLI 客户端编译完成"

# 编译 ARM64 版本
echo "=== 编译 macOS ARM64 版本 ==="
# 删除旧的可执行文件
rm -f dist/netbird_macos_arm64/cloink-ui
rm -f dist/netbird_macos_arm64/cloink
echo "已删除旧的可执行文件"

# 编译 UI 客户端
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
    go build -o dist/netbird_macos_arm64/cloink-ui \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/ui

echo "ARM64 UI 客户端编译完成"

# 编译 CLI 客户端
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
    go build -o dist/netbird_macos_arm64/cloink \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/
echo "ARM64 CLI 客户端编译完成"

# 创建 launchd 配置文件
echo "=== 创建 launchd 配置文件 ==="
mkdir -p dist/netbird_macos_amd64/launchd
mkdir -p dist/netbird_macos_arm64/launchd

cat > dist/netbird_macos_amd64/launchd/com.cloink.client.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.cloink.client</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/cloink</string>
        <string>up</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/cloink.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/cloink.log</string>
</dict>
</plist>
EOF

# 复制到 ARM64 目录
cp dist/netbird_macos_amd64/launchd/com.cloink.client.plist dist/netbird_macos_arm64/launchd/

echo "launchd 配置文件创建完成"

# 设置可执行权限
chmod +x dist/netbird_macos_amd64/cloink-ui
chmod +x dist/netbird_macos_amd64/cloink
chmod +x dist/netbird_macos_arm64/cloink-ui
chmod +x dist/netbird_macos_arm64/cloink

echo "设置可执行权限完成"

# 检查文件
echo "=== 检查输出文件 ==="
echo "AMD64 版本:"
ls -la dist/netbird_macos_amd64/
echo "ARM64 版本:"
ls -la dist/netbird_macos_arm64/

# 打包文件
echo "=== 打包文件 ==="
tar -czf dist/cloink-macos-amd64-$APPVER.tar.gz -C dist/netbird_macos_amd64/ .
tar -czf dist/cloink-macos-arm64-$APPVER.tar.gz -C dist/netbird_macos_arm64/ .
echo "打包完成:"
echo "- dist/cloink-macos-amd64-$APPVER.tar.gz"
echo "- dist/cloink-macos-arm64-$APPVER.tar.gz"

echo "=== 编译完成 ==="
echo "输出目录:"
echo "- dist/netbird_macos_amd64/ (AMD64 版本)"
echo "- dist/netbird_macos_arm64/ (ARM64 版本)"
echo ""
echo "使用方法:"
echo "1. 解压发布包:"
echo "   tar -xzf cloink-macos-amd64-$APPVER.tar.gz  # 适用于 Intel Mac"
echo "   或"
echo "   tar -xzf cloink-macos-arm64-$APPVER.tar.gz  # 适用于 Apple Silicon Mac"
echo "2. 复制文件到系统目录:"
echo "   sudo cp cloink-ui /usr/local/bin/"
echo "   sudo cp cloink /usr/local/bin/"
echo "   sudo cp launchd/com.cloink.client.plist /Library/LaunchDaemons/"
echo "3. 加载并启动服务:"
echo "   sudo launchctl load /Library/LaunchDaemons/com.cloink.client.plist"
echo "   sudo launchctl start com.cloink.client"
echo "4. 运行 GUI 客户端:"
echo "   open -a cloink-ui"
