# Cloink SaaS Pro 授权任务拆分

## 目标

基于 `cloink-saas` 创建 `cloink-saas-pro` 分支，并新增账号级授权能力。基础版只开放核心组网能力，Pro/更高版本再开放高级身份、审计、DNS、品牌、中继、反向代理、设备合规、HA、网页远控等能力。

当前分支：`cloink-saas-pro`

## 基础版限制

- 只允许本地身份认证。
- 不支持接入 IDP，不允许新增身份提供商。
- 不支持流日志。
- 不支持 DNS 日志。
- 自建中继数最多 1 个。
- 用户数最多 3 个。
- 设备数最多 10 台。
- 反向代理服务器最多 1 台。
- 自定义域名最多 1 个。
- 自定义规则最多 3 条。
- 不支持设备合规功能。
- 不支持 DNS 功能。
- 不支持品牌定制。
- 不支持 HA 路由。
- 不支持网页 SSH 和 RDP。

## 设计原则

- 授权模块只负责账号/套餐能力，不替代现有用户 RBAC 权限。
- 服务端强制拦截，前端隐藏按钮只作为体验优化。
- RDP 当前没有 management 侧专用 session API，按产品决策只做 Dashboard 入口隐藏；不额外防护用户绕过前端手工创建临时 TCP 访问。
- 基础组网、登录、稳定性、重连、安全修复不做授权限制。
- 读接口可以返回授权状态，写接口必须拒绝未授权变更。
- 限额类能力需要在创建、更新和恢复已有数据时都能正确处理。

## 任务列表

### 0. 分支与文档

- [x] 从 `cloink-saas` 创建 `cloink-saas-pro` 分支。
- [x] 新增 `pro.md` 任务台账。

### 1. 授权核心模型

- [x] 新增授权/套餐模块：`management/server/entitlements`。
- [x] 定义 `Feature`、`Limit`、`Plan`、`Decision` 等核心类型。
- [x] 定义 `Checker` 接口，用于判断功能开关和数量限制。
- [x] 实现基础版默认策略。
- [x] 增加单元测试覆盖基础版功能开关和限额。

### 2. 账号授权状态来源

- [x] 先实现静态来源，默认基础版。
- [x] 预留后续 SaaS 订阅、license、trial 的 Provider 接口。
- [x] 在管理服务启动时注入授权 checker。
- [x] 暴露账号授权状态查询能力，供 Dashboard 展示。

### 3. 身份认证与 IDP 限制

- [x] 基础版只允许本地身份认证。
- [x] 禁止新增、启用或更新外部身份提供商。
- [x] 禁止开启 JWT group sync / groups propagation 等外部身份增强能力。
- [x] 确保已有 IDP 配置在基础版下不会继续作为可用登录入口。
- [x] 更新相关 handler / manager 测试。

### 4. 流日志与 DNS 日志限制

- [x] 禁止开启流日志相关开关。
- [x] 禁止开启流量 packet counter。
- [x] 禁止开启 DNS collection / DNS 日志。
- [x] 禁止开启 exit node collection。
- [x] 禁止配置 flow local storage / syslog 输出。
- [x] 更新 settings 和 extra settings 测试。

### 5. 自建中继限制

- [x] 限制基础版自建中继数最多 1 个。
- [x] 禁止配置超过 1 个 registered relay。
- [x] 确认 relay group / peer preference 不绕过自建中继限制。
- [x] 对超限写入返回明确授权错误。
- [x] 增加 registered relay 限额测试。

### 6. 用户和设备数量限制

- [x] 限制基础版用户数最多 3 个。
- [x] 限制基础版设备数最多 10 台。
- [x] 在用户邀请、用户创建、peer 登录/注册入口做检查。
- [x] 明确已超限账号的行为：允许读取和删除，禁止新增。
- [x] 增加用户和设备限额测试。

### 7. 反向代理、自定义域名、自定义规则限制

- [x] 限制基础版反向代理服务器最多 1 台。
- [x] 限制基础版自定义域名最多 1 个。
- [x] 限制基础版自定义规则最多 3 条。
- [x] 在 reverse proxy service/domain/rule 创建和更新入口做检查。
- [x] 增加反向代理限额测试。

### 8. 设备合规限制

- [x] 禁止创建、更新、启用 posture checks。
- [x] 禁止在策略中引用 posture checks。
- [x] 确保 network map 生成时基础版不会启用设备合规判断。
- [x] 增加 posture 限制测试。

### 9. DNS 功能限制

- [x] 禁止创建和更新 DNS nameserver group。
- [x] 禁止开启 DNS 相关账号设置。
- [x] 禁止自定义 DNS domain。
- [x] 确认客户端同步配置不下发 DNS 功能。
- [x] 增加 DNS 限制测试。

### 10. 品牌定制限制

- [x] 禁止更新 logo、dark logo、icon、tab title、primary color。
- [x] Dashboard 读取授权状态后隐藏或禁用品牌定制入口。
- [x] 增加品牌字段更新测试。

### 11. HA 路由限制

- [x] 识别现有 HA route 判定字段和创建路径。
- [x] 禁止基础版创建或启用 HA 路由。
- [x] 确保普通单 peer route 不受影响。
- [x] 增加 route manager / handler 测试。

### 12. 网页 SSH 和 RDP 限制

- [x] 识别网页 SSH/RDP 的 API、proxy 和前端入口。
- [x] 禁止基础版创建网页 SSH 会话/远程 job。
- [x] 基础版隐藏网页 RDP 入口；当前不做服务端防绕过，后续若新增 management 侧 RDP session API 再接入 `FeatureWebRDP`。
- [x] 禁止下发网页远控相关能力。
- [x] Dashboard 隐藏或禁用入口。
- [x] 增加网页远控授权测试。

### 13. 错误模型和前端体验

- [x] 统一未授权错误码，例如 `feature_not_entitled`、`limit_exceeded`。
- [x] 错误响应包含 feature、limit、current、required_plan。
- [x] Dashboard 根据授权状态展示基础版限制。
- [x] Dashboard 对未授权功能显示升级提示。

### 14. 回归测试

- [x] 授权核心单元测试。
- [x] settings / extra settings 更新测试。
- [x] IDP handler 测试。
- [x] user / peer 限额测试。
- [x] relay 限额测试。
- [x] DNS / posture / route / reverse proxy 测试。
- [x] 账号授权状态查询 API 测试。
- [x] 受影响包 `go test`。

### 15. 离线授权中心 / 机器码

- [x] 机器码使用 `server_url=<Dashboard域名>,key=<password>` 生成，Dashboard 域名去掉 `http(s)://`、端口、路径和结尾 `/`，再用授权密钥 AES 加密并 base64 输出到授权中心。
- [x] 授权 key 使用 OpenSSL/CryptoJS 兼容 AES-256-CBC salted passphrase 格式加密后输出 base64，解密密码默认内置为约定 key，并支持 `CLOINK_LICENSE_AES_KEY` 覆盖；后端保留旧 AES-256-GCM 格式解密兼容。
- [x] 授权明文格式：`server_url=cloink.4w.ink,license=enterprise,key=<password>,start_time=2020/1/1,end_time=2099/12/31,name=<授权使用方>;`，`name` 支持直接 UTF-8 中文或 base64 编码后的 UTF-8 值。
- [x] `license` 支持 `try`、`year`、`enterprise`，可写成 `license=enterprise` 或 `license=[enterprise]`；当前任一有效授权均映射为 Pro 权益，`license=[]` 按企业版授权处理。
- [x] 授权 key 持久化到 management `Datadir/license.json`。
- [x] Dashboard 新增授权中心，展示机器码、授权 URL、授权类型、状态、Key 掩码并支持更新/清除授权。
- [x] Dashboard 授权中心展示授权使用方，并移除授权 Key 的生成说明文案。
- [x] Dashboard 授权中心和用户、客户端、中继页面展示基础版当前用量、总额度和剩余额度，例如用户 `1/3`、客户端 `8/10`、中继 `0/1`。
- [x] Dashboard 每次 API 请求携带当前域名，后端据此判断授权 URL 是否匹配。
- [x] 当当前 Dashboard URL 与授权 `server_url` 不一致时，前端全局劫持到授权页并显示“授权 URL 不符，请联系售后解决”，覆盖冻结其他操作。
- [x] 新增交互式授权 Key 生成脚本 `scripts/generate-cloink-license.sh`：输入机器码、授权使用方、授权版本、有效期和密钥后输出授权 Key；选择 `enterprise` 时跳过日期输入并固定到 2099 年。

## 接入点记录

### 账号设置 / ExtraSettings

- 解析入口：`management/server/http/handlers/accounts/accounts_handler.go`
- 授权状态查询：`GET /api/accounts/{accountId}/entitlements`
- 服务端强制入口：`management/server/account.go`
- Settings 读取入口：`management/server/settings/manager.go`
- 需要覆盖：IDP 登录选项、JWT groups、groups propagation、flow logs、DNS logs、DNS domain、routing peer DNS resolution、branding、relay preferences、registered relays。

### IDP / 登录方式

- IDP 管理：`management/server/identity_provider.go`
- IDP HTTP：`management/server/http/handlers/idp/idp_handler.go`
- 登录偏好：`management/server/http/handlers/idp/login_preference_handler.go`
- connector guard：`management/server/http/handlers/idp/connector_guard_handler.go`
- 用户邀请和 IDP 用户创建：`management/server/user.go`

### 用户和设备限额

- 用户创建/邀请：`management/server/user.go`
- 用户 HTTP：`management/server/http/handlers/users`
- peer 登录/注册：`management/internals/shared/grpc/server.go`
- peer 管理：`management/server/peer.go`

### DNS

- DNS settings：`management/server/http/handlers/dns/dns_settings_handler.go`
- nameserver groups：`management/server/http/handlers/dns/nameservers_handler.go`
- DNS manager：`management/server/dns.go`
- sync 下发：`management/internals/controllers/network_map/controller/controller.go`

### 自建中继

- relay HTTP：`management/server/http/handlers/relays/relays_handler.go`
- relay settings 字段：`types.ExtraSettings.RegisteredRelays`
- relay 下发：`management/internals/shared/grpc/token_mgr.go`

### 反向代理 / 自定义域名 / 自定义规则

- service API：`management/internals/modules/reverseproxy/service/manager/api.go`
- service manager：`management/internals/modules/reverseproxy/service/manager/manager.go`
- custom domain API：`management/internals/modules/reverseproxy/domain/manager/api.go`
- custom domain manager：`management/internals/modules/reverseproxy/domain/manager/manager.go`
- access logs：`management/internals/modules/reverseproxy/accesslogs`

### 设备合规

- posture HTTP：`management/server/http/handlers/policies/posture_checks_handler.go`
- posture manager：`management/server/posture_checks.go`
- policy 引用：`management/server/policy.go`
- network map 计算：`management/server/types/account_components.go`

### HA 路由

- routes HTTP：`management/server/http/handlers/routes/routes_handler.go`
- route manager：`management/server/route.go`
- HA 判定：`route/hauniqueid.go`、`route/route.go`

### 网页 SSH / RDP

- SSH 服务端开关：`management/server/peer.go`
- SSH 策略协议：`types.PolicyRuleProtocolNetbirdSSH`，写入入口 `management/server/policy.go`
- 远程 job：`management/server/peer.go`、`management/server/http/handlers/peers/peers_handler.go`
- RDP：当前仅发现 `client/wasm/internal/rdp` 客户端实现，暂未发现 management 侧 RDP session API；按产品决策仅通过 Dashboard `web_rdp` 授权隐藏/显示入口。

## 进度记录

- 2026-05-31：创建 `cloink-saas-pro` 分支，新增授权任务拆分文档。
- 2026-05-31：完成授权核心模型、基础版默认策略、静态 Provider 和单元测试。
- 2026-05-31：完成主要授权接入点梳理，记录到本文档。
- 2026-05-31：接入账号设置层授权检查，限制基础版 flow/DNS logs、DNS 设置、品牌、自建中继数、JWT groups 和 groups propagation。
- 2026-05-31：接入 IDP 创建/更新授权检查，并禁止基础版配置外部 provider 登录选项。
- 2026-05-31：接入 DNS nameserver group 创建/更新授权检查。
- 2026-05-31：接入设备合规授权检查，限制 posture checks 创建/更新和策略引用。
- 2026-05-31：接入 HA 路由授权检查，基础版禁止创建同一 HA route 组内的多路由。
- 2026-05-31：将授权 checker 改为服务启动时注入，保持运行时默认基础版，同时避免裸测试 manager 被套餐策略误伤。
- 2026-05-31：接入反向代理限额，基础版限制 service 1 个、自定义域名 1 个、自定义规则按 service targets/path mappings 计数最多 3 条。
- 2026-05-31：接入用户和设备数限额，基础版限制用户 3 个、设备/peer 10 台，超限账号允许读取和删除但禁止新增。
- 2026-05-31：接入网页 SSH 相关服务端限制，基础版禁止开启 peer SSH、禁止 netbird-ssh 策略和远程 job 创建；RDP 当前按 Dashboard 入口隐藏处理。
- 2026-05-31：接入 network map 下发过滤，基础版会在同步视图中剥离 DNS、posture 和 netbird-ssh 能力，避免历史配置继续生效。
- 2026-05-31：补齐 relay peer/group preference 的唯一中继引用计数，避免通过偏好配置绕过自建中继 1 个的限额。
- 2026-05-31：完善 relay 引用归一，registered relay 的 key、ID、address 与 peer/group preference 中的同一引用会按同一个中继计数，避免误伤合法的单中继偏好配置。
- 2026-05-31：补齐身份侧基础版限制，外部 IdP 登录选项和外部 IdP 用户创建都会被授权检查拦截，只保留嵌入式本地身份路径。
- 2026-05-31：复核 RDP 入口，当前 management 侧未发现独立 RDP session API；RDP 仅存在于 wasm 客户端代理实现，按产品决策不做服务端临时 TCP 访问防绕过。
- 2026-05-31：补充 HA route 授权测试，基础版同一 HA 组的第二条路由会被 `FeatureHARoutes` 拒绝。
- 2026-05-31：修正 networks/resources 既有测试中过期的 reverse proxy reload mock 预期，恢复 `management/server/...` 全包回归。
- 2026-05-31：回归通过：`go test ./management/server/...`、`go test ./management/internals/...`。
- 2026-05-31：新增账号授权状态查询 API：`GET /api/accounts/{accountId}/entitlements`，返回 `plan`、`features`、`limits`、`usage`，并复用账号读取权限校验；同步 OpenAPI 与生成类型，供 Dashboard 后续读取。
- 2026-05-31：补充账号授权状态 handler / manager 测试，基础版默认快照会返回本地认证、品牌禁用、用户 3、设备 10 等授权信息。
- 2026-05-31：回归通过：`go test ./management/server`、`go test ./management/server/...`、`go test ./management/internals/...`、`go test ./shared/management/...`。
- 2026-05-31：Dashboard 接入账号授权状态查询，新增授权 hook 和升级提示组件；基础版会隐藏 DNS、设备合规、网络/DNS 日志、网页 SSH/RDP 等导航或按钮，并禁用 flow logs、branding、DNS 域名、JWT/group propagation 等设置入口。
- 2026-05-31：Dashboard 校验通过：`npm run lint` 无错误（仓库仍有既有 warning），`npx tsc --noEmit` 通过。
- 2026-05-31：Dashboard 从 `cloink-saas` 新建 `cloink-saas-pro` 分支，前端授权接入改动已保留在该分支工作区。
- 2026-05-31：补充 IDP HTTP handler 授权专项测试，断言基础版 `identity_providers` 授权拒绝会在创建/更新身份提供商接口中返回 403 和 `feature_not_entitled` 响应。
- 2026-05-31：IDP handler 回归通过：`go test ./management/server/http/handlers/idp`、`go test ./management/server/http/handlers/...`。
- 2026-05-31：授权明文新增 `name` 字段作为授权使用方，后端支持 UTF-8 中文与 base64 编码 UTF-8 解析，Dashboard 授权中心展示该字段并移除 Key 生成说明。
- 2026-05-31：授权中心前端不再因本地权限态禁用 Key 输入框和按钮，写入权限继续由后端接口校验；后端兼容 `license=[]` 并按企业版授权处理。
- 2026-05-31：授权 key 加密格式切换为 OpenSSL/CryptoJS 兼容的 `Salted__` AES-256-CBC base64 输出，支持用户给出的 `U2FsdGVkX1...` 示例；旧 AES-GCM key 仍可解密。
- 2026-05-31：授权更新接口权限从 `accounts.update` 调整为 `settings.update`，避免 Admin 角色因账号模块更新权限为 false 导致授权中心保存返回 403；无授权码仍默认基础版。
- 2026-05-31：机器码由明文域名改为 AES/base64 加密机器码，明文为 `server_url=<domain>,key=<password>`；授权方解密机器码后补充 `license`、`start_time`、`end_time`、`name` 字段，再反向加密生成授权 Key。
- 2026-05-31：新增交互式授权 Key 生成脚本 `scripts/generate-cloink-license.sh`，并将 Dashboard 中 `enterprise` 授权显示为“长期授权”。
- 2026-05-31：补齐授权错误结构化响应，`feature_not_entitled` / `limit_exceeded` 会通过 HTTP JSON 返回 `error_code`、`feature` / `limit`、`current`、`allowed`、`required_plan` 等字段，避免前端解析 message。
- 2026-05-31：错误模型回归通过：`go test ./management/server/entitlements`、`go test ./shared/management/http/util`、`go test ./management/server/http/handlers/idp`、`go test ./management/server/http/handlers/...`、`go test ./shared/management/...`、`go test ./management/server/...`。
- 2026-05-31：同步 OpenAPI `ErrorResponse` schema 并重新生成 `shared/management/http/api/types.gen.go`，授权错误结构化字段已进入生成类型；生成后回归通过 `go test ./shared/management/...`、`go test ./management/server/http/handlers/...`、`go test ./management/server/... -run TestDoesNotExist`。
- 2026-05-31：确认 RDP 授权策略调整为仅 Dashboard 入口层控制：无 `web_rdp` 授权隐藏入口，有授权显示入口，不做服务端临时 TCP 访问防绕过。
- 2026-05-31：新增离线授权中心后端闭环：机器码改为 Dashboard 域名，新增 AES/base64 license 解析校验、`Datadir/license.json` 持久化、`GET/PUT /api/accounts/{accountId}/license`，并由 license 状态驱动 Basic/Pro entitlement provider。
- 2026-05-31：license 明文格式按 `server_url/license/key/start_time/end_time` 解析，`license=[try|year|enterprise]` 中任一有效项都会开启 Pro；授权 URL 不符、未生效、过期、无效 key 都会回落基础版并返回明确状态。
- 2026-05-31：Dashboard 新增“授权中心”设置页，展示当前域名机器码、授权 URL、授权类型、授权状态和已安装 key 掩码；更新/清除授权后刷新 entitlements 缓存。
- 2026-05-31：Dashboard API 请求统一携带 `X-Cloink-Dashboard-Host` 当前域名；当后端返回 `url_mismatch` 时，全局冻结前端并劫持到授权页显示“授权 URL 不符，请联系售后解决”。
- 2026-05-31：同步 OpenAPI `AccountLicense` / `UpdateAccountLicenseRequest` 并重新生成 `shared/management/http/api/types.gen.go`。
- 2026-05-31：授权中心回归通过：`go test ./management/server/licensing`、`go test ./management/server -run 'Test(GetAccountLicense|UpdateAccountLicense|GetAccountEntitlements)'`、`go test ./management/server/http/handlers/accounts -run 'Test(GetAccountLicense|UpdateAccountLicense|GetAccountEntitlements)'`、`go test ./management/server/licensing ./management/server ./management/server/http/handlers/accounts ./shared/management/http/api`、Dashboard `npx tsc --noEmit`、`npm run lint`。
- 2026-05-31：基础版用量展示补齐：`GET /entitlements` 和 `GET/PUT /license` 返回 `usage`，Dashboard 授权中心展示账号资源用量卡片，用户、服务用户、客户端、中继页面顶部展示对应资源的已用/总额/剩余。
- 2026-05-31：基础版用量回归通过：`go test ./management/server -run 'Test(GetAccountEntitlements|AccountEntitlementUsage|GetAccountLicense|UpdateAccountLicense)' -count=1 -timeout=2m`、`go test ./management/server/http/handlers/accounts -run 'Test(GetAccountEntitlements|GetAccountLicense|UpdateAccountLicense)' -count=1 -timeout=2m`、`go test ./management/server/licensing ./shared/management/http/api -count=1 -timeout=2m`、Dashboard `npx tsc --noEmit`、`npx eslint src/modules/account/ResourceUsage.tsx`。
