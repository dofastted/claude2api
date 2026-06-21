# 精简版：build tag 剥离支付 + 订阅

## Goal

用 Go build tag 让**一份代码**编出两种产物：

- **完整版（默认）**：行为与今日完全一致，含支付 + 订阅。
- **精简版（带 build tag）**：只保留核心反代，剥离全部 **payment + subscription** 相关代码、路由与依赖；网关鉴权退化为**纯 API key 校验**（无套餐/配额门槛）。

## 背景与现状（已调研）

- **payment 模块边界清晰**：独立 package `internal/payment/*`（amount/crypto/currency/fee/load_balancer/registry/types/wire）+ `internal/payment/provider/*`（airwallex/alipay/easypay/stripe/wxpay/factory）；handler 侧 `payment_handler.go`、`payment_webhook_handler.go`、`admin/payment_handler.go`；路由 `routes/payment.go` + `auth.go` 的微信支付 OAuth。→ 相对容易整块剥离。
- **subscription 深度耦合核心反代**（关键风险点）：
  - 路由 `routes/gateway.go` 用 `middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg)` —— **订阅服务嵌在网关鉴权中间件**。
  - 核心网关 handler（`gateway_handler.go`、`openai_gateway_handler.go`、`gateway_handler_responses.go`、`openai_chat_completions.go`、`gemini_v1beta_handler.go` 等）均引用 subscription。
  - `admin/subscription_handler.go` + `routes/admin.go` 的整组 `/subscriptions` 路由。
  - wire / handler.go 依赖注入对 Subscription / Payment handler 深度绑定。
- **build tag 现状**：仓库内 `//go:build` **仅用于测试文件**，生产代码无分版先例 → 需要新建一套生产代码分版约定。

## Requirements

### R1 — 分版机制
- 选定一个 build tag（如 `slim`，design 定名）。默认构建 = 完整版；`-tags slim` = 精简版。
- 分版边界清晰、可维护：优先用「按 tag 分文件」（`xxx.go` / `xxx_slim.go` + build tag）而非满文件散落 `if`，降低长期维护成本。
- 完整版默认构建零行为变化，无需任何额外 flag。

### R2 — 剥离 payment
- 精简版不编译/不注册：payment package、payment provider、payment handler（含 admin）、payment 路由、微信支付 OAuth、payment 相关 config/wire 注入。
- 精简版二进制中不含支付 provider 的第三方依赖（能从依赖图移除的尽量移除；至少不在精简版代码路径引用）。

### R3 — 剥离 subscription 并退化鉴权
- 精简版不编译/不注册 subscription handler、admin 订阅路由、订阅业务逻辑。
- 网关鉴权中间件提供**无订阅退化实现**：精简版下 `APIKeyAuthWithSubscription*` 等价行为退化为纯 API key 校验（校验 key 有效性即放行，不做套餐/配额判定）。
- 核心反代各 handler 中对 subscription 的引用，在精简版下走退化分支或被 tag 隔离，保证核心反代请求链路完整可用。

### R4 — 一致性与可构建性
- 两个版本都能 `go build` / `go vet` 通过；完整版 `go test ./...` 通过。
- 提供两版构建命令/Make target，文档说明如何编出精简版。

## 约束

- **不改变完整版任何行为**：所有剥离都在 build tag 之后；默认路径不受影响。
- 外科手术式改动：只隔离支付/订阅，不顺手重构邻近核心反代逻辑。
- 退化鉴权要安全：精简版纯 API key 校验仍须校验 key 真实有效，不能变成「无鉴权放行」。
- 子1 的反代真实性增强**不属于本任务剥离范围**，且必须在精简版下仍可用。

## Acceptance Criteria

- [ ] `go build`（默认/完整版）产物行为与改动前一致，`go test ./...` 全通过。
- [ ] `go build -tags slim`（精简版）成功，启动后核心反代请求（至少一条 OpenAI/Anthropic/Gemini 链路）可正常完成。
- [ ] 精简版无任何 payment / subscription 路由（含 admin 与 webhook），相关 handler 未注册。
- [ ] 精简版网关鉴权对有效 API key 放行、对无效 key 拒绝，且不做套餐/配额判定。
- [ ] 精简版代码路径不引用 payment provider；依赖图中支付相关第三方库不被精简版主链路拉入。
- [ ] 有文档/Make target 说明两版构建方式。

## 开放问题（design 阶段解决）

- build tag 命名与分版粒度（按文件拆分的最小集合：哪些文件需要 `_full.go` / `_slim.go` 配对）。
- 退化鉴权中间件的落点：在 `middleware` 层提供 tag 分版实现，还是注入一个 no-op SubscriptionService。
- wire 注入如何分版（providerset 是否需要 full/slim 两套），避免 nil 指针。
- config 中支付/订阅相关项在精简版下的处理（保留为惰性 / tag 隐藏）。
- 前端是否需要对应精简（本任务先聚焦后端，前端按需在 design 评估）。

## Notes

- 复杂任务：本 prd 之后需补 `design.md`（技术设计）与 `implement.md`（执行清单）方可 `task.py start`。
- 关键风险集中在 subscription 与核心反代鉴权的解耦，design 须优先攻克退化中间件方案。
