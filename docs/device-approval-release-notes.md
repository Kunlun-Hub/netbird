# 设备接入审批发布说明

本文用于设备接入审批功能上线、升级和回滚时参考。

## 功能范围

设备接入审批开启后，用户通过 SSO 或设备码新增的真实设备会先进入待审批状态。管理员审批通过前，设备保持登录态，但不会获得正常 Network Map，也不能访问网络资源、路由、DNS、Peer 或服务。

以下设备不参与审批：

- 使用 Setup Key 新增的设备。
- Web SSH 创建的临时设备。
- Web RDP 创建的临时设备。
- 嵌入式代理设备。
- 同一账号下设备名不变的重新登录设备。

## 升级要求

启用该功能前，建议同时升级以下组件：

- Management 服务端。
- Dashboard 前端。
- 用户客户端。

服务端和 Dashboard 必须一起升级，否则管理员可能无法在控制台开启开关、查看待审批设备或执行审批操作。

客户端建议同步升级。旧客户端不理解新的 `requiresApproval` 状态时，开启设备审批后可能只表现为获取到空 Network Map 或无法访问网络，而不会显示“您的设备需要管理员审批”的明确提示。新客户端会在待审批和审批通过时显示通知，并在审批后自动继续同步网络。

## 上线步骤

1. 升级 Management 服务端。
2. 升级 Dashboard 前端。
3. 发布新客户端安装包，并提示用户升级客户端。
4. 管理员进入 `设置 > 认证`，开启 `设备接入审批`。
5. 使用一台新设备登录，确认设备进入 `设备 > 待审批设备`。
6. 管理员审批设备，确认客户端无需重新登录即可接入网络。
7. 检查活动日志，确认出现设备审批记录。

## 部署后 smoke 检查

仓库提供了一个部署后检查脚本：

```bash
DEVICE_APPROVAL_BASE_URL=https://cloink.example.com \
DEVICE_APPROVAL_TOKEN=<api-token> \
./scripts/device-approval-smoke.sh --device <设备名> --user <用户邮箱>
```

上线前可以先运行本地模拟测试，确认 smoke 脚本自身可用：

```bash
./scripts/test-device-approval-smoke.sh
```

该测试会启动一个临时本地 HTTP 服务，验证只读检查和 `--approve` 审批检查两条路径，不会访问真实部署。

脚本默认只读，会检查：

- `/device-approval` 公开提示页能否读取 URL 参数。
- `/api/accounts` 中 `settings.extra.peer_approval_enabled` 的当前值。
- `/api/peers` 中是否存在匹配的 `approval_required=true` 待审批设备。
- `/api/users?service_user=false` 中的用户信息能否与待审批设备关联。

如果需要让脚本审批第一台匹配设备，并检查 `peer.approve` 活动日志：

```bash
DEVICE_APPROVAL_BASE_URL=https://cloink.example.com \
DEVICE_APPROVAL_TOKEN=<api-token> \
./scripts/device-approval-smoke.sh --device <设备名> --user <用户邮箱> --approve
```

如部署环境的鉴权头不是 `Authorization: Bearer <token>`，可以显式传入：

```bash
DEVICE_APPROVAL_AUTH_HEADER="Token <api-token>" ./scripts/device-approval-smoke.sh
```

## 兼容性说明

- 已有设备不会因为开启设备审批而自动变成待审批。
- 关闭设备审批开关后，当前待审批设备会自动允许接入。
- 如果已有部署已经保存 `peer_approval_enabled`，升级后会保留原值。
- 待审批设备会被排除在有效设备集合外，不会收到正常 Network Map。
- 审批通过后，服务端会触发对应设备刷新 Network Map。

## 回滚说明

如果上线后需要临时回滚：

1. 先在 Dashboard `设置 > 认证` 关闭 `设备接入审批`，让当前待审批设备自动允许接入。
2. 确认 `设备 > 待审批设备` 已无需要处理的设备。
3. 再回滚 Dashboard 或 Management 服务端。

不要在存在待审批设备时直接回滚到不支持设备审批的旧版本，否则旧客户端或旧 Dashboard 可能无法清晰展示这些设备的待审批状态。
