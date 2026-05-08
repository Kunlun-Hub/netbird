# Cloink / NetBird Architecture

```mermaid
flowchart LR
  User[管理员 / 终端用户] --> Dashboard[Dashboard Web UI<br/>Next.js / React]
  Dashboard -->|REST API / OAuth 回调| Management

  subgraph ControlPlane[控制面：netbird/management]
    Management[Management Server<br/>HTTP + gRPC]
    API[HTTP Handlers<br/>accounts / peers / policies / routes / dns / networks / setup_keys / users / version_releases]
    Core[核心域服务<br/>Account Manager / Network Map / Permissions / Jobs]
    Modules[扩展模块<br/>Reverse Proxy / Zones / Network Traffic / Integrations]
    Store[持久化<br/>/var/lib/netbird / store / migration]
    Management --> API --> Core
    Core --> Modules
    Core --> Store
  end

  subgraph RealtimePlane[连接协商与中继：signal / relay / stun]
    Signal[Signal Service<br/>gRPC 信令 / ICE Candidates]
    Relay[Relay Service<br/>QUIC / WebSocket fallback]
    Stun[STUN / ICE<br/>NAT 探测]
  end

  subgraph ClientSide[端侧 Agent：netbird/client]
    ClientUI[Client UI / CLI / Daemon]
    Auth[Auth / Profile Manager]
    Engine[Peer Engine<br/>ICE / Dispatcher / Worker]
    WG[WireGuard Interface<br/>iface / wgproxy / rosenpass]
    LocalNet[本地网络能力<br/>Firewall / ACL / DNS / Route Manager / Netflow]
    ClientUI --> Auth --> Engine --> WG
    Engine --> LocalNet
  end

  subgraph External[外部依赖]
    IdP[Identity Provider<br/>OIDC / SSO / JWT]
    Peers[其他 NetBird Peers]
    AdminData[配置与版本文件<br/>config.yaml / version_releases/files]
  end

  Management -->|IdP sync / auth| IdP
  Management -->|Network Map / Policy / DNS / Routes| ClientUI
  ClientUI -->|Login / Register / Sync| Management
  Engine -->|Signal exchange| Signal
  Engine -->|STUN probing| Stun
  Engine -->|Relay fallback| Relay
  WG <-->|Encrypted WireGuard tunnel| Peers
  Store --> AdminData
```

核心链路：

- 管理入口：管理员通过 Dashboard 调用 Management HTTP API，覆盖账号、用户、组、策略、路由、DNS、网络资源、安装密钥、版本发布等管理功能。
- 控制分发：Management 维护账号状态、权限、Network Map、任务和持久化数据，并把网络配置同步给客户端。
- 端侧执行：Client Daemon 完成认证、拉取网络地图、配置 WireGuard、应用 ACL/防火墙/DNS/路由，并通过 ICE 建立点对点连接。
- 实时连接：Signal 只负责协商消息；STUN 帮助 NAT 探测；P2P 不可用时走 Relay；最终业务流量通过 WireGuard 加密隧道传输。
- 自托管部署：`netbird/main/docker-compose.yml` 当前把 Dashboard 暴露在 `8080`，把组合服务暴露在 `8081` 和 UDP `3478`，数据卷挂载到 `/var/lib/netbird`。
