# Cloink 项目概览

本文用于汇报和交接，按当前代码结构梳理 Cloink 项目的主要模块、职责和用途。

## 1. 产品整体定位

Cloink 是基于 NetBird 改造的一套安全组网与远程访问平台，核心能力包括：

- 通过 WireGuard 建立端到端加密网络。
- 通过控制面统一管理用户、设备、策略、路由、DNS 和网络资源。
- 通过 Dashboard 提供可视化运维管理。
- 通过 Signal / STUN / Relay 支持复杂网络环境下的连接协商和中继回退。
- 通过客户端 Agent 在终端侧执行网络配置、防火墙、DNS、路由和连接维护。
- 通过版本发布模块管理客户端安装包和升级分发。

一句话概括：

> Cloink 把传统 VPN 中分散的证书、网关、路由、防火墙和安全组配置，收敛到统一控制面，并让真实业务流量通过 WireGuard 加密直连。

## 2. 顶层模块说明

| 模块 | 路径 | 主要用途 |
|---|---|---|
| Dashboard 前端 | `dashboard/` | Web 管理后台，供管理员配置设备、用户、策略、路由、DNS、网络资源和版本发布。 |
| Management 控制面 | `netbird/management/` | 核心后端服务，负责账号、权限、设备、网络地图、策略、路由、DNS、事件和持久化。 |
| Client Agent | `netbird/client/` | 安装在用户设备上的客户端，负责认证、拉取配置、配置 WireGuard、应用路由/DNS/防火墙。 |
| Signal 服务 | `netbird/signal/` | 连接协商服务，客户端通过它交换 ICE 候选地址和连接信令。 |
| Relay 服务 | `netbird/relay/` | 当点对点连接失败时提供中继回退，支持 QUIC / WebSocket 等方式。 |
| STUN 服务 | `netbird/stun/` | 用于 NAT 探测，提升点对点连接成功率。 |
| Reverse Proxy | `netbird/proxy/` | 反向代理相关能力，用于安全暴露内部服务或远程访问场景。 |
| 公共协议与客户端库 | `netbird/shared/` | Management、Signal、Relay 等服务共享的协议、认证、HTTP、gRPC、状态码和客户端代码。 |
| 部署配置 | `netbird/main/` | 自托管 docker-compose、服务配置和启动入口。 |
| 发布与安装 | `netbird/release_files/`、`netbird/dist/`、`netbird/install.sh` | 系统服务文件、安装包产物、安装脚本和客户端分发相关内容。 |
| 文档与汇报素材 | `netbird/docs/` | 架构图、项目说明、图片资源等。 |

## 3. Management 控制面模块

Management 是整个系统的大脑，负责把身份、权限、网络状态和策略转换成客户端可执行的网络配置。

| 子模块 | 路径 | 用途 |
|---|---|---|
| HTTP API | `netbird/management/server/http/` | 对 Dashboard 和外部系统提供 REST API。 |
| API Handlers | `netbird/management/server/http/handlers/` | 各业务接口实现，包括账号、设备、策略、路由、DNS、网络、用户等。 |
| Account | `netbird/management/server/account/` | 账号、租户、组织级数据管理。 |
| Peer | `netbird/management/server/peer/` | 设备节点注册、状态、连接配置管理。 |
| Users / Groups | `netbird/management/server/users/`、`groups/` | 用户与分组管理。 |
| Permissions | `netbird/management/server/permissions/` | 角色、权限、操作范围控制。 |
| Networks / Routes | `netbird/management/server/networks/`、`routes/` | 网络资源、路由、路由节点、访问范围管理。 |
| DNS | `netbird/management/server/http/handlers/dns/` | DNS 配置、Nameserver、Zone 等管理入口。 |
| Policies | `netbird/management/server/http/handlers/policies/` | 访问控制策略管理。 |
| Setup Keys | `netbird/management/server/http/handlers/setup_keys/` | 批量注册设备的安装密钥。 |
| Events / Activity | `netbird/management/server/activity/`、`handlers/events/` | 操作日志、事件审计和状态追踪。 |
| IDP 集成 | `netbird/management/server/idp/` | 对接身份提供商，支持 OIDC / SSO / JWT 等能力。 |
| Version Releases | `netbird/management/server/http/handlers/version_releases/` | 客户端版本发布管理，支持安装包上传、下载、最新版本标记和公开查询。 |
| Store / Migration | `netbird/management/server/store/`、`migration/` | 数据存储和版本迁移。 |
| Network Map Controller | `netbird/management/internals/controllers/network_map/` | 生成并分发客户端网络地图。 |

汇报表达：

> Management 负责统一控制网络准入、权限策略和连接配置，是 Cloink 平台的控制中心。

## 4. Dashboard 前端模块

Dashboard 是管理员日常使用的 Web 控制台。

| 前端模块 | 路径 | 用途 |
|---|---|---|
| 页面路由 | `dashboard/src/app/` | Next.js App Router 页面，包括控制台、安装、邀请、远程访问等入口。 |
| 功能模块 | `dashboard/src/modules/` | 业务页面和组件实现。 |
| Overview | `dashboard/src/modules/overview/` | 系统总览、设备和网络状态展示。 |
| Peers / Peer | `dashboard/src/modules/peers/`、`peer/` | 设备列表、设备详情和状态管理。 |
| Access Control | `dashboard/src/modules/access-control/` | 策略、访问控制和 SSH 访问规则。 |
| Networks | `dashboard/src/modules/networks/` | 网络资源和路由节点管理。 |
| Routes / Exit Node | `dashboard/src/modules/routes/`、`exit-node/` | 路由、出口节点和流量转发配置。 |
| DNS | `dashboard/src/modules/dns/` | DNS、Nameserver、Zone 管理。 |
| Groups / Users | `dashboard/src/modules/groups/`、`users/` | 用户和分组管理。 |
| Setup Keys | `dashboard/src/modules/setup-keys/` | 批量接入密钥管理。 |
| Posture Checks | `dashboard/src/modules/posture-checks/` | 设备姿态检查配置。 |
| Remote Access | `dashboard/src/modules/remote-access/` | SSH / RDP 等远程访问入口。 |
| Reverse Proxy | `dashboard/src/modules/reverse-proxy/` | 反向代理和内部服务暴露管理。 |
| Settings | `dashboard/src/modules/settings/` | 系统设置、账号设置和集成配置。 |

汇报表达：

> Dashboard 将复杂的网络和安全配置可视化，让管理员可以用页面完成设备接入、权限配置和资源管理。

## 5. Client Agent 模块

Client Agent 运行在终端设备上，是实际执行网络连接和本地配置的组件。

| 子模块 | 路径 | 用途 |
|---|---|---|
| CLI / 命令入口 | `netbird/client/cmd/` | 提供 `cloink up`、`status`、`login` 等命令能力。 |
| 客户端服务 | `netbird/client/server/` | 本地 daemon 服务，负责长期运行和状态维护。 |
| WireGuard 接口 | `netbird/client/iface/` | 创建、配置和管理 WireGuard 网络接口。 |
| Peer Engine | `netbird/client/internal/peer/` | Peer 连接管理、ICE 协商、连接 worker 和调度。 |
| Relay Client | `netbird/client/internal/relay/` | 中继连接客户端逻辑。 |
| DNS | `netbird/client/internal/dns/`、`dnsfwd/` | 本地 DNS 配置、拦截和转发。 |
| Firewall / ACL | `netbird/client/firewall/`、`internal/acl/` | 本地防火墙和访问控制规则执行。 |
| Route Manager | `netbird/client/internal/routemanager/` | 静态路由、动态路由、出口节点和系统路由配置。 |
| Netflow | `netbird/client/internal/netflow/` | 网络流量采集和日志能力。 |
| Network Monitor | `netbird/client/internal/networkmonitor/` | 网络变化监听和连接恢复。 |
| Updater | `netbird/client/internal/updater/` | 客户端更新下载、签名校验和安装。 |
| SSH / Remote Access | `netbird/client/ssh/` | SSH 代理、服务端和远程访问能力。 |
| Desktop UI | `netbird/client/ui/` | 桌面端 UI、托盘和用户交互。 |
| Android / iOS | `netbird/client/android/`、`client/ios/` | 移动端相关代码。 |

汇报表达：

> Client Agent 是 Cloink 的执行端，负责把控制面下发的策略真正落到设备网络栈里。

## 6. 连接服务模块

| 服务 | 路径 | 用途 |
|---|---|---|
| Signal | `netbird/signal/` | 客户端之间交换连接信令，不承载业务流量。 |
| STUN | `netbird/stun/` | 帮助客户端发现公网地址和 NAT 类型。 |
| Relay | `netbird/relay/` | 当 P2P 不可达时提供中继通道，保障可用性。 |
| Shared Signal | `netbird/shared/signal/` | Signal 协议和客户端公共代码。 |
| Shared Relay | `netbird/shared/relay/` | Relay 协议、鉴权和客户端公共代码。 |

汇报表达：

> Signal、STUN 和 Relay 共同解决复杂网络环境下的连通性问题，优先直连，失败时自动回退。

## 7. 部署与交付模块

| 模块 | 路径 | 用途 |
|---|---|---|
| 自托管部署 | `netbird/main/docker-compose.yml` | 当前部署 Dashboard 和组合服务，暴露 `8080`、`8081` 和 UDP `3478`。 |
| 服务配置 | `netbird/main/config.yaml` | 管理服务、信令、中继、STUN、认证等运行配置。 |
| 安装脚本 | `netbird/install.sh` | Linux 客户端安装、服务注册和启动。 |
| Systemd 服务 | `netbird/release_files/systemd/` | Linux 服务文件，包括 management、signal、client 等。 |
| 构建脚本 | `netbird/build_linux.sh`、`build_windows.sh`、`build_macos.sh`、`build_docker.sh` | 各平台构建和镜像打包。 |
| 安装包产物 | `netbird/dist/` | 当前构建出的 Cloink 客户端和安装包。 |

汇报表达：

> 项目支持自托管和多平台客户端交付，已经具备从服务部署到客户端分发的闭环。

## 8. 当前项目亮点

- 架构清晰：Dashboard、Management、Client、Signal、Relay、STUN 分工明确。
- 安全基础扎实：底层使用 WireGuard，加密链路简洁可靠。
- 管理能力完整：用户、设备、组、策略、路由、DNS、网络资源都可集中管理。
- 连接适应性强：支持 P2P、STUN NAT 探测和 Relay 回退。
- 运维闭环增强：新增 Version Releases，可管理客户端安装包和版本分发。
- 自托管友好：当前 docker-compose 已将主要服务整合，便于快速部署。
- 可扩展空间大：已有 Reverse Proxy、Remote Access、Posture Checks、Netflow、Updater 等企业级能力基础。

## 9. 汇报时的推荐讲法

可以按以下顺序介绍：

1. 传统 VPN 架构的问题：配置分散、链路复杂、证书和网段维护成本高。
2. Cloink 的架构思路：控制面统一管理，数据面 WireGuard 加密直连。
3. 核心模块：Dashboard、Management、Client、Signal、Relay、STUN。
4. 核心能力：设备接入、访问控制、路由、DNS、远程访问、版本发布。
5. 产品优势：安全、易部署、易运维、跨网络环境稳定、可持续扩展。

一句总结：

> Cloink 不是简单替代 VPN，而是把“远程访问、组网、安全策略和客户端运维”整合成一个统一平台。
