# 反代真实性增强：UA 拉取 / codex-ws 全开 / 家族化 UA↔TLS 指纹

## Goal

让 sub2api 发往上游官方端点的请求更贴近**真实官方客户端**，降低被判第三方的风险。三个子能力构成一个连贯的「客户端身份」体系：

1. **UA 自动拉取**（codex + claude）：从官方发布源识别最新客户端版本，自动更新对外伪装的 UA 及其联动指纹字段。
2. **codex websocket 一键全开**：admin 侧一次操作即可为账号开启整组 websocket 长连接能力。
3. **家族化 UA↔TLS 配对指纹库**：为真实客户端家族（codex-cli / codex-desktop / claude-cli / claude-desktop）分别提供真实 TLS 指纹，且 UA 与 TLS 按「产品家族 + 变体」成对一致。

## 背景与现状（已调研）

- **UA 现状**：硬编码常量。`pkg/claude/constants.go` 的 `CLICurrentVersion = "2.1.161"`、`User-Agent: claude-cli/<ver> (external, cli)`；codex 侧 `repository/openai_oauth_service.go` 用 `codex-cli/0.91.0`。注释明确警告：**UA 版本必须与 `X-Stainless-Package-Version`、`cc_version` 等一组指纹字段严格一致，不一致会被 Anthropic 判第三方**。→ UA 拉取不是改单个字符串，而是改**一组联动字段**。
- **codex ws 现状**：账号 `Extra` 上一组开关由 `service/account.go` 解析：`openai_ws_enabled`、`responses_websockets_v2_enabled`、`openai_oauth_responses_websockets_v2_enabled`、`openai_apikey_responses_websockets_v2_enabled`、对应 `*_mode`、`openai_ws_force_http`、`openai_ws_allow_store_recovery` 等。底层用 `coder/websocket`。
- **TLS 现状**：`pkg/tlsfingerprint/dialer.go` 用 utls + `HelloCustom`，从 `Profile`/DB `model.TLSFingerprintProfile` 构造 ClientHello；默认模拟 Claude Code（固定 JA3 `44f88fca...`）。已有 admin `tls_fingerprint_profile_handler.go` 管理 profile。→ 已具备「按 profile 定制指纹」的底座，需扩展为**家族化预设 + 与账号/UA 的配对绑定**。

## Requirements

### R1 — UA 自动拉取（codex + claude）
- 从官方发布源（GitHub release / npm 等）周期性识别 codex CLI 与 claude CLI 的最新版本号。
- 拉取到新版本后，更新对外 UA 及**全部联动指纹字段**，保证版本号在 UA / X-Stainless-* / cc_version 等处严格一致。
- 必须可关闭（开关）并有**内置兜底版本**：联网失败 / 解析失败时回退到内置默认，绝不发出残缺或不一致的头。
- 拉取结果可观测（当前生效版本、来源、最后更新时间、上次失败原因）。

### R2 — codex websocket 一键全开
- 提供一个「一键全开」入口（admin 操作或账号级开关），一次性把目标 codex 账号整组 `openai_*ws*` Extra 字段置为启用/期望模式。
- 明确「全部 ws 功能」覆盖的字段集合与各自取值（在 design.md 固化清单）。
- 操作幂等、可回退（能一键关回默认）。

### R3 — 家族化 UA↔TLS 配对指纹库
- 内置真实客户端家族预设：至少 codex-cli / codex-desktop / claude-cli / claude-desktop，各自包含「真实 UA（族）」+「真实 TLS 指纹」。
- UA 与 TLS 指纹**成对一致**：选定某家族变体后，该连接发出的 UA 头与 TLS ClientHello 同属该家族，不出现「codex desktop 的 UA 配 claude cli 的 TLS」这类错配。
- 复用现有 `tlsfingerprint.Profile` / `model.TLSFingerprintProfile` 底座；新增的是**家族预设 + 配对关系**，不是另起炉灶。
- 选择/绑定方式（账号级 or profile 级 or 全局默认）在 design.md 确定；**默认保持现有 Claude 指纹行为不变**。

## 约束

- 默认（不开启任何新开关）行为与今日一致：完整版现有 UA / 指纹 / ws 解析逻辑不被破坏。
- 本能力在**完整版与精简版**下都要可用，因此实现**不得依赖 payment / subscription 模块**。
- UA 联动字段一致性是硬约束：任何自动更新都要么整组一致更新，要么整组回退兜底，禁止半更新。
- 联网拉取需有超时、失败重试与频率限制，不阻塞启动与请求热路径。
- 遵循仓库现有分层（handler / service / repository / pkg）与 wire 注入约定。

## Acceptance Criteria

- [ ] R1：开启自动拉取后，能在不重启的情况下把 codex/claude UA 及联动字段更新到最新版本；联网失败时稳定回退内置兜底且日志可见；关闭开关后行为回到硬编码默认。
- [ ] R1：存在测试覆盖「版本解析 + 联动字段一致性 + 失败回退」。
- [ ] R2：对一个 codex 账号执行「一键全开」后，`service/account.go` 各 ws 解析方法返回期望启用态；执行「一键关回」后恢复默认；操作幂等。
- [ ] R3：内置 ≥4 个家族变体预设（codex-cli/desktop、claude-cli/desktop），每个变体 UA 与 TLS 指纹成对；选定变体后实际发出的 UA 与 ClientHello 同族（有测试/抓包验证手段）。
- [ ] R3：默认配置下指纹与 UA 行为与改动前完全一致（无回归）。
- [ ] 完整版与精简版均能编译并启用本能力；全量 `go test ./...`（完整版）通过。

## 开放问题（design 阶段解决）

- UA 发布源的确切端点与解析方式（GitHub API vs npm registry vs 官方下载页），以及联动字段（X-Stainless-* 等）如何随版本推导。
- 家族变体的「真实 TLS 指纹」数据如何获得与维护（抓包采集 / 已知 JA3 库 / 内置常量）。
- 家族变体的选择粒度：账号级绑定 / profile 扩展 / 请求级路由。

## Notes

- 复杂任务：本 prd 之后需补 `design.md`（技术设计）与 `implement.md`（执行清单）方可 `task.py start`。
