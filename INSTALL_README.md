# Cloink Linux 安装指南

## 快速安装

### 一键安装（推荐）

```bash
# 基本安装
curl -s https://pan.4w.ink/f/d/peID/install.sh | sudo bash

# 指定版本
curl -s https://pan.4w.ink/f/d/peID/install.sh | sudo bash -s -- -v 0.68.3

# 带 Setup Key 安装
curl -s https://pan.4w.ink/f/d/peID/install.sh | sudo bash -s -- -s YOUR_SETUP_KEY

# 完整配置安装
curl -s https://pan.4w.ink/f/d/peID/install.sh | sudo bash -s -- -s YOUR_KEY -m https://your-server.com
```

## 使用方法

### install.sh 选项

```bash
-v, --version VERSION  指定安装版本
-u, --url URL          自定义下载链接
-s, --setup-key KEY    设置 Setup Key
-m, --management URL   设置 Management 服务器地址
-h, --help             显示帮助信息
```

### 卸载

```bash
curl -s https://pan.4w.ink/f/d/peID/uninstall.sh | sudo bash
```

## 常用命令

### Cloink 命令

```bash
cloink --help              # 查看帮助
cloink status              # 查看状态
cloink up                  # 启动连接
cloink down                # 断开连接
cloink login               # 登录
```

### 系统服务管理

```bash
systemctl start cloink     # 启动服务
systemctl stop cloink      # 停止服务
systemctl restart cloink   # 重启服务
systemctl status cloink    # 查看服务状态
journalctl -u cloink -f    # 查看服务日志
```

## 安装的文件

- `/usr/local/bin/cloink` - CLI 客户端
- `/usr/local/bin/cloink-ui` - UI 客户端（如果有）
- `/etc/cloink/` - 配置目录
- `/var/lib/cloink/` - 数据目录
- `/etc/systemd/system/cloink.service` - 系统服务

## 支持的架构

- amd64 (x86_64)
- arm64 (aarch64)
- armv7

## 手动安装

如果需要手动安装：

```bash
# 1. 下载压缩包
wget https://pan.4w.ink/f/d/peID/cloink-linux-amd64-0.68.3.tar.gz

# 2. 解压
tar -xzf cloink-linux-amd64-0.68.3.tar.gz

# 3. 安装到系统
sudo install cloink /usr/local/bin/
sudo install cloink-ui /usr/local/bin/

# 4. 创建服务（参考 install.sh 中的 systemd 配置）
```
