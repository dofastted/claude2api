# 子1 执行计划：反代真实性增强

> 配套 `prd.md` + `design.md`。三块（R1/R2/R3）可独立交付；建议按 R2 → R3 → R1 顺序（由易到难、由低风险到高风险）。每步带验证命令与回滚点。

## 前置
- [ ] 工作分支：`feat/proxy-realism-enhancement`（从 main）。
- [ ] 基线：`cd backend && go build ./... && go test ./... ` 全绿，记录基线。

---

## 阶段 A — R2：codex websocket 一键全开（最低风险，先做）

- [ ] A1. service 层加 `EnableAllOpenAIWS(ctx, accountID)` / `ResetOpenAIWS(ctx, accountID)`：按 design「全开目标态」**8 字段表**写/删 `accounts.extra`（**含两个分类型 `*_enabled=true`**，否则已有 false 覆盖通用 true）；非 OpenAI 账号返回错误。
- [ ] A1b.（codex-C）enable-all 前校验全局 `gateway.openai_ws.responses_websockets_v2` 未禁用；禁用时返回明确提示（账号 extra 无法覆盖全局禁用）。OAuth/API Key 类型分别处理。
- [ ] A2. admin handler 加动作 + 路由（`routes/admin.go`），仅 admin 鉴权可调用。
- [ ] A3. 单测：enable-all 后 `account.go` 各 ws 解析方法返回期望值（**覆盖已有分类型 false 被纠正**）；全局禁用时拒绝；reset 回默认；重复执行幂等。
- [ ] A4.（可选）前端账号管理加「一键全开 WS / 重置」按钮。
- **验证**：`go test ./internal/service/... ./internal/handler/admin/...`
- **回滚点**：纯新增动作，删除新增文件/路由即可。

---

## 阶段 B — R3：家族化 UA↔TLS 配对（中风险）

- [ ] B1. 新建 `internal/pkg/clientidentity`：定义 `ClientFamily` + 上层 `Resolve(account) -> snapshot{headers, version fields, tls profile name}`（codex-D：family 解析在上层，**不进 dialer**）；默认 family 行为等价今日。
- [ ] B2. 采集真实指纹/UA（数据依赖）：抓包或查已知库得到 codex-cli/desktop、claude-cli/desktop 的 TLS 指纹与 UA（codex 注意真实族是 `codex_cli_rs`，UA 含 OS/arch 后缀）；落为 `tlsfingerprint.Profile` 预设 + UA 模板，**注明来源与采集日期**。codex family 必须有自己的 profile，不可复用 Node/Claude 默认指纹。
- [ ] B3. TLS profile 选择路径对接上层 resolver（复用/对接现有 `ResolveTLSProfile(account)`，`account_usage_service.go:1165`）；`tlsfingerprint.NewDialer` 保持低层只收 profile，无 family 时用今日默认。
- [ ] B4. header / billing / identity 注入点统一从 resolver 取 snapshot，保证与 TLS 同族（杜绝错配）。
- [ ] B5. 测试：family→snapshot 配对断言（header 与 tls profile 同族）；**无 family = 现有 profile 回归**；可选 dialer 抓包集成测试。
- **验证**：`go test ./internal/pkg/clientidentity/... ./internal/pkg/tlsfingerprint/...`
- **review gate**：B2 指纹数据需人工核对真实性后再继续（错误指纹危害大于不改）。
- **回滚点**：清空账号 `client_family` 即回今日行为。

---

## 阶段 C — R1：UA 自动拉取（最高风险，最后做）

- [ ] C1. `IdentityRegistry`（pkg/clientidentity）：`atomic.Pointer` 持有 **claude + codex 两族完整 snapshot**，只读 getter；初始化为内置兜底（现有常量）。
- [ ] C2. 注入点改造（codex-A：**全落点**，非仅 DefaultHeaders）：`pkg/claude.DefaultHeaders`、`service/gateway_billing_block.go:73`(cc_version/fp/cch)、`service/gateway_service.go:4322`、`service/identity_service.go:28`、codex `openai_gateway_service.go:45/1268/71`(UA/originator/x-codex) 全部改为读 snapshot，缺省回退常量。**先在 snapshot 恒等于常量前提下跑通（零行为变化）**，再接 fetcher。
- [ ] C3. `VersionFetcher` service：claude-cli(npm @anthropic-ai/claude-code)、claude-sdk(**优先解析 claude-code 包内 sdk 依赖**，latest 兜底)、codex(github/npm，真实族 codex_cli_rs)；组装**完整一致快照**；失败丢弃保留旧值。处理解析陷阱（v 前缀/预发布/latest 不同步/UA 后缀/originator 同族）。
- [ ] C4. 周期调度（后台 goroutine，不阻塞启动/请求），超时+重试+频率限制。
- [ ] C5. config 开关 `gateway.ua_auto_fetch.{enabled,interval,sources}`（默认 enabled=false）；admin setting 暴露生效版本/来源/最后状态。
- [ ] C6. 测试：版本解析（mock 响应）、联动字段一致性、失败回退、关闭开关=常量。
- **验证**：`go test ./internal/service/... ./internal/pkg/claude/... ./internal/pkg/clientidentity/...`
- **回滚点**：`enabled=false` 即回硬编码；或回退 commit。

---

## 收尾（全部完成后）
- [ ] 全量 `go build ./... && go vet ./... && go test ./...`（完整版）通过。
- [ ] 确认本能力不引用 payment/subscription（`go list` 依赖检查），为子2 精简版可用性背书。
- [ ] 3.3 spec 更新：把「UA 联动字段一致性」「ws 全开字段清单」「family 指纹采集来源」沉淀到 `.trellis/spec`。
- [ ] 3.4 提交。

## 验证命令汇总
```bash
cd backend
go build ./... && go vet ./...
go test ./internal/service/... ./internal/handler/admin/... \
        ./internal/pkg/clientidentity/... ./internal/pkg/tlsfingerprint/... ./internal/pkg/claude/...
```
