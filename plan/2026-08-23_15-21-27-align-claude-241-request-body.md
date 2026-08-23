---
mode: plan
task: 对齐官方 Claude Code 2.1.241 请求体
created_at: "2026-08-23T15:21:27+08:00"
complexity: complex
---

# Plan: 对齐官方 Claude Code 2.1.241 请求体

## Goal

- OAuth / SetupToken mimic 出站体在第一方判定相关字段上对齐 `vm-29-official-20260823`（官方 CLI 2.1.241）。
- 真实 Claude Code 2.1.241 入站仍透传、不被 mimic 改写。
- 不把官方工具表和会话专属长提示词整段灌进被代理请求。

## Scope

- In:
  - 本仓库 `X:/project/sub2api` 的 OAuth / SetupToken mimic 请求体与默认头。
  - billing 构造、`cc_version` 指纹取样、beta 列表、Settings 默认身份句。
  - 相关单测与过时注释。
  - 生产 KIN unofficial `rewrite` persona 作为 Phase 3 跟进（Node only）。
- Out:
  - 不把官方 Agent / Bash / Edit 工具表写进 mimic。
  - 不覆盖客户端已有的 `thinking` / `output_config` / `context_management`。
  - 默认不注入官方 system[2] 全文（`# Doing tasks` / `# Using your tools`）和 system[3]（memory / Environment / `<total_tokens>`）。
  - 不改入站 validator 判定逻辑（Agent SDK 句与任意 `cc_entrypoint=` 已覆盖）。
  - 不重建 / 不重启 KIN Go worker。

## Assumptions / Dependencies

- 抓包路径 `kin-29 → MITM → SOCKS → api.anthropic.com`，未走 kin-worker / `:8787`，代表当前官方主路径。
- 最新上游 `Wei-Shaw/sub2api`（`X:/temp/sub2api` @ `d45135d87`）没有对应注入改动；要对齐只能本仓自改。
- 官方主对话现在是 4 块 system。历史选择是只留中性扩写，避免污染被代理行为和 CRS `refusal`。本次跟协议指纹，不跟整本说明书。
- 现有指纹盐 `59cf53e54c78` + 第 4/7/20 字符仍然有效。主对话官方后缀 `134` 对可见用户句命中；对首个 `<system-reminder>` 块会算成错误值。Web Search 子请求官方后缀 `567` 已直接命中。
- `metadata.user_id` 在 CLI `>= 2.1.78` 已是 JSON；版本升到 `2.1.241` 后 `FormatMetadataUserID` 会自动走 JSON。
- `cch` 旧实现是 body xxHash64 的 5 位 hex。恢复前必须用抓包 01–05 五个值离线验算；对不上就先只改 `cc_entrypoint`，不写假 `cch=00000`。

## Phases

1. Phase 1 — P0 协议指纹（本仓先做）
   - 身份句改为 `You are a Claude agent, built on Anthropic's Claude Agent SDK.`（`claudeCodeSystemPrompt` + Settings 默认值）。
   - `CLICurrentVersion = "2.1.241"`；UA `claude-cli/2.1.241 (external, sdk-cli)`；stainless 默认 `0.112.1` / node `v26.3.0`。`X-App` 保持 `cli`。不把 arch 写死为 `x64`。
   - billing：`cc_entrypoint=sdk-cli`；验算通过后恢复 `cch=`；主对话写会话稳定 `cc_prompt_id`；第 2 轮起写 `cc_prev_req`（上一轮上游 `req_...`）。Web Search 类短请求只留 `cc_version + entrypoint + cch`。
   - `extractFirstUserText`：第一条 `role=user` 中跳过以 `<system-reminder>` 开头的 text block，取第一段可见用户文本；不够再回退旧逻辑。
   - `FullClaudeCodeMimicryBetas()` 按抓包顺序补 `thinking-token-count-2026-05-13`、`mid-conversation-system-2026-04-07`、`advanced-tool-use-2025-11-20`（仅主对话 / 有工具时）。不加 `redact-thinking`。
   - 中性扩写前导 `\n`；`cache_control` 改为 `{type:ephemeral, ttl:1h, scope:global}`。
2. Phase 2 — P1 体形态（默认保持 3 块）
   - 默认仍是 `[billing][Agent SDK 身份][中性扩写 + 1h/global]`。
   - 仅当 Phase 1 上线后仍被判第三方时，再加第 4 块短 `# Text output`（只要开头那段）。仍不注入 memory / Environment / 假 `total_tokens`。
3. Phase 3 — KIN unofficial rewrite（生产 Node）
   - 同步 Phase 1 的身份句、UA `sdk-cli`、`cc_entrypoint=sdk-cli`、beta、`cch` / `cc_prompt_id`。
   - rewrite 仍 3 块；`persona_park` 继续把调用方 system 挂到首条 user 后的 `role:system`。
   - 只 scp + `node --check`。不 `systemctl restart`，除非明确要求。更新 `kin-gateway-ops` protocol / architecture 文案。

## Tests & Verification

- 身份句 / `sdk-cli` / 版本 2.1.241 -> 更新 `gateway_prompt_test`、`gateway_anthropic_apikey_passthrough_test`、Settings 默认值断言。
- 指纹取可见 user 文本 -> 用抓包 01 两段 content：reminder 不得算出 `134`，可见句必须算出 `134`；03 必须算出 `567`。
- `cch` 算法 -> 先对 01–05 五个官方 body 离线验算，通过后再写单测；失败则本项延期。
- `cc_prompt_id` 同会话稳定、`cc_prev_req` 第二轮出现 -> 新增 mimic 多轮单测。
- beta 顺序与「无 redact-thinking」 -> `gateway_beta_test`。
- 真实 CC 2.1.241 透传不改写 -> 保持现有 billing + metadata 检测。
- 上线后对照 -> 非官方再抓一包核身份 / UA / billing / beta；官方 VM 再跑一遍确认透传。

## Issue CSV

- Path: issues/2026-08-23_15-21-27-align-claude-241-request-body.csv
- Must share the same timestamp/slug as this plan.

## Tools / MCP

- `Read` / `Grep`：定位 mimic、billing、beta、Settings 默认值。
- `Shell`：跑 Go 单测；经 `ccmax-ssh` 离线验算抓包 `cch` / 指纹。
- `ccmax-ssh`：Phase 3 读取 / 部署 KIN persona（不重启 gateway）。
- 不使用浏览器自动化验证本协议改动。

## Acceptance Checklist

- [ ] mimic 出站身份句为 Agent SDK 句，不再是 `You are Claude Code, Anthropic's official CLI for Claude.`
- [ ] UA 为 `claude-cli/2.1.241 (external, sdk-cli)`，与 `cc_version=2.1.241.{fp}` 一致
- [ ] billing 使用 `cc_entrypoint=sdk-cli`；`cch` 仅在算法验算通过后出现
- [ ] 主对话含稳定 `cc_prompt_id`；后续轮含 `cc_prev_req`
- [ ] `cc_version` 指纹取样跳过 leading `<system-reminder>`，对抓包可见句得到 `134`
- [ ] mimic 默认 beta 含 3 个新 token，顺序与抓包一致，且不含 `redact-thinking`
- [ ] 扩写块前导 `\n`，`cache_control` 为 `ttl=1h` + `scope=global`
- [ ] 默认仍是 3 块 system，不注入官方工具表或 16k 会话块
- [ ] 真实 Claude Code 入站不被 rewrite
- [ ] KIN unofficial rewrite 与本仓 Phase 1 指纹一致（Phase 3）

## Risks / Blockers

- 身份句、ttl、`scope` 变化会使旧 prompt cache 失效，第一轮 `cache_creation` 上升。
- `cch` 算法若与官方不一致，假签名比不带更像伪造。验算失败则拆出单独跟进。
- 若误注入官方 2/3 块全文，可能造成行为漂移和 CRS `refusal`。
- KIN 与 sub2api 不同步会导致出站指纹分裂。Phase 1 合入后马上跟 Phase 3。

## Rollback / Recovery

- 身份句 / UA / billing / beta 都是常量与纯函数，回滚对应提交即可。
- KIN 用上一版 persona / sanitize 文件覆盖。
- 不改 DB schema，不迁账号。

## Checkpoints

- Commit after: 离线验完 `cch` 与指纹取样，且 Phase 1 单测通过。
- Commit after: KIN Phase 3 文件已 scp 且 `node --check` 通过（不主动 restart）。
- Phase 2 只有线上仍出现 429 extra-usage / 第三方判定时才开。

## References

- 抓包：`/opt/kin-gateway/data/captures/vm-29-official-20260823/`（`SUMMARY.md`、`bodies/01-05-post-v1-messages-req.json`）
- `backend/internal/pkg/claude/constants.go`：`CLICurrentVersion`、`FullClaudeCodeMimicryBetas`、`DefaultHeaders`
- `backend/internal/service/gateway_service.go`：`claudeCodeSystemPrompt` / Expansion
- `backend/internal/service/gateway_billing_block.go`：`computeClaudeCodeFingerprint`、`buildBillingAttributionText`
- `backend/internal/service/gateway_claude_oauth_body.go`：3 块 system 默认配置
- `backend/internal/service/metadata_userid.go`：`FormatMetadataUserID`（`>= 2.1.78` 走 JSON）
- 旧 `cch` 实现：commit `e51c9e50b`；删除：`6cfb7898d`
- KIN：`kin-gateway-ops/references/protocol.md`、`architecture.md`
