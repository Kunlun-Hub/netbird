#!/bin/bash
# NetBird Windows AMD64 GUI 构建脚本

set -e

echo "=== NetBird Windows AMD64 GUI 构建脚本 ==="

normalize_windows_version() {
    local raw="$1"
    local cleaned core
    local -a parts numeric_parts

    cleaned="${raw#v}"
    core="${cleaned%%[-+]*}"

    IFS='.' read -r -a parts <<< "$core"
    for part in "${parts[@]}"; do
        if [[ "$part" =~ ^[0-9]+$ ]]; then
            numeric_parts+=("$part")
        else
            break
        fi
    done

    while [[ ${#numeric_parts[@]} -lt 4 ]]; do
        numeric_parts+=("0")
    done

    printf "%s.%s.%s.%s" \
        "${numeric_parts[0]:-0}" \
        "${numeric_parts[1]:-0}" \
        "${numeric_parts[2]:-0}" \
        "${numeric_parts[3]:-0}"
}

normalize_msi_version() {
    local normalized="$1"
    local -a parts
    IFS='.' read -r -a parts <<< "$normalized"
    printf "%s.%s.%s" \
        "${parts[0]:-0}" \
        "${parts[1]:-0}" \
        "${parts[2]:-0}"
}

# 显示帮助信息
show_help() {
    echo "用法: $0 [OPTIONS] [VERSION]"
    echo ""
    echo "选项:"
    echo "  -h, --help    显示此帮助信息"
    echo "  -m, --msi     构建 MSI 安装包 (需要 WiX Toolset)"
    echo "  -n, --nsis    构建 NSIS 安装包 (需要 makensis，默认)"
    echo "  -a, --all     同时构建 MSI 和 NSIS 安装包"
    echo ""
    echo "示例:"
    echo "  $0                  # 使用默认版本 0.0.0.1，构建 NSIS 安装包"
    echo "  $0 1.2.3 -m         # 编译版本 1.2.3，构建 MSI 安装包"
    echo "  $0 2.0.0 -a         # 编译版本 2.0.0，同时构建两种安装包"
    echo "  APPVER=1.2.3 $0     # 通过环境变量设置版本"
}

# 解析命令行参数
BUILD_MSI=false
BUILD_NSIS=true  # 默认构建 NSIS
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -m|--msi)
            BUILD_MSI=true
            BUILD_NSIS=false
            shift
            ;;
        -n|--nsis)
            BUILD_MSI=false
            BUILD_NSIS=true
            shift
            ;;
        -a|--all)
            BUILD_MSI=true
            BUILD_NSIS=true
            shift
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
export APPVER_NSI="$(normalize_windows_version "$APPVER")"
export APPVER_MSI="$(normalize_msi_version "$APPVER_NSI")"
echo "版本号: $APPVER"
echo "NSIS 版本号: $APPVER_NSI"
echo "MSI 版本号: $APPVER_MSI"

# 创建输出目录
rm -rf dist/netbird_windows_amd64
mkdir -p dist/netbird_windows_amd64
echo "清理并创建输出目录: dist/netbird_windows_amd64"

# 复制指定版本的 opengl32.dll
echo "=== 准备 opengl32.dll ==="
if [ -f "/home/apps/opengl32.dll" ]; then
    cp /home/apps/opengl32.dll dist/netbird_windows_amd64/opengl32.dll
    echo "opengl32.dll 已复制到输出目录"
else
    echo "错误: 未找到 /home/apps/opengl32.dll"
    exit 1
fi

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

# 编译 UI 客户端
echo "=== 编译 Windows UI 客户端 ==="
if command -v x86_64-w64-mingw32-gcc &> /dev/null; then
    # 删除旧的exe文件
    rm -f dist/netbird_windows_amd64/cloink-ui.exe
    echo "已删除旧的 cloink-ui.exe"
    
    # 生成 Windows 资源文件（包含图标和提权 manifest）
    if command -v x86_64-w64-mingw32-windres &> /dev/null; then
        echo "生成 UI Windows 资源文件..."
        x86_64-w64-mingw32-windres client/ui/resources.rc -O coff -o client/ui/rsrc_windows_amd64.syso
        echo "UI Windows 资源文件生成完成"
    else
        echo "错误: x86_64-w64-mingw32-windres 未安装"
        exit 1
    fi
    
    CC=x86_64-w64-mingw32-gcc CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
        go build -o dist/netbird_windows_amd64/cloink-ui.exe \
        -ldflags "-s -w -H windowsgui -X github.com/netbirdio/netbird/version.version=$APPVER" \
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
# 删除旧的exe文件
rm -f dist/netbird_windows_amd64/cloink.exe
echo "已删除旧的 cloink.exe"

if command -v x86_64-w64-mingw32-windres &> /dev/null; then
    echo "生成 CLI Windows 资源文件..."
    x86_64-w64-mingw32-windres client/resources_cli.rc -O coff -o client/rsrc_windows_amd64.syso
    echo "CLI Windows 资源文件生成完成"
else
    echo "错误: x86_64-w64-mingw32-windres 未安装"
    exit 1
fi

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -o dist/netbird_windows_amd64/cloink.exe \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$APPVER" \
    ./client/
echo "CLI 客户端编译完成"

rm -f client/rsrc_windows_amd64.syso

# 检查文件
echo "=== 检查输出文件 ==="
ls -la dist/netbird_windows_amd64/

# 生成 Windows 安装程序
echo "=== 生成 Windows 安装程序 ==="

# 构建 NSIS 安装包
if [ "$BUILD_NSIS" = true ]; then
    echo "正在构建 NSIS 安装包..."
    if command -v makensis &> /dev/null; then
        # 检查 NSIS 插件
        if [ -f "/usr/share/nsis/Plugins/amd64-unicode/ShellExecAsUser.dll" ] && [ -f "/usr/share/nsis/Plugins/amd64-unicode/EnVar.dll" ]; then
            echo "NSIS 插件已就绪"
            rm -f cloink-installer.exe dist/cloink-installer.exe
            # 编译安装程序
            echo "使用版本号: $APPVER (NSIS: $APPVER_NSI)"
            (cd client && makensis -V4 installer.nsis)
            
            if [ -f "cloink-installer.exe" ]; then
                mv -f cloink-installer.exe dist/
                echo "NSIS 安装程序编译成功: dist/cloink-installer.exe"
                echo "文件大小: $(du -h dist/cloink-installer.exe | cut -f1)"
            else
                echo "警告: NSIS 安装程序编译失败，请检查错误信息"
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
fi

# 构建 MSI 安装包
if [ "$BUILD_MSI" = true ]; then
    echo "正在构建 MSI 安装包..."
    if command -v wix &> /dev/null || command -v candle &> /dev/null || command -v light &> /dev/null; then
        rm -f dist/cloink-installer.msi dist/cloink-installer.wixobj
        # 检查 WiX 是否可用
        if command -v wix &> /dev/null; then
            echo "使用 WiX v4 构建..."
            # WiX v4 构建方式
            (cd client && wix build -nologo -o ../dist/cloink-installer.msi cloink.wxs)
        elif command -v candle &> /dev/null && command -v light &> /dev/null; then
            echo "使用 WiX v3 构建..."
            # WiX v3 构建方式
            (cd client && candle -nologo cloink.wxs -o ../dist/cloink-installer.wixobj && light -nologo ../dist/cloink-installer.wixobj -o ../dist/cloink-installer.msi)
        fi
        
        if [ -f "dist/cloink-installer.msi" ]; then
            echo "MSI 安装程序编译成功: dist/cloink-installer.msi"
            echo "文件大小: $(du -h dist/cloink-installer.msi | cut -f1)"
        else
            echo "警告: MSI 安装程序编译失败，请检查错误信息"
            echo "提示: WiX Toolset 需要在 Windows 上运行，或者使用 Wine"
        fi
    else
        echo "错误: WiX Toolset 未安装"
        echo "MSI 构建需要 WiX Toolset"
        echo ""
        echo "安装 WiX:"
        echo "在 Windows 上: https://wixtoolset.org/releases/"
        echo "在 Linux 上: 可以使用 Wine 运行 WiX 或者使用 .NET 版本的 WiX v4"
    fi
fi

echo "=== 编译完成 ==="
echo "输出目录: dist/netbird_windows_amd64/"
echo ""
echo "文件列表:"
ls -la dist/netbird_windows_amd64/
ls -la dist/ 2>/dev/null || true

echo ""
echo "构建的安装程序:"
if [ -f "dist/cloink-installer.exe" ]; then
    echo "- dist/cloink-installer.exe (NSIS)"
fi
if [ -f "dist/cloink-installer.msi" ]; then
    echo "- dist/cloink-installer.msi (MSI)"
fi

echo ""
echo "使用方法:"
echo "1. 复制安装程序到 Windows 系统"
echo "2. 双击运行安装程序"
echo "3. 按照安装向导完成安装"
