#!/bin/bash
# NetBird Windows AMD64 GUI 构建脚本
# 包含 opengl32.dll 和 wintun.dll

set -e

echo "=== NetBird Windows AMD64 GUI 构建脚本 ==="

# 版本号
export APPVER="${APPVER:-0.0.0.1}"
echo "版本号: $APPVER"

# 创建输出目录
rm -rf dist/netbird_windows_amd64
mkdir -p dist/netbird_windows_amd64
echo "清理并创建输出目录: dist/netbird_windows_amd64"

# 下载 wintun.dll
echo "=== 下载 wintun.dll ==="
if [ ! -f "dist/netbird_windows_amd64/wintun.dll" ]; then
    echo "下载 wintun..."
    curl -L -o /tmp/wintun.zip "https://www.wintun.net/builds/wintun-0.14.1.zip"
    unzip -o /tmp/wintun.zip -d /tmp/wintun
    cp /tmp/wintun/wintun/bin/amd64/wintun.dll dist/netbird_windows_amd64/
    echo "wintun.dll 已复制到输出目录"
else
    echo "wintun.dll 已存在，跳过下载"
fi

# 注意：不再包含 Mesa3D OpenGL DLL，避免冲突
# Windows 系统自带 OpenGL 支持

# 编译 UI 客户端
echo "=== 编译 Windows UI 客户端 ==="
if command -v x86_64-w64-mingw32-gcc &> /dev/null; then
    # 生成 Windows 资源文件（包含图标）
    export PATH=$PATH:$(go env GOPATH)/bin
    if command -v rsrc &> /dev/null; then
        echo "生成 Windows 资源文件..."
        rsrc -arch amd64 -ico client/ui/assets/netbird.ico -o client/ui/rsrc_windows_amd64.syso
        echo "Windows 资源文件生成完成"
    else
        echo "警告: rsrc 工具未找到，图标可能不会显示"
    fi
    
    CC=x86_64-w64-mingw32-gcc CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
        go build -o dist/netbird_windows_amd64/cloink-ui.exe \
        -ldflags "-s -w -H windowsgui -X github.com/netbirdio/netbird/version.version=0.68.3" \
        ./client/ui
    
    # 清理生成的资源文件
    rm -f client/ui/rsrc_windows_amd64.syso
    
    echo "UI 客户端编译完成"
else
    echo "错误: x86_64-w64-mingw32-gcc 未安装"
    echo "请安装 mingw-w64: apt-get install mingw-w64"
    exit 1
fi

# 编译 CLI 客户端
echo "=== 编译 Windows CLI 客户端 ==="
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -o dist/netbird_windows_amd64/cloink.exe \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=0.68.3" \
    ./client/
echo "CLI 客户端编译完成"

# 检查文件
echo "=== 检查输出文件 ==="
ls -la dist/netbird_windows_amd64/

# 生成 Windows 安装程序
echo "=== 生成 Windows 安装程序 ==="
if command -v makensis &> /dev/null; then
    # 检查 NSIS 插件
    if [ -f "/usr/share/nsis/Plugins/amd64-unicode/ShellExecAsUser.dll" ] && [ -f "/usr/share/nsis/Plugins/amd64-unicode/EnVar.dll" ]; then
        echo "NSIS 插件已就绪"
        # 编译安装程序
        export APPVER="${APPVER:-0.30.0.0}"
        echo "使用版本号: $APPVER"
        (cd client && makensis -V4 installer.nsis)
        
        if [ -f "netbird-installer.exe" ]; then
            echo "安装程序编译成功: netbird-installer.exe"
            echo "文件大小: $(du -h netbird-installer.exe | cut -f1)"
        else
            echo "警告: 安装程序编译失败，请检查错误信息"
        fi
    else
        echo "错误: NSIS 插件缺失"
        echo "请安装必要的 NSIS 插件:"
        echo "1. EnVar 插件"
        echo "2. ShellExecAsUser 插件"
        echo ""
        echo "安装命令:"
        echo "cd /tmp && curl -L -o envar.zip https://nsis.sourceforge.io/mediawiki/images/7/7f/EnVar_plugin.zip && sudo unzip -o envar.zip -d /usr/share/nsis/"
        echo "cd /tmp && curl -L -o shellexec.7z https://nsis.sourceforge.io/mediawiki/images/6/68/ShellExecAsUser_amd64-Unicode.7z && 7z x shellexec.7z -o/tmp/shellexec -y && sudo cp /tmp/shellexec/ShellExecAsUser.dll /usr/share/nsis/Plugins/amd64-unicode/"
    fi
else
    echo "错误: makensis 未安装"
    echo "请安装 NSIS: apt-get install nsis"
fi

echo "=== 编译完成 ==="
echo "输出目录: dist/netbird_windows_amd64/"
echo ""
echo "文件列表:"
ls -la dist/netbird_windows_amd64/

if [ -f "cloink-installer.exe" ]; then
    echo ""
    echo "Windows 安装程序: cloink-installer.exe"
    echo "======================================"
    echo "安装程序已包含以下文件:"
    echo "- cloink-ui.exe (GUI 客户端)"
    echo "- cloink.exe (CLI 客户端)"
    echo "- wintun.dll (WireGuard 驱动)"
    echo ""
    echo "使用方法:"
    echo "1. 复制 cloink-installer.exe 到 Windows 系统"
    echo "2. 双击运行安装程序"
    echo "3. 按照安装向导完成安装"
    echo "4. 安装完成后会自动启动 Cloink UI"
fi
