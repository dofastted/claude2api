# Claude OAuth 固定环境转发框架：三套技术资料对比与设计

> 日期：2026-07-12  
> 范围：`claude-codex-gateway/`、`claude-codex-relay-reference/`、`cpa/`  
> 目标：所有 Claude OAuth 请求都以服务端确认的 `session_id` 作为唯一粘性键，稳定绑定 OAuth 凭证和 Windows、macOS、Linux 三个冻结环境之一；只有当当前 OAuth 被确认达到凭证级请求/额度上限、被撤销或永久失效时，才允许携带原 `session_id` 原子迁移到下一个兼容 OAuth。所有环境相关的入站字段必须被丢弃，并由网关生成固定值。

## 1. 结论

推荐以 CLIProxyAPI 的 Go 执行管线为运行底座，采用 `claude-codex-gateway` 的三槽位冻结模型和顺序敏感的请求重写方法，吸收 `claude-codex-relay-reference` 的严格客户端准入与 Header 白名单思路，但不要采用它从客户端学习 Header 的方式。

新框架的核心不应是更大的 profile，而应是 **Session Binding（会话绑定）** 和不可拆分的 **Environment Capsule（环境胶囊）**：

```text
Verified session_id
  └── Session Binding
      ├── OAuth Pool / OAuth Credential
      ├── Environment Slot: Windows | macOS | Linux
      ├── Capsule Set Version / Migration Epoch
      └── Egress Route: one mandatory proxy / one upstream allowlist

Claude OAuth Credential
  └── Credential Policy
      ├── Slot 0: Windows Capsule
      ├── Slot 1: macOS Capsule
      └── Slot 2: Linux Capsule
```

每个胶囊同时冻结以下内容：CLI 家族与版本、UA、Stainless 字段、设备与会话种子、时区、system 模板、billing/CCH 策略、beta 能力、TLS 指纹和代理出口。请求不能单独覆盖其中任何一项。`session_id` 绑定 OAuth 凭证、环境槽和胶囊版本；配额迁移时保留 `session_id` 和环境槽，但必须换用新 OAuth 自己的同槽胶囊，不得跨凭证复制 device ID 等凭证级身份。

最重要的实现原则是：**不要复制入站请求再删除若干字段；应从空的出站 Header 集合和经过 schema 解析的 body 重新构造请求。** 只有明确列入业务白名单的内容字段可以保留，所有身份、环境、路由和认证字段都由服务端生成。

## 2. 范围与假设

三个目录都是技术资料或源码摘录，不是三个可独立构建、运行的完整项目。因此本文比较的是资料中能够证实的架构和行为，不把未收录的能力当作已实现。

本文对“只允许三个系统环境”的解释是：

- 三个环境固定为 Windows、macOS、Linux，每个凭证恰好有三个持久化槽位。
- 请求调度只使用服务端确认的 `session_id`。首次请求由服务端选择 OAuth 和环境槽并原子建立绑定；后续请求只读取该绑定。
- 客户端不能通过 UA、system、model、IP、协议类型或自报环境切换 OAuth 或槽位。外部 `session_id` 必须由服务端生成或验签，不能把未验证字符串直接用作凭证路由键。
- 三个槽位使用同一个强制代理出口；如果需要三个不同公网出口，应建立三个独立 Credential Policy，而不是在请求级随机选择代理。
- 允许接管同一 session 的 OAuth 必须属于同一个同构池，共享模型能力、胶囊兼容性摘要、环境槽定义、出口和上游允许列表。
- “完整模拟环境”指网关可控制的出站 HTTP、body、TLS、时区元数据、会话身份和网络出口保持一致。它不等同于运行一台真实 Windows/macOS/Linux 主机，也不能仅靠 HTTP Header 证明真实操作系统。
- 固定 UA 必须来自经维护的 Claude Code 官方客户端身份目录；不接受请求方自报的任意 UA。

## 3. 三个目录的技术框架

### 3.1 `claude-codex-gateway`

这是三套资料中最接近目标的一套。它把系统拆为路由鉴权、账号调度、客户端识别、body 策略、身份处理和传输指纹六层，并为 Claude 与 Codex 分别提供环境 profile pool。

Claude 侧的关键链路是：

```text
入口识别
  → OAuth / API Key 分支
  → 判断是否真实 Claude Code
  → 选择三槽位环境 profile
  → 重写 system / metadata / cache_control / beta
  → 完成 CCH 签名
  → 重建或过滤 Header
  → profile 最终覆盖
  → 绑定 TLS profile
  → 上游
```

可证实的关键能力：

- `EnvironmentClass` 定义 Windows、Linux、macOS、Desktop；Desktop 最终并入 Windows 槽位。见 `claude-codex-gateway/core-code/shared/01_env_class_and_route.go:25` 和 `shared/02_route_to_slot.go:12`。
- v2 pool 的槽位顺序固定为 Windows、macOS、Linux。见 `shared/02_route_to_slot.go:28`。
- `ClaudeEnvironmentProfile` 已包含 device、session、UA、OS、架构、runtime、timezone、TLS、telemetry 和 beta 集合。见 `claude/06_profile_struct_and_defaults.go:27`。
- profile 校验要求 UA、client ID、device ID 和 session seed，且会补齐 TLS、cache policy 和时区。见 `claude/06_profile_struct_and_defaults.go:84`。
- 未绑定的凭证会懒生成冻结的 v2 pool，并持久化到账号扩展字段。见 `claude/09_acquire_profile_slot.go:54`。
- 同一 OS 的并发请求共享冻结身份，slot lease 不互斥。见 `claude/09_acquire_profile_slot.go:75`。
- profile 在客户端白名单 Header、指纹和 mimic Header 之后应用，能够执行最终覆盖。见 `claude/04_build_upstream_request.go:134`、`:151`、`:167`、`:177`。
- body 的 beta 清洗发生在 CCH 签名前，避免签名内容与最终请求不一致。见 `claude/04_build_upstream_request.go:93`、`:107`、`:117`。
- 最后绑定 TLS profile。见 `claude/04_build_upstream_request.go:216`。

### 3.2 `claude-codex-relay-reference`

这是 Node.js + Redis 风格的转发参考。它将客户端 Validator、鉴权限制、Header 过滤、身份服务、账号 Header 缓存、system 处理和账户级代理拆成独立模块。

主要链路是：

```text
路由
  → API Key / 客户端限制
  → Validator 判断官方 CLI 特征
  → 账号调度与 proxy agent
  → system / metadata / tool 处理
  → Header 白名单与账号级指纹
  → Anthropic 上游
```

可证实的关键能力：

- Validator 会检查 UA、路径、Claude system 模板和 `metadata.user_id`，准入与上游伪装分离。
- Claude Header 使用白名单，而不是转发任意请求 Header。见 `claude-codex-relay-reference/core/utils/headerFilter.js:53`。
- CDN、代理和原始认证 Header 有明确过滤。见 `core/utils/headerFilter.js:8`、`:29`。
- 账号 Header 服务提供默认 Windows 身份，并能把真实 Claude Code Header 缓存到 Redis。见 `core/services/claudeCodeHeadersService.js:14`、`:118`。
- Stainless 指纹按账号首次写入 Redis，随后覆盖出站 Header。见 `core/services/requestIdentityService.js:132`、`:156`。
- 请求按账号取得 proxy agent。见 `core/services/relay/claudeRelayService.identity.excerpt.js:112`。
- 非真实 Claude Code 请求会移动原 system、生成 Claude Code system，并补 `metadata.user_id`。见 `core/services/relay/claudeRelayService.identity.excerpt.js:205`。

### 3.3 `cpa`

`cpa/claude-codex-forwarding-cloak_CN.md` 描述的是当前 CLIProxyAPI 的 Go 运行底座。其优势不是单一伪装策略，而是已有完整的多协议入口、认证调度、translator、thinking 管线、Claude/Codex executor、HTTP/WS 和响应翻译能力。

主要链路是：

```text
Gin API
  → Access/Auth Manager
  → 协议 Handler
  → 模型与凭证选择
  → Translator / Thinking
  → Claude Executor
  → Cloak / tool rename / CCH / Header / uTLS
  → Anthropic 上游
  → 响应翻译
```

可证实的关键能力：

- 统一执行入口已经支持 HTTP、SSE、WebSocket、多 provider 和跨协议转换。见 `cpa/claude-codex-forwarding-cloak_CN.md:69`。
- Claude executor 已包含 system Cloak、user ID、工具名映射、CCH、Header 处理和 uTLS。见 `cpa/claude-codex-forwarding-cloak_CN.md:94`、`:232`、`:258`、`:279`。
- `ScrubProxyAndFingerprintHeaders` 已承担入站代理与浏览器指纹清理。见 `cpa/claude-codex-forwarding-cloak_CN.md:258`。
- OAuth 与 API Key 的认证实现、auth selector 和 session affinity 已存在。见 `cpa/claude-codex-forwarding-cloak_CN.md:423`。
- thinking 使用“canonical representation → provider applier”，适合作为 body 正规化的架构参考。见 `cpa/claude-codex-forwarding-cloak_CN.md:410`。

## 4. 优劣对比

| 维度 | `claude-codex-gateway` | `claude-codex-relay-reference` | `cpa` |
|---|---|---|---|
| 语言与运行模型 | Go；类型明确，适合高并发和严格管线 | Node.js；模块直观，Redis 依赖明显 | Go；完整服务、SDK、HTTP/WS 与多 provider |
| 三系统环境 | 已有冻结的 Win/macOS/Linux v2 pool | 没有三槽位；主要是一个账号 Header 集 | 当前资料只有设备稳定与配置默认值，没有完整三槽位策略 |
| Body 重写 | 最完整；system、metadata、beta、cache、CCH 有顺序约束 | 有 system、metadata、tool 和 payload rules | 已有 Cloak、签名、tool rename、translator，适合承载新策略 |
| Header 策略 | mimic 分支可跳过透传，profile 最后覆盖 | Claude 使用白名单，规则容易审计 | 已清理代理指纹并补默认值，但 Cloak 模式和真实客户端路径存在差异 |
| 身份稳定 | 凭证级冻结 profile、设备、会话、telemetry | Redis 按账号缓存/学习 Stainless 指纹 | user ID/device/session 有缓存与按 auth 混淆能力 |
| TLS | profile 可绑定 TLS 指纹 | 摘录中没有完整的 TLS 身份胶囊 | 已有 uTLS 客户端 |
| 时区 | profile 有 timezone 字段 | 没有完整时区模型 | 当前设计稿没有把时区与凭证、出口绑定 |
| 固定代理出口 | 资料中未显示与 profile 的强绑定 | 已有账户级 proxy agent | 底座有代理能力，但当前文档没有证明其与环境身份原子绑定 |
| 客户端准入 | 主要用于决定真 CC 与 mimic | 最强；Validator 和 Key 限制分层 | 主要依赖 API 鉴权与 Cloak 判断，缺少面向环境胶囊的严格准入契约 |
| 可观测性 | 有 profile slot/debug snapshot | 日志丰富，Redis 状态可查询 | 已有服务日志与执行框架，但缺少统一的 normalization report |
| 主要风险 | profile 是可选功能；请求仍参与选槽；存在 passthrough；出口未纳入胶囊 | 学习客户端 Header 会污染固定身份；默认身份较旧；重试改工具名属于补救策略 | 配置项分散；`auto` 会跳过真实 CC 的 system Cloak；尚无凭证-三槽-出口原子策略 |

### 4.1 `claude-codex-gateway` 的优点

- 三槽位冻结模型已经解决“一个凭证最多三个系统身份”的主体问题。
- profile 最后覆盖和 CCH 最后签名体现了正确的顺序意识。
- 身份字段覆盖面最广，包含 TLS、telemetry、timezone 和 beta。
- 懒生成并持久化便于老凭证迁移。

### 4.2 `claude-codex-gateway` 的不足

- `DetectClaudeEnvironmentClass(headers, body)` 仍读取不可信入站信息来选槽。虽然这些值不应直接进入出站，但攻击者仍能任意切换三个身份。
- `acquireClaudeEnvironmentProfileForRequest` 在 `single environment` 未启用时直接返回 nil，不能满足“所有 OAuth 请求强制规范化”。
- 真 Claude Code 与 mimic 分支不完全相同；若目标是“任何请求都覆写环境字段”，就不应因 UA 看起来真实而跳过环境规范化。
- v2 lease 是共享身份而非配额或并发限制。“三个槽位”不等于“只允许三个客户端”或“三个同时在线设备”。如果需要设备数量限制，必须另建 session/device admission 状态。
- 现有资料未把 proxy route、upstream host allowlist、DNS/连接策略与 profile 原子绑定。
- Header 最终覆盖不等于完整清除；更稳妥的方式是从空 Header map 构造。

### 4.3 `claude-codex-relay-reference` 的优点

- “准入”和“伪装”分离，安全边界清楚。
- Claude Header 使用允许列表，优于不断扩充禁止列表。
- 账号级代理已有明确落点，适合演化为固定出口。
- Redis 的首次写入和版本缓存便于多实例共享状态。

### 4.4 `claude-codex-relay-reference` 的不足

- 从真实客户端捕获并学习 Header 与“网关拥有固定环境”目标相反。客户端可以影响未来请求的账号身份。
- 默认只有一个 Windows 身份，没有三系统胶囊，也没有时区、TLS 和出口的一致性校验。
- 指纹字段不足时保持原样，会在严格模式中产生不确定出站结果；新框架应 fail closed。
- 部分“真实客户端”判断依赖 UA 和 system 相似度，属于可伪造信号，不能作为授权依据。
- 403 后随机化工具名是错误恢复，不是环境一致性设计；它增加请求之间的差异。
- Node.js 模块对 Header、body、Redis、代理分别处理，缺少一次性生成和验证的最终出站快照。

### 4.5 `cpa` 的优点

- 最适合作为实际落地底座，无需重新实现入口、流式响应、translator、auth manager 和 executor。
- Go 类型系统便于把胶囊和策略做成不可变对象。
- Claude executor 已掌握 CCH、OAuth 工具名、uTLS 和响应反向映射等难点。
- canonical thinking 管线说明项目已有“先规范化，再按 provider 输出”的架构习惯。

### 4.6 `cpa` 的不足

- 当前 Cloak 是请求级行为开关，不是凭证级强制安全策略。
- Header 默认值、device profile、user ID、代理、时区和 OAuth 调度仍是分散配置，可能形成互相矛盾的组合。
- `auto` 模式信任 `claude-cli` UA 而跳过部分重写，不符合“任何请求都排除并覆写环境字段”。
- 多 provider 和跨协议能力很强，但 Claude OAuth 凭证需要更窄的 capability：只能进入 Claude executor，只能到允许的 Anthropic 主机。
- 当前设计稿没有定义最终出站不变量，也没有证明代理不可绕过。

## 5. 推荐架构：Environment Capsule Gateway

### 5.1 核心对象

建议新增 `OAuthPoolPolicy`、`SessionBinding`、`CredentialPolicy`、`EnvironmentCapsule`、`ClientIdentity` 和 `TransportIdentity` 六个概念，而不是继续向已有 profile 平铺字段。

```go
type OAuthPoolPolicy struct {
    PoolID                       string
    Provider                     string // fixed: claude_oauth
    CredentialIDs                []string
    CapsuleCompatibilityDigest   string
    EgressRouteID                string
    AllowedUpstreams             []Origin
    AllowedModels                []string
}

type SessionBinding struct {
    SessionIDHash       string
    OAuthPoolID         string
    CredentialID        string
    EnvironmentSlot     int
    CapsuleSetVersion   uint64
    MigrationEpoch      uint64
    EgressRouteID       string
    CreatedAt           time.Time
    LastSeenAt          time.Time
    ExpiresAt           time.Time
}

type CredentialPolicy struct {
    CredentialID       string
    OAuthPoolID        string
    Provider           string // fixed: claude_oauth
    CapsuleSetVersion  uint64
    AllowedSlots       [3]EnvironmentClass
    EgressRouteID      string
    AllowedUpstreams   []Origin
    AllowedModels      []string
    Strict             bool
}

type EnvironmentCapsule struct {
    CredentialID       string
    Slot               int
    Environment        EnvironmentClass
    Identity           ClientIdentity
    RequestPolicy      RequestRewritePolicy
    TransportPolicy    TransportIdentity
    EgressRouteID      string
    Version            uint64
    Digest             string
}
```

`SessionBinding` 是请求调度的唯一事实源。存储层必须提供原子的 `GetOrCreate` 和 compare-and-swap 迁移，且多实例共享同一份状态。业务日志只保存 `session_id` 的密钥化哈希，不保存可被用来枚举或定向路由的原值。

`CapsuleCompatibilityDigest` 与单个 `EnvironmentCapsule.Digest` 不同。前者只覆盖池内必须一致的模型能力、三槽模板版本、重写规则、TLS 策略、出口和上游 allowlist，不包含 credential ID、device ID 等必然随 OAuth 改变的字段。只有前者相同的凭证才能在迁移中互相接管。

逻辑上的 `ClientIdentity` 至少包含：

- 官方客户端家族、CLI 版本和模板版本。
- `User-Agent`、`X-App`、Stainless language/package/OS/arch/runtime/version。
- device ID、client ID、session seed、telemetry user/session ID、terminal type。
- IANA 时区名和与环境匹配的 locale/accept-language。
- system 模板、billing 模板、beta 集合和 cache policy。

`TransportIdentity` 至少包含：

- TLS profile 和 ALPN/HTTP 版本策略。
- 强制 proxy transport；禁止环境变量代理和 direct fallback。
- 精确的 scheme、host、port、path 前缀允许列表。
- DNS 策略和代理连接健康状态。

### 5.2 强制不变量

每次发出请求前必须统一检查以下不变量；任何一项失败都返回网关错误，不能降级直连或透传：

1. 入站 `session_id` 必须由服务端生成或验签，且只有它能作为 OAuth 和环境的粘性调度键。
2. 每个有效 `session_id` 同一时刻只能有一个 active `SessionBinding`。
3. OAuth credential 的 provider 必须是 `claude_oauth`，且 credential 只能关联一个 active Credential Policy。
4. policy 必须恰好包含三个冻结槽位：Windows、macOS、Linux，不能运行时新增第四槽。
5. binding 指定的 credential、environment slot、capsule version 和 egress 必须能解析到唯一 capsule，并与 pool/policy 一致。
6. UA、system、model、客户端 IP、协议类型、请求负载和实时负载不得重新选择已绑定 session 的 OAuth 或环境。
7. target origin 必须精确匹配 allowlist；禁止请求方提供 base URL。
8. transport 必须确认使用 pool/policy 指定的 proxy；代理不可用时拒绝，不能 direct fallback。
9. Authorization 只能由 credential broker 在最后一步注入；入口、插件和日志不可读取 raw token。
10. 最终 Header 中不能存在未列入出站允许列表的字段。
11. 最终 body 中所有 host-owned 字段必须等于 capsule 派生值。
12. body 清洗完成后再生成 billing/CCH；签名后不允许插件继续修改 body。
13. Header、body、TLS、timezone 和 egress 必须来自 binding 锁定的同一个 capsule version。
14. 出站前计算 capsule digest 和 normalization digest，供审计但不记录 token 或完整用户内容。

### 5.3 Session 驱动的 OAuth 与环境选择

首次出现的 `session_id` 没有绑定记录，必须由服务端完成一次初始选择：

1. 验证或签发 `session_id`，并生成用于索引和日志的密钥化哈希。统一显式载体建议为 `X-CLIProxy-Session-ID`。
2. 根据 API key / tenant policy 确定唯一 OAuth pool，不允许请求 body 指定 pool。
3. 以 `HMAC(server_secret, tenant_id || api_key_id || session_id)` 作为内部 binding key，不解析 `session_id` 内容来获取 OAuth 编号或环境值。
4. 以 binding key 对当前可用凭证集做 rendezvous hashing 选择首个 OAuth，并以独立的 domain-separated hash 稳定选择 Windows、macOS 或 Linux 槽。两次选择不得读取请求业务字段或实时负载。
5. 使用存储层的 `GetOrCreate` 原子创建 binding。并发创建冲突时必须放弃本地候选，改用已落库的 binding。
6. 后续所有 HTTP、SSE 和 WebSocket 请求只按 `session_id` 读取 OAuth、槽位和版本，不重新执行负载均衡。

对不能回传服务端签发 session 的旧客户端，可以在通过 API key 认证后接受其原生 session 标识，但只能将它作为上述 HMAC 的一部分，不得直接解析或作为存储主键。严格模式下缺少任何可确认 session 标识的请求必须拒绝，不得使用 request ID 伪造新 session。

各入站协议必须在 API key / tenant 准入验证后经过同一个 `SessionResolver`，再进入 OAuth auth selector 和 executor：

| 来源 | 处理 |
|---|---|
| `X-CLIProxy-Session-ID` | 首选的统一显式载体；验签或纳入认证命名空间后使用 |
| 协议原生 session 字段 | 只由对应 adapter 提取，纳入 tenant/API key 命名空间后使用 |
| WebSocket 后续帧 | 继承握手时锁定的 binding，不允许帧内重新声明 session |
| `metadata.user_id`、UA、IP、request ID | 禁止用作 session 选择来源 |

显式载体和协议原生字段同时存在但不一致时，必须返回 `400 conflicting_session_id`，不得静默选一个。`X-CLIProxy-Session-ID` 仅供入站调度，在出站 Header 重建时必须丢弃。API key / tenant 只用于确定 session 有权进入的 OAuth pool；pool 内 OAuth 和环境的实际选择仍只由 session binding key 决定。

槽位一旦绑定，后续请求试图通过任何入站字段切换环境都应返回 `409 environment_slot_mismatch`。不提供使用 UA 或 OS hint 重新选槽的兼容模式。

### 5.4 OAuth 配额迁移

只有可确认为凭证级请求上限、额度耗尽、凭证撤销或永久失效的结果才能触发迁移。网络超时、代理断线、上游 `5xx`、请求参数错误、上下文超限和未归类的 `429` 都不得切换 OAuth。对带有 `Retry-After` 的临时限流应先冷却，只在确认为 credential-scoped limit 后迁移。

迁移步骤必须是：

1. 将旧 OAuth 标记为 exhausted/revoked 或进入有期限的 cooldown。
2. 在同一 OAuth pool 的稳定凭证环中，从当前 credential 的后继位置开始，选择第一个支持当前模型、处于 `available` 状态且 `CapsuleCompatibilityDigest` 相同的凭证。不得因瞬时负载改变后继顺序。
3. 保留原 `session_id`、`EnvironmentSlot`、`EgressRouteID` 和对外会话语义，换用新 OAuth 自己的同槽 capsule。
4. 使用 `(session_id_hash, old_credential_id, migration_epoch)` 作为 compare-and-swap 条件，原子更新 credential 并使 `MigrationEpoch + 1`。
5. CAS 失败的并发请求必须重读 binding，不得另选第三个 OAuth。

已建立上游连接的请求不得中途迁移。SSE 只有在尚未向客户端输出任何内容时才可以整体重试；一旦输出任何字节就只能结束当前流。WebSocket 在单个连接生命周期内固定 OAuth，断线后的新连接再读取最新 binding。

凭证级上限必须由集中的 `CredentialLimitClassifier` 根据已测试的 provider 状态码、错误代码和响应特征判定。未知 `403`/`429` 默认为不可迁移，不得由 executor 各自写模糊字符串匹配。单个入站请求最多允许一次配额迁移后的整体重试，并必须在尚未对客户端产生可见输出时执行，防止无界换号和重放。

### 5.5 七阶段请求管线

```text
1. Admission
   鉴权、客户端/路径/方法/媒体类型/大小限制

2. Policy resolution
   API key → tenant → OAuth Pool Policy

3. Binding resolution
   verified session_id → atomic GetOrCreate SessionBinding → OAuth credential + environment slot + capsule

4. Canonical body normalization
   解析允许的业务字段；丢弃所有 host-owned 字段；生成固定环境字段

5. Outbound reconstruction
   从空 Header map 创建请求；写入 capsule Header；最后注入 OAuth

6. Finalize and verify
   beta/body 对称清洗 → billing/CCH → invariant validator → 冻结请求

7. Bound transport
   capsule TLS profile + policy proxy + exact upstream allowlist
```

插件拦截器若保留，只能位于阶段 4 之前处理业务内容，或位于响应阶段；不能在阶段 6 之后修改请求。

## 6. 字段清洗与覆写合同

### 6.1 Header

应从空 Header map 构建，以下入站类别一律不复制：

| 类别 | 示例 | 策略 |
|---|---|---|
| 认证 | `Authorization`、`X-Api-Key`、Cookie | 丢弃；OAuth broker 最后注入 |
| 路由 | `Host`、forwarded 系列、CF/CDN 系列 | 丢弃；由 target/transport 生成 |
| 客户端环境 | `User-Agent`、`X-App`、所有 `X-Stainless-*` | 丢弃；从 capsule 重建 |
| 会话身份 | `X-Claude-Code-Session-Id`、traceparent、baggage | 丢弃；从服务端 session binding 生成 |
| 能力 | `Anthropic-Beta`、`Anthropic-Version` | 丢弃；由模型、模板和 capsule 计算 |
| 连接 | `Connection`、`Proxy-*`、`TE`、`Upgrade` | 丢弃；由 Go transport 管理 |
| 表示 | `Content-Type`、`Accept`、`Accept-Encoding` | 使用服务端固定允许值 |
| 语言 | `Accept-Language` | 使用 capsule 固定 locale，不信任入站值 |

这比“先复制再覆盖已知字段”更安全，因为未来出现的新客户端指纹 Header 默认不会泄漏。

### 6.2 Body

Body 应先解析为受支持的 Anthropic Messages schema，再分别处理业务字段和 host-owned 字段。

必须丢弃并重建：

- `system` 中的客户端 billing/环境标识；用户的合法系统指令按明确策略迁移或拒绝，不能和官方身份模板混在一起。
- `metadata.user_id` 及 metadata 中所有环境、设备、账号、session、timezone 字段。
- 客户端提供的 billing header、CCH、CLI version 和 fingerprint。
- 与最终 beta 不兼容的字段。
- 能暴露第三方运行环境的未知 `client_metadata` 字段。

可以保留但必须 schema 校验：

- `messages` 的用户内容和合法 content block。
- `tools`、`tool_choice`，但要执行名称映射并保存响应反向映射。
- `model`，但必须经过凭证 policy 的模型 allowlist 和服务端映射。
- `max_tokens`、`stream`、thinking 等业务能力字段，但须经过上游能力校验。

不得用字符串全局替换来“清理环境”，因为用户消息正文可能合法包含 `User-Agent`、系统名或时区文本。

### 6.3 时区

时区不是标准 HTTP 客户端身份字段。要保持一致，应同时控制：

- capsule 的 IANA timezone，例如 `America/Los_Angeles`，不要只存 `UTC-8`。
- 所有服务端生成的本地时间、telemetry、feature flag 和 turn metadata。
- 与时区匹配的 locale、终端类型和系统模板（如果协议确实发送这些信息）。
- 测试时钟；夏令时切换必须由 IANA 数据库处理。

不要伪造不存在的上游 Header。没有协议字段承载时区时，只保证网关生成数据的一致性即可。

## 7. OAuth 凭证与固定出口

### 7.1 凭证最小权限

raw OAuth token 不应放入通用 request context。推荐引入 `CredentialLease`：

```text
Executor 获得 opaque lease
  → lease 携带 session binding hash / credential ID / migration epoch
  → Finalizer 请求一次性 Authorization 注入
  → BoundTransport 校验 binding/policy/capsule/target
  → 发送后 lease 失效
```

在 Authorization 注入前必须重新确认 lease 中的 credential 和 migration epoch 仍与 binding 一致；如果发生迁移，旧 lease 立即失效。这样 translator、普通插件、日志和入站 handler 都拿不到 token。Claude OAuth credential 也不能被通用 OpenAI/自定义 provider executor 复用。

### 7.2 出口绑定

固定出口必须同时在应用层和网络层实施：

- 应用层：每个 OAuth Pool Policy 只有一个 `EgressRouteID`，pool 内所有 Credential Policy 必须一致；transport 创建时必须显式解析该 route。
- Transport 层：禁用 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` 的隐式影响；不允许代理失败后回退直连。
- 目标层：只允许 `https://api.anthropic.com` 及明确批准的 OAuth/模型端点；重定向必须重新校验或直接禁止。
- 网络层：容器/主机防火墙只允许进程连接指定代理，阻止直接连接公网 443。
- 代理层：代理账户或 mTLS 身份绑定 credential policy，记录实际出口 IP。
- 健康检查：通过同一 transport 验证代理和上游，不使用另一条直连探针冒充健康。

只有应用层 proxy 配置不能证明“统一出口”，因为错误配置、环境变量、重试 fallback 或其他 HTTP client 都可能绕过它。

## 8. 配置示例

下面是建议的配置形态，不代表当前项目已支持：

```yaml
claude-oauth-pools:
  - id: "claude-pool-01"
    strict: true
    credentials:
      - "claude-oauth:account-01"
      - "claude-oauth:account-02"
      - "claude-oauth:account-03"
    capsule-compatibility-digest: "sha256:example"
    upstream-allowlist:
      - "https://api.anthropic.com:443"
    egress-route: "proxy:claude-fixed-01"
    session-binding:
      store: "shared"
      ttl: "24h"
      selection-key: "verified-session-id"
      migration: "credential-limit-only"
    capsule-set:
      version: 1
      slots:
        - environment: windows
          identity-template: "claude-code/windows/2.1.x"
          timezone: "America/Los_Angeles"
        - environment: macos
          identity-template: "claude-code/macos/2.1.x"
          timezone: "America/Los_Angeles"
        - environment: linux
          identity-template: "claude-code/linux/2.1.x"
          timezone: "America/Los_Angeles"
```

生产配置中应保存解析后的完整 capsule 和 digest，不能在每次请求时从松散默认值临时拼装。`SessionBinding` 是运行时状态，不写回配置文件；应保存在具备唯一约束、TTL 和 CAS 能力的共享存储中。

## 9. 状态、版本与更新

- `SessionBinding` 的建议主键是 `session_id_hash`，并维护 `credential_id` 反向索引以支持凭证撤销和迁移。多实例不得使用仅进程内粘性表。
- binding 在 TTL 内持续续期；过期后当作新 session 重新选择。正在进行的 SSE/WebSocket 不得因 TTL 到期中途切换。
- Capsule Set 采用 copy-on-write 版本。正在进行的 session 固定旧版本，新 session 使用新版本。
- 官方 CLI 身份目录只能由管理员或签名更新包更新，不能从普通流量自动学习。
- 更新前验证三个槽位完整、字段交叉一致、TLS profile 存在、proxy route 可用、上游 allowlist 非空。
- 更新必须原子切换整个 capsule set，不能只更新 UA 而保留旧 package/runtime 版本。
- 保留上一个可用版本用于显式回滚；运行时不能因当前版本错误自动降级到客户端 Header。
- OAuth refresh token 更新不改变 capsule identity；credential rotation 产生新的 Credential Policy，并通过与配额迁移相同的 CAS 机制迁移原凭证上的 active bindings。
- OAuth 状态至少区分 `available`、`cooldown`、`exhausted`、`revoked` 和 `unhealthy`。只有 `exhausted`/`revoked` 可以直接触发 session 迁移；`cooldown` 必须保留恢复时间。

## 10. 可观测与审计

每个请求至少记录以下脱敏字段：

- request ID、session ID 的密钥化哈希、OAuth pool/policy ID 的哈希、credential ID 的哈希、slot、capsule version/digest。
- binding 结果：`created`、`reused` 或 `migrated`，以及 migration epoch 和脱敏迁移原因。
- normalization 结果：删除了哪些字段类别、覆盖了哪些 host-owned 字段，不记录字段原值。
- target origin、egress route ID、代理连接结果、可选的实际出口 IP。
- TLS profile、CLI identity template version、CCH 是否在最终阶段生成。
- invariant validator 结果和拒绝原因。

禁止记录 OAuth token、完整 Header、`metadata.user_id` 原值、完整 system/messages 或代理密码。

## 11. 失败策略

严格模式下全部 fail closed：

| 故障 | 行为 |
|---|---|
| `session_id` 缺失、伪造或无法验签 | `401 invalid_session_id` |
| 显式与协议原生 session ID 冲突 | `400 conflicting_session_id` |
| session binding 存储不可用 | `503 session_binding_unavailable`，禁止退化为无粘性调度 |
| capsule 缺失或校验失败 | `503 environment_capsule_unavailable` |
| 请求试图切换已绑定槽位 | `409 environment_slot_mismatch` |
| 凭证上限但 pool 内没有兼容 OAuth | `503 compatible_oauth_unavailable`，保留原 binding |
| session 迁移 CAS 冲突 | 重读最新 binding，不创建第二条迁移链 |
| 已输出部分 SSE 后凭证报错 | 结束当前流，不自动切换并重放 |
| proxy 不可用 | `503 required_egress_unavailable`，禁止直连 |
| target 不在 allowlist | `502 upstream_policy_violation` |
| body 无法按 schema 解析 | `400 invalid_request_schema` |
| 最终不变量失败 | `500 outbound_normalization_failed`，不发送请求 |
| OAuth lease 不匹配 policy | `403 credential_scope_violation` |
| capsule 版本更新中 | 使用已固定的旧版本完成当前 session，或在建立 session 前短暂拒绝 |

不要在 403 后随机修改工具名、UA、OS 或代理反复试探。这会破坏固定环境承诺，也难以审计。

## 12. 测试与完成标准

### 12.1 单元测试

- 任意大小写和重复形式的环境 Header 都被删除。
- 所有入站 adapter 都将显式/原生 session 字段解析为同一 canonical binding key；冲突字段必须拒绝。
- `metadata.user_id`、UA、IP 和 request ID 不能成为 session 回退来源。
- 未知 Header 默认不进入出站请求。
- 三个槽位生成互不冲突、稳定且可复现的 device/session 身份。
- 入站 UA、OS、arch、runtime、timezone、metadata 和 billing 均不能影响 capsule 输出。
- beta 清洗发生在 CCH 之前，签名后 body 不再变化。
- 同一 `session_id` 在任意请求内容和协议形态下始终复用同一 OAuth、槽位和 capsule version。
- 并发首次请求只能创建一个 binding。
- session 已绑定后不能跨槽。
- 只有凭证级 limit/revocation 分类能触发迁移；网络错误、`5xx`、参数错误和临时 `429` 不得改变 binding。
- 迁移保留 `session_id`、environment slot 和 egress，切换到新 OAuth 的同槽 capsule，且 migration epoch 只增加一次。
- 多个并发 limit 响应只能产生一次成功迁移。
- capsule digest 对任一环境字段变化都敏感。

### 12.2 集成测试

对每个槽位发送至少三类请求：真实 Claude Code 形态、第三方兼容形态、恶意冲突字段形态。三者的最终环境快照必须完全相同，只有允许的业务内容和服务端会话值可以不同。

必须覆盖：

- HTTP 非流式与 SSE 流式。
- WebSocket 连接期间固定 OAuth，重连后使用最新 binding。
- OAuth refresh 前后。
- 同一 session 跨 HTTP/SSE/WebSocket 重用同一 binding。
- 凭证配额耗尽后迁移到同池兼容 OAuth，保留 session 和环境槽。
- 配额迁移的并发 CAS 竞争，以及无兼容后继 OAuth 时的 fail-closed 行为。
- SSE 已输出首字节后禁止自动迁移重放。
- proxy 正常、超时、认证失败和断线。
- 上游 30x 重定向到未允许 host。
- 多实例同时加载和更新 capsule set。
- 多实例并发创建、读取和迁移同一 session binding。
- 插件试图在 finalization 后修改 Header/body。
- 环境变量设置了其他 `HTTP_PROXY`/`HTTPS_PROXY` 时仍只能走 policy proxy。

### 12.3 网络验收

- 在代理端确认三个槽位的公网出口完全相同。
- 在主机或容器防火墙确认服务进程不能直连 Anthropic 443。
- 抓取网关到代理的流量，确认没有第二条 transport 或 fallback。
- 对最终出站快照做黄金测试，但所有 token 和用户内容必须脱敏。

### 12.4 完成标准

只有同时满足以下条件才算完成：

1. 一个 Claude OAuth credential 只能解析到一个 policy 和恰好三个 capsule。
2. 一个有效 `session_id` 在任意时刻只能解析到一个 active binding；常规请求始终复用其 OAuth 和环境。
3. 任意入站环境字段、业务负载或协议形态都不能改变已绑定 session 的 OAuth 和最终环境快照。
4. 只有确认的凭证级上限、撤销或永久失效可以触发迁移；迁移原子保留 session 和环境槽，且并发下只发生一次。
5. pool 内所有可接管 OAuth 均通过同一胶囊兼容性校验，并使用同一强制 egress route。
6. 代理故障时零次直连尝试。
7. Claude OAuth token 无法进入其他 provider executor 或自定义 target。
8. 单元、集成、网络验收和黄金快照测试全部通过。

## 13. 建议落地顺序

### Phase 1：建立强制边界

- 定义 `OAuthPoolPolicy`、`SessionBinding`、`CredentialPolicy`、`EnvironmentCapsule`、`TransportIdentity` 和 invariant validator。
- 限制 Claude OAuth credential 只能进入 Claude executor 和固定 Anthropic origin。
- 将 proxy 变为必需依赖并禁止 direct fallback。

### Phase 2：Session 粘性与 OAuth 迁移

- 实现可共享的 binding store，提供 TTL、唯一创建、credential 反向索引和 CAS 迁移。
- 统一所有入站协议的 `session_id` 提取、验签和哈希，并使 binding 成为 OAuth/环境调度的唯一事实源。
- 实现凭证状态分类、同构 OAuth 池选择和配额耗尽时的单次原子迁移。
- 先完成并发首次绑定、并发迁移、非配额错误不漂移和无兼容后继凭证的 fail-closed 测试。

### Phase 3：三槽位与请求重建

- 移植 `claude-codex-gateway` 的 v2 三槽位模型。
- 把选槽从 UA 探测改成首次 binding 的服务端策略；已绑定 session 不再重新选槽。
- 从空 Header map 重建；body 采用 typed schema 清洗。

### Phase 4：身份一致性

- 把 system、billing、CCH、beta、metadata、telemetry、timezone 和 TLS 全部纳入 capsule。
- 取消从普通客户端学习 Header；只允许管理员更新身份目录。
- 把最终签名放在所有 body 修改之后，并冻结请求。

### Phase 5：审计与故障验证

- 增加 binding result/migration epoch、normalization report、capsule digest、egress route 审计。
- 完成 SSE/WebSocket 迁移边界、proxy 故障、重定向、跨 executor 和插件篡改测试。
- 用抓包与代理日志验证固定出口，而不是只检查配置值。

## 14. 最终取舍

不建议直接照搬任一目录：

- 只用 `claude-codex-relay-reference` 会停留在 Header 学习和请求伪装，无法形成三系统、时区、TLS、凭证和出口的一致边界。
- 只用现有 `cpa` Cloak 会继续保留请求级开关和分散配置，无法证明所有请求都被强制规范化。
- 直接使用 `claude-codex-gateway` 已最接近目标，但仍需去掉可选 profile、客户端驱动选槽、passthrough 和未绑定出口这四个缺口。

因此，最小风险路线是：**CLIProxyAPI 作为执行底座 + session_id 驱动的 OAuth/环境粘性绑定 + 同构 OAuth 池的原子配额迁移 + gateway v2 三槽模型 + relay 的准入/白名单思想 + 凭证级 Environment Capsule 与 Bound Transport。**

## 实现纠正（2026-07-13）

环境胶囊归属 **凭证**，不是 OAuth 池配置项：

- 仅 `Claude OAuth` / `Codex CLI OAuth` / `Grok CLI OAuth` 三类凭证自动生成并绑定 Windows/macOS/Linux 三个环境胶囊。
- `admin/claude-oauth-pools` 仅保留互迁、出口、shadow/enforce 策略；禁止手建池级胶囊。
- SetupToken、APIKey 及其他平台不进入自动胶囊路径。

