#!/bin/bash
set -e

CONFIG_FOLDER="/etc/cloink"
CONFIG_FILE="$CONFIG_FOLDER/install.conf"

CLI_APP="cloink"
UI_APP="cloink-ui"

# 默认配置
OS_NAME=""
OS_TYPE=""
ARCH="$(uname -m)"
INSTALL_DIR="/usr/bin"
SUDO=""

# 获取 sudo 权限
if command -v sudo > /dev/null && [ "$(id -u)" -ne 0 ]; then
    SUDO="sudo"
elif command -v doas > /dev/null && [ "$(id -u)" -ne 0 ]; then
    SUDO="doas"
fi

# 设置默认 API 地址
if [ -z "${CLOINK_API_URL+x}" ]; then
    # 尝试从命令行参数获取
    if [ "$1" != "" ] && [[ "$1" != "--"* ]]; then
        CLOINK_API_URL="$1"
    else
        # 默认值，用户后续需要手动修改或者通过 cloink up 指定
        CLOINK_API_URL=""
    fi
fi

# 设置默认版本
if [ -z "${CLOINK_VERSION+x}" ]; then
    CLOINK_VERSION="latest"
fi

# 检测依赖并安装
install_dependencies() {
    echo "检查并安装依赖..."
    
    if [ -f /etc/os-release ]; then
        OS_NAME="$(. /etc/os-release && echo "$ID")"
        
        case "$OS_NAME" in
            ubuntu|debian|linuxmint|pop)
                ${SUDO} apt-get update
                ${SUDO} apt-get install -y curl tar ca-certificates systemd
                ;;
            centos|rhel|fedora|rocky|alma)
                if command -v dnf >/dev/null 2>&1; then
                    ${SUDO} dnf install -y curl tar ca-certificates systemd
                else
                    ${SUDO} yum install -y curl tar ca-certificates systemd
                fi
                ;;
            arch|manjaro|endeavouros)
                ${SUDO} pacman -Syu --noconfirm curl tar ca-certificates systemd
                ;;
            alpine)
                ${SUDO} apk add --no-cache curl tar ca-certificates openrc
                ;;
            *)
                echo "警告：无法确定系统类型，请确保已安装 curl、tar、ca-certificates"
                ;;
        esac
    else
        echo "警告：无法检测系统，请确保已安装 curl、tar、ca-certificates"
    fi
}

# 获取可用的版本列表
get_available_versions() {
    echo "从 $CLOINK_API_URL 获取版本列表..."
    VERSIONS_JSON=$(curl -s "${CLOINK_API_URL}/api/version-releases")
    if [ $? -ne 0 ]; then
        echo "错误：无法获取版本列表，请检查 CLOINK_API_URL 是否正确"
        exit 1
    fi
    echo "$VERSIONS_JSON"
}

# 根据版本和架构获取下载URL
get_download_url() {
    local VERSION=$1
    local PLATFORM=$2
    local ARCH=$3
    
    echo "查找 $PLATFORM/$ARCH 版本 $VERSION..."
    
    VERSIONS=$(get_available_versions)
    
    # 查找匹配的版本
    local DOWNLOAD_URL=$(echo "$VERSIONS" | grep -o '"downloadUrl":"[^"]*"' | grep -i "$PLATFORM" | grep -i "$ARCH" | cut -d'"' -f4 | head -n1)
    
    # 如果没找到，尝试查找 latest 版本
    if [ -z "$DOWNLOAD_URL" ]; then
        echo "未找到精确匹配，尝试查找最新版本..."
        DOWNLOAD_URL=$(echo "$VERSIONS" | grep -o '"downloadUrl":"[^"]*"' | grep -i "$PLATFORM" | grep -i "$ARCH" | cut -d'"' -f4 | head -n1)
    fi
    
    echo "$DOWNLOAD_URL"
}

# 下载并安装 cloink
download_and_install() {
    # 检测架构
    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l|armv6l)
            ARCH="armv7"
            ;;
        *)
            echo "不支持的架构：$ARCH"
            exit 1
            ;;
    esac
    
    echo "检测到系统架构：$ARCH"
    
    # 获取下载URL
    DOWNLOAD_URL=$(get_download_url "$CLOINK_VERSION" "linux" "$ARCH")
    
    if [ -z "$DOWNLOAD_URL" ]; then
        echo "错误：找不到适合 linux/$ARCH 的版本"
        echo "请在 $CLOINK_API_URL 发布页面检查是否有相应版本"
        exit 1
    fi
    
    echo "下载地址：$DOWNLOAD_URL"
    
    # 下载压缩包
    cd /tmp
    echo "正在下载..."
    curl -fL -o "cloink.tar.gz" "$DOWNLOAD_URL"
    
    if [ $? -ne 0 ]; then
        echo "下载失败"
        exit 1
    fi
    
    # 解压
    echo "正在解压..."
    mkdir -p /tmp/cloink-install
    tar -xzf "cloink.tar.gz" -C /tmp/cloink-install --strip-components 1 2>/dev/null || tar -xzf "cloink.tar.gz" -C /tmp/cloink-install
    
    # 安装二进制文件
    echo "正在安装..."
    ${SUDO} mkdir -p "$INSTALL_DIR"
    
    # 查找并安装二进制文件
    if [ -f "/tmp/cloink-install/$CLI_APP" ]; then
        ${SUDO} cp "/tmp/cloink-install/$CLI_APP" "$INSTALL_DIR/"
        ${SUDO} chmod +x "$INSTALL_DIR/$CLI_APP"
        echo "已安装 $CLI_APP 到 $INSTALL_DIR"
    fi
    
    if [ -f "/tmp/cloink-install/$UI_APP" ]; then
        ${SUDO} cp "/tmp/cloink-install/$UI_APP" "$INSTALL_DIR/"
        ${SUDO} chmod +x "$INSTALL_DIR/$UI_APP"
        echo "已安装 $UI_APP 到 $INSTALL_DIR"
    fi
    
    # 检查是否有 systemd 服务文件
    if [ -d "/tmp/cloink-install/systemd" ]; then
        echo "找到 systemd 服务文件"
        ${SUDO} mkdir -p /etc/systemd/system/
        ${SUDO} cp /tmp/cloink-install/systemd/*.service /etc/systemd/system/
    fi
    
    # 清理
    rm -f "cloink.tar.gz"
    rm -rf "/tmp/cloink-install"
    
    echo "安装完成！"
}

# 创建 systemd 服务
setup_systemd_service() {
    echo "设置 systemd 服务..."
    
    # 创建服务文件
    SERVICE_FILE="/etc/systemd/system/cloink.service"
    
    if [ ! -f "$SERVICE_FILE" ]; then
        cat <<EOF | ${SUDO} tee "$SERVICE_FILE"
[Unit]
Description=Cloink Network Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=$INSTALL_DIR/$CLI_APP service run
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
        echo "已创建 systemd 服务文件"
    else
        echo "systemd 服务文件已存在"
    fi
    
    # 重新加载 systemd
    ${SUDO} systemctl daemon-reload
}

# 启动服务并设置开机自启
setup_and_start_service() {
    echo "配置服务..."
    
    # 安装服务
    if ! ${SUDO} $CLI_APP service install 2>&1; then
        echo "服务已安装或使用 systemd"
        setup_systemd_service
    fi
    
    # 启动服务
    echo "启动服务..."
    if command -v systemctl >/dev/null 2>&1; then
        ${SUDO} systemctl enable cloink.service
        ${SUDO} systemctl start cloink.service
        echo "服务已设置为开机自启"
    else
        if ! ${SUDO} $CLI_APP service start 2>&1; then
            echo "警告：服务启动可能失败，请检查"
        fi
    fi
}

# 准备 tun 模块（如果需要）
prepare_tun_module() {
    echo "检查 tun 模块..."
    if [ ! -c /dev/net/tun ]; then
        if [ ! -d /dev/net ]; then
            ${SUDO} mkdir -m 755 /dev/net
        fi
        ${SUDO} mknod /dev/net/tun c 10 200
        ${SUDO} chmod 0755 /dev/net/tun
    fi
    
    if ! lsmod | grep -q "^tun\s" 2>/dev/null; then
        if [ -f "/lib/modules/tun.ko" ]; then
            ${SUDO} insmod /lib/modules/tun.ko 2>/dev/null || true
        fi
    fi
}

# 主安装函数
install_cloink() {
    echo "========================================"
    echo "      Cloink 客户端安装程序"
    echo "========================================"
    echo ""
    
    # 检查是否已安装
    if [ -x "$(command -v $CLI_APP)" ]; then
        echo "检测到 Cloink 已安装"
        
        # 检查是否是交互式终端
        if [ -t 0 ]; then
            # 交互式模式，询问用户
            read -p "是否要继续更新/重新安装？(y/N): " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                exit 0
            fi
        else
            # 非交互式模式（管道运行），自动继续更新
            echo "非交互式模式，自动继续更新..."
        fi
        
        # 停止现有服务
        echo "停止现有服务..."
        if command -v systemctl >/dev/null 2>&1; then
            ${SUDO} systemctl stop cloink.service 2>/dev/null || true
        else
            ${SUDO} $CLI_APP service stop 2>/dev/null || true
        fi
    fi
    
    # 安装依赖
    install_dependencies
    
    # 准备 tun 模块
    prepare_tun_module
    
    # 下载并安装
    download_and_install
    
    # 设置并启动服务
    setup_and_start_service
    
    # 保存配置
    ${SUDO} mkdir -p "$CONFIG_FOLDER"
    echo "api_url=$CLOINK_API_URL" | ${SUDO} tee "$CONFIG_FILE" > /dev/null
    echo "installed_at=$(date -Iseconds)" | ${SUDO} tee -a "$CONFIG_FILE" > /dev/null
    
    echo ""
    echo "========================================"
    echo "      安装完成！"
    echo "========================================"
    echo ""
    echo "下一步：运行 '$CLI_APP up' 来连接到你的 Cloink 网络"
    echo ""
}

# 显示帮助
show_help() {
    echo "Cloink 客户端安装程序"
    echo ""
    echo "用法："
    echo "  export CLOINK_API_URL=\"https://your-cloink-server.com\""
    echo "  export CLOINK_VERSION=\"latest\"  # 可选"
    echo "  ./install.sh"
    echo ""
    echo "选项："
    echo "  --help, -h     显示此帮助信息"
    echo "  --uninstall    卸载 Cloink"
    echo ""
    echo "环境变量："
    echo "  CLOINK_API_URL     Cloink 管理后台地址（必需）"
    echo "  CLOINK_VERSION     要安装的版本，默认 latest"
}

# 卸载函数
uninstall_cloink() {
    echo "正在卸载 Cloink..."
    
    # 停止服务
    if command -v systemctl >/dev/null 2>&1; then
        ${SUDO} systemctl stop cloink.service 2>/dev/null || true
        ${SUDO} systemctl disable cloink.service 2>/dev/null || true
        ${SUDO} rm -f /etc/systemd/system/cloink.service 2>/dev/null
        ${SUDO} systemctl daemon-reload
    else
        ${SUDO} $CLI_APP service stop 2>/dev/null || true
        ${SUDO} $CLI_APP service uninstall 2>/dev/null || true
    fi
    
    # 删除二进制文件
    ${SUDO} rm -f "$INSTALL_DIR/$CLI_APP" 2>/dev/null
    ${SUDO} rm -f "$INSTALL_DIR/$UI_APP" 2>/dev/null
    
    # 删除配置文件（保留配置以防需要）
    echo "配置文件保留在 $CONFIG_FOLDER，如需彻底删除请手动执行："
    echo "  ${SUDO} rm -rf $CONFIG_FOLDER"
    
    echo "卸载完成！"
}

# 主流程
case "$1" in
    --help|-h)
        show_help
        exit 0
        ;;
    --uninstall)
        uninstall_cloink
        exit 0
        ;;
    *)
        install_cloink
        ;;
esac
