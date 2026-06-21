# 子1 设计：反代真实性增强

> 配套 prd：`prd.md`。本文件聚焦技术设计：边界、契约、数据流、权衡、兼容性、灰度/回滚。
> 已整合 codex 交叉验证（gpt-5.5）must-fix，见末尾「修订记录」。
> 三个能力相对独立，可分别实现、分别灰度，故按 R1/R2/R3 分节设计。

## 总体边界

- 新增一个轻量 package：`internal/pkg/clientidentity`（暂定名），统一承载「客户端身份快照」概念：
  - **家族（family）枚举**：`codex-cli` / `codex-desktop` / `claude-cli` / `claude-desktop`。
  - 每个家族解析为一个**完整 identity snapshot**：`{ Headers(UA + X-Stainless-* + X-App + anthropic-beta 组合), VersionFields(cli/sdk/cc_version 及 fp), TLSProfileName }`。
- **核心架构原则（codex-D）**：family → snapshot 的解析放在**上层 resolver**，header 注入点、billing block、identity fingerprint、TLS profile 选择**全部从同一个 resolver 取值**，从结构上杜绝「header 切了 family、TLS/billing 仍是旧默认」的错配。
- 该 package **不依赖** payment / subscription（满足精简版可用约束）。
- 复用既有底座：`pkg/tlsfingerprint`（保持纯 TLS 执行层）、既有 `ResolveTLSProfile(account)` 路径、`model.TLSFingerprintProfile`、`pkg/claude/constants.go`、`pkg/openai`、`service/identity_service.go`。

---

## R1 — UA 自动拉取（codex + claude）

### A. 联动落点全集（codex-A：不止 DefaultHeaders）
版本/身份字段硬编码分散在多处，**必须由同一 snapshot 统一驱动**，否则半更新会更易被判第三方：

| 落点 | 文件:行 | 联动内容 |
|------|---------|----------|
| Claude 默认头 | `pkg/claude/constants.go:68,91` | `CLICurrentVersion`、`User-Agent`、`X-Stainless-Package-Version`、`X-Stainless-Runtime-Version`、`X-Stainless-OS/Arch`、`X-App` |
| Claude billing 归因 | `service/gateway_billing_block.go:73` | `cc_version=X.Y.Z.{fp}`、`cc_entrypoint=cli`、`cch` 占位与**签名顺序**（须在 body 最终修改后签名，见 `gateway_service.go:6711`） |
| Claude 模板替换 | `service/gateway_service.go:4322` | 硬读 `claude.CLICurrentVersion` |
| 默认 identity 指纹 | `service/identity_service.go:28` | 硬编码同一组 Stainless 字段 |
| Codex 客户端身份 | `service/openai_gateway_service.go:45,1268,71` | `codexCLIUserAgent`、`codexCLIVersion`、`originator` 默认值、`session_id`/`conversation_id`/`x-codex-turn-state`/`x-codex-turn-metadata`/`OpenAI-Beta` 透传白名单 |

设计：把 **Claude identity snapshot** 与 **Codex identity snapshot** 作为两个完整快照对象，上述落点改为读快照（缺省回退现有硬编码常量）。

### B. 数据源（codex-B 修正）
| 目标 | 来源 | 注意 |
|------|------|------|
| claude-cli 版本 | npm `@anthropic-ai/claude-code` dist-tags.latest | 仅作版本提示 |
| claude SDK 版本 | **优先**解析 claude-code 发布包内实际依赖的 `@anthropic-ai/sdk` 版本；npm sdk@latest 仅兜底 | CLI 未必用 sdk latest，直接拉 latest 有超前/滞后风险 |
| codex 版本 | github releases `openai/codex` / npm `@openai/codex` | 真实伪装对象是 `codex_cli_rs/<ver>`（见 `openai_gateway_service.go:45`），非泛 `@openai/codex` |

解析陷阱（必须处理）：release tag 带 `v`/预发布、npm latest 与 github latest 不同步、UA 需 OS/arch/terminal 后缀（非纯 semver）、`originator` 与 UA 前缀须同族否则 `IsCodexOfficialClientByHeaders`（`pkg/openai/request.go:12`）误判。
**真实指纹/头以抓包校验为准**，自动拉取只负责版本号，头模板的非版本部分仍以人工核验的内置模板为准。

### 组件
- `VersionFetcher`（service）：后台周期任务，产出 claude-cli / claude-sdk / codex 版本。
- `IdentityRegistry`（clientidentity）：`atomic.Pointer` 持有当前 snapshot（claude + codex 两族基线），只读 getter。
- 原子一致性（硬约束）：一次更新 = 一个完整一致 snapshot；任一字段缺失则整体丢弃保留旧值/兜底，**禁止半更新**。
- 不在请求热路径联网；fetch 在后台 goroutine，带超时/重试/频率限制。

### 开关与回滚
- config `gateway.ua_auto_fetch.{enabled(默认false),interval,sources}`；admin setting 暴露生效版本/来源/最后状态。
- 关闭开关 = 回退硬编码常量（今日行为）。

---

## R2 — codex websocket 一键全开

### 「全开目标态」完整字段清单（codex-C 修正：补齐分类型 enabled）
写 `accounts.extra`，以 `service/account.go` 解析方法判定为准：

| 字段 | 目标值 | 必要性 |
|------|--------|--------|
| `openai_oauth_responses_websockets_v2_enabled` | `true` | **必须**（分类型 enabled 优先级最高，已有 false 会覆盖通用 true） |
| `openai_oauth_responses_websockets_v2_mode` | `ctx_pool` | OAuth 模式 |
| `openai_apikey_responses_websockets_v2_enabled` | `true` | **必须**（同上） |
| `openai_apikey_responses_websockets_v2_mode` | `ctx_pool` | API Key 模式 |
| `responses_websockets_v2_enabled` | `true` | 兼容总开关 |
| `openai_ws_enabled` | `true` | 历史总开关 |
| `openai_ws_allow_store_recovery` | `true` | store 恢复 |
| `openai_ws_force_http` | `false` | 不强制降级 |

- `ctx_pool`/`passthrough` 均为有效值（`account.go:1338` 归一）；`shared/dedicated` 兼容归并 ctx_pool。
- **前置校验**：全局 `gateway.openai_ws.responses_websockets_v2` 若被禁用，账号 extra 无法单独开启 → enable-all 前需检查全局开关并在响应中提示。
- OAuth / API Key 账号类型分别处理（resolver 分类型读取，见 `account.go:1419`）。

### 接口
- admin：`POST /admin/accounts/:id/ws/enable-all` + `.../ws/reset`；service `EnableAllOpenAIWS` / `ResetOpenAIWS`，幂等，仅 OpenAI 账号。

---

## R3 — 家族化 UA↔TLS 配对指纹库

### 落点（codex-D 修正：family 放上层 resolver，不进 dialer）
- `clientidentity` 解析 `account.extra.client_family` → 返回完整 snapshot `{headers, version fields, tls profile name}`。
- **outbound request builder（header/billing/identity）与 TLS profile 选择都从这个 resolver 取值**。
- `tlsfingerprint.NewDialer(profile,...)`（`dialer.go:119`）保持低层执行，**不感知 family**；family→profile 在上层（复用/对接现有 `ResolveTLSProfile(account)`，`account_usage_service.go:1165`）。
- 默认 TLS 注释为 Node.js/Claude Code 指纹（`dialer.go:55`），**不可拿它覆盖 codex family** —— codex family 必须有自己的 profile。

### 模型与默认
- 内置 ≥4 family 预设（codex-cli/desktop、claude-cli/desktop），各含 UA 模板（版本由 R1 snapshot 填充）+ 专属 `tlsfingerprint.Profile`。
- 缺省 `client_family` → 维持现状（claude 账号=现有 claude 指纹、codex 账号=现有 codex 指纹），**默认行为不变**。

### 真实指纹数据（implement 数据依赖）
- 各 family 真实 TLS 指纹（cipher/curve/ext 顺序、JA3）+ desktop UA 字面值需**抓包采集**，落为 Profile 常量/迁移，注明来源与采集日期。design 只定结构与配对契约，不编造 JA3。

---

## 跨能力数据流

```
启动 → IdentityRegistry 载入内置兜底 snapshot(claude+codex)
  └ (R1 开启) VersionFetcher 周期拉取版本 → 校验完整 → 原子 swap
请求 → resolver(account.extra.client_family) → snapshot
  ├ header 注入：snapshot.Headers (UA/X-Stainless/X-App/anthropic-beta)
  ├ billing block：snapshot.VersionFields (cc_version/fp/cch 顺序)
  ├ identity fingerprint：snapshot.Headers (Stainless 组)
  └ TLS dialer：snapshot.TLSProfileName → tlsfingerprint 执行
账号管理 → 一键全开 ws (R2) 批量写 extra(8 字段)
```

## 测试策略
- R1：版本解析（mock npm/github）、**全落点联动一致性**（UA/cc_version/identity/codex 同步）、失败回退、开关关闭=常量。
- R2：enable-all 后各解析方法返回期望值（含分类型 enabled）、已有 false 被纠正、全局禁用时拒绝、reset 回默认、幂等。
- R3：family→snapshot 配对断言（header 与 tls profile 同族）、默认（无 family）回归、可选 dialer 抓包集成测试。

## 风险与权衡
- 半更新比不更新更危险 → snapshot 原子化 + 全落点统一读取。
- 错误指纹比不改更易被识别 → 预设先少而准，以抓包为准。
- 联网在后台，绝不入请求热路径。

## 修订记录（codex 交叉验证采纳）
- A：identity 联动扩展到 billing block / identity_service / codex headers，统一 snapshot 驱动。
- B：数据源修正（codex_cli_rs 真实族、sdk 版本优先从 claude-code 包解析、UA 后缀/ originator 同族）。
- C：ws 全开补齐两个分类型 enabled 字段 + 全局开关前置校验。
- D：family resolver 上移，tlsfingerprint 保持低层。
