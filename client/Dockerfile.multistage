# 多阶段构建 Dockerfile - 支持多架构构建
# 使用方式: docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t ohoimager/cloink:latest .

# 构建阶段
FROM golang:1.25-alpine3.23 AS builder

# 安装必要工具
RUN apk add --no-cache git bash ca-certificates

# 设置工作目录
WORKDIR /build

# 复制 go mod 文件以利用缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 接受构建参数
ARG VERSION="0.68.3"

# 编译 Cloink 客户端
RUN CGO_ENABLED=0 GOOS=linux \
    go build -o cloink \
    -ldflags "-s -w -X github.com/netbirdio/netbird/version.version=${VERSION}" \
    ./client/

# 运行阶段
FROM alpine:3.23.3

# 安装运行时依赖
RUN apk add --no-cache \
    bash \
    ca-certificates \
    ip6tables \
    iproute2 \
    iptables

# 设置环境变量
ENV \
    NETBIRD_BIN="/usr/local/bin/cloink" \
    CL_LOG_FILE="console,/var/log/cloink/client.log" \
    CL_DAEMON_ADDR="unix:///var/run/cloink.sock" \
    CL_ENTRYPOINT_SERVICE_TIMEOUT="30" \
    NB_LOG_FILE="console,/var/log/cloink/client.log" \
    NB_DAEMON_ADDR="unix:///var/run/cloink.sock" \
    NB_ENTRYPOINT_SERVICE_TIMEOUT="30"

# 创建必要目录
RUN mkdir -p /var/lib/cloink /var/log/cloink /var/run

# 从构建阶段复制二进制文件和入口脚本
COPY --from=builder /build/cloink /usr/local/bin/
COPY client/cloink-entrypoint.sh /usr/local/bin/

# 设置可执行权限
RUN chmod +x /usr/local/bin/cloink \
    && chmod +x /usr/local/bin/cloink-entrypoint.sh

# 设置入口点
ENTRYPOINT [ "/usr/local/bin/cloink-entrypoint.sh" ]

# 健康检查（可选）
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD cloink status --check live 2>/dev/null || exit 1

