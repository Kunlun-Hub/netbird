#!/bin/bash
# Cloink Linux 一键安装脚本
# 支持 amd64/arm64/armv7

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 默认配置
DEFAULT_VERSION="0.68.3"
BASE_URL="https://pan.4w.ink/f/d/peID"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/cloink"
DATA_DIR="/var/lib/cloink"
SERVICE_NAME="cloink"

# 显示帮助信息
show_help() {
    cat << EOF
Cloink Linux 一键安装脚本

用法: $0 [OPTIONS]

选项:
  -v, --version VERSION  指定安装版本 (默认: $DEFAULT_VERSION)
  -u, --url URL          自定义下载链接
  -s, --setup-key KEY    设置 Setup Key
  -m, --management URL   设置 Management 服务器地址
  -h, --help             显示此帮助信息

示例:
  $0                                    # 使用默认配置安装
  $0 -v 1.2.3                           # 安装指定版本
  $0 -s your-key-here -m https://your-server.com  # 完整配置安装

EOF
}

# 检测架构
detect_arch() {
    local arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7*)
            echo "armv7"
            ;;
        *)
            error "不支持的架构: $arch"
            exit 1
            ;;
    esac
}

# 检查是否为 root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        error "请使用 root 权限运行此脚本"
        echo "尝试使用: sudo $0 $*"
        exit 1
    fi
}

# 检测系统是否使用 systemd
check_systemd() {
    if command -v systemctl &> /dev/null; then
        return 0
    else
        return 1
    fi
}

# 停止旧服务
stop_service() {
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        info "正在停止 Cloink 服务..."
        systemctl stop "$SERVICE_NAME" || true
    fi
    if pgrep -x "cloink" > /dev/null; then
        warning "发现 Cloink 进程正在运行，正在终止..."
        pkill -x "cloink" 2>/dev/null || true
    fi
}

# 创建目录
create_dirs() {
    info "创建必要目录..."
    mkdir -p "$INSTALL_DIR"
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR"
    mkdir -p "/var/log/cloink"
}

# 下载安装包
download_package() {
    local arch=$1
    local version=$2
    local filename="cloink-linux-${arch}-${version}.tar.gz"
    local download_url="${BASE_URL}/${filename}"

    if [ -n "$CUSTOM_URL" ]; then
        download_url="$CUSTOM_URL"
    fi

    info "下载 Cloink v${version} (${arch})..."

    local temp_dir=$(mktemp -d)
    local temp_file="${temp_dir}/${filename}"

    if command -v curl &> /dev/null; then
        curl -L -o "$temp_file" "$download_url"
    elif command -v wget &> /dev/null; then
        wget -O "$temp_file" "$download_url"
    else
        error "未找到 curl 或 wget，请先安装其中一个"
        exit 1
    fi

    if [ ! -f "$temp_file" ]; then
        error "下载失败"
        exit 1
    fi

    echo "$temp_file"
    echo "$temp_dir"
}

# 解压安装
extract_install() {
    local archive=$1
    local temp_dir=$2

    info "解压安装包..."
    tar -xzf "$archive" -C "$temp_dir"

    local extract_dir=$(find "$temp_dir" -type d -name "cloink*" | head -n 1)
    if [ -z "$extract_dir" ]; then
        extract_dir="$temp_dir"
    fi

    info "安装 Cloink 可执行文件..."
    if [ -f "$extract_dir/cloink" ]; then
        install -m 755 "$extract_dir/cloink" "$INSTALL_DIR/"
    fi
    if [ -f "$extract_dir/cloink-ui" ]; then
        install -m 755 "$extract_dir/cloink-ui" "$INSTALL_DIR/"
    fi

    # 清理临时文件
    rm -rf "$temp_dir"
}

# 创建 systemd 服务
create_systemd_service() {
    info "创建 systemd 服务..."

    cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=Cloink VPN Client
Documentation=https://cloink.io
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
ExecStart=${INSTALL_DIR}/cloink service run
Restart=always
RestartSec=5s
StartLimitInterval=0
Environment=HOME=/root

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cloink

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
}

# 配置 Cloink
configure_cloink() {
    if [ -n "$SETUP_KEY" ] || [ -n "$MANAGEMENT_URL" ]; then
        info "配置 Cloink..."
        
        local config_args=()
        
        if [ -n "$SETUP_KEY" ]; then
            config_args+=("--setup-key" "$SETUP_KEY")
        fi
        
        if [ -n "$MANAGEMENT_URL" ]; then
            config_args+=("--management-url" "$MANAGEMENT_URL")
        fi
        
        # 创建临时配置
        info "初始化配置..."
        if [ ! -f "$CONFIG_DIR/config.json" ]; then
            # 这里可以添加默认配置
            true
        fi
    fi
}

# 启用并启动服务
start_service() {
    info "启用 Cloink 服务..."
    systemctl enable "$SERVICE_NAME"
    
    info "启动 Cloink 服务..."
    systemctl start "$SERVICE_NAME"
    
    sleep 3
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "Cloink 服务已成功启动"
    else
        error "Cloink 服务启动失败"
        echo "查看日志: journalctl -u ${SERVICE_NAME} -n 50"
        exit 1
    fi
}

# 显示安装完成信息
show_summary() {
    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║${NC}                        ${GREEN}安装完成！${NC}                         ${CYAN}║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}安装目录：${NC} ${INSTALL_DIR}                                   ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}数据目录：${NC} ${DATA_DIR}                                     ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}配置目录：${NC} ${CONFIG_DIR}                                  ${CYAN}║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}常用命令：${NC}                                             ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   cloink --help              # 查看帮助                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   cloink status              # 查看状态                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   cloink up                  # 启动连接                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   cloink down                # 断开连接                ${CYAN}║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}服务管理：${NC}                                             ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   systemctl start cloink     # 启动服务                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   systemctl stop cloink      # 停止服务                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   systemctl restart cloink   # 重启服务                ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   systemctl status cloink    # 查看服务状态            ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   journalctl -u cloink -f    # 查看服务日志            ${CYAN}║${NC}"
    echo -e "${CYAN}╠════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${CYAN}║${NC} ${BLUE}卸载命令：${NC}                                             ${CYAN}║${NC}"
    echo -e "${CYAN}║${NC}   curl -s ${BASE_URL}/uninstall.sh | sudo bash           ${CYAN}║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

# 主函数
main() {
    local version="$DEFAULT_VERSION"
    local arch
    arch=$(detect_arch)
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--version)
                version="$2"
                shift 2
                ;;
            -u|--url)
                CUSTOM_URL="$2"
                shift 2
                ;;
            -s|--setup-key)
                SETUP_KEY="$2"
                shift 2
                ;;
            -m|--management)
                MANAGEMENT_URL="$2"
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done

    echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║${NC}                   ${GREEN}Cloink 安装脚本${NC}                        ${CYAN}║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    check_root "$@"

    if ! check_systemd; then
        error "此脚本需要 systemd 系统"
        exit 1
    fi

    info "系统架构: $arch"
    info "安装版本: $version"

    stop_service
    create_dirs

    # 下载并解压
    read -r temp_file temp_dir < <(download_package "$arch" "$version")

    if [ -z "$temp_file" ] || [ ! -f "$temp_file" ]; then
        error "下载失败"
        exit 1
    fi

    extract_install "$temp_file" "$temp_dir"

    # 验证安装
    if ! command -v cloink &> /dev/null; then
        error "安装失败: 找不到 cloink 命令"
        exit 1
    fi

    success "Cloink 安装成功！版本: $(cloink --version 2>&1 || echo "unknown")"

    create_systemd_service
    configure_cloink
    start_service
    show_summary
}

# 运行主函数
main "$@"
