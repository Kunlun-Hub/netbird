#!/bin/bash
# Cloink Linux 卸载脚本

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/cloink"
DATA_DIR="/var/lib/cloink"
LOG_DIR="/var/log/cloink"
SERVICE_NAME="cloink"

# 检查 root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        error "请使用 root 权限运行此脚本"
        echo "尝试使用: sudo $0"
        exit 1
    fi
}

# 停止服务
stop_service() {
    info "停止 Cloink 服务..."
    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        systemctl stop "$SERVICE_NAME" || true
    fi
    
    if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
        systemctl disable "$SERVICE_NAME" || true
    fi
    
    if pgrep -x "cloink" > /dev/null; then
        warning "强制终止 Cloink 进程..."
        pkill -x "cloink" 2>/dev/null || true
        pkill -x "cloink-ui" 2>/dev/null || true
    fi
}

# 删除服务文件
remove_service() {
    info "删除服务文件..."
    if [ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]; then
        rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
        systemctl daemon-reload
        systemctl reset-failed 2>/dev/null || true
    fi
}

# 删除文件
remove_files() {
    info "删除可执行文件..."
    if [ -f "$INSTALL_DIR/cloink" ]; then
        rm -f "$INSTALL_DIR/cloink"
    fi
    if [ -f "$INSTALL_DIR/cloink-ui" ]; then
        rm -f "$INSTALL_DIR/cloink-ui"
    fi
}

# 清理数据
cleanup_data() {
    echo -e "${YELLOW}警告：是否要删除配置和数据目录？${NC}"
    echo "  $CONFIG_DIR"
    echo "  $DATA_DIR"
    echo "  $LOG_DIR"
    read -p "删除这些文件吗？ (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        info "删除配置和数据目录..."
        rm -rf "$CONFIG_DIR"
        rm -rf "$DATA_DIR"
        rm -rf "$LOG_DIR"
    else
        warning "配置和数据目录已保留"
    fi
}

# 完成
show_complete() {
    echo ""
    echo -e "${GREEN}Cloink 已成功卸载${NC}"
    echo ""
    echo -e "${BLUE}配置和数据目录已${NC}"
}

# 主函数
main() {
    echo -e "${RED}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║${NC}                   ${YELLOW}Cloink 卸载脚本${NC}                        ${RED}║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    check_root

    read -p "确认要卸载 Cloink 吗？ (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        info "已取消卸载"
        exit 0
    fi

    stop_service
    remove_service
    remove_files
    cleanup_data

    show_complete
}

main "$@"
