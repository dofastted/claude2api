# 子2 设计：build tag 精简版（剥离 payment + subscription）

> 配套 prd：`prd.md`。聚焦：分版机制、解耦边界、退化契约、wire 处理、权衡、回滚。
> 已整合 codex 交叉验证（gpt-5.5）must-fix，见末尾「修订记录」。

## build tag 选型

- tag 名：`slim`。默认构建（无 tag）= **完整版**零行为变化；`go build -tags slim` = 精简版。
- 按文件拆分（`xxx.go` `//go:build !slim` + `xxx_slim.go` `//go:build slim`），**只拆边界文件**（路由注册、wire provider set、payment/subscription service provider、billing/订阅 helper），不复制大 handler。
- full/slim 同名函数签名必须一致。CI 跑 `go test ./...` 与 `go test -tags slim ./...`。
- 首次为生产代码引入分版 → spec 固化约定。

## 「纯 API key」退化语义（用户已确认）

精简版网关鉴权**只做**：API key 真伪、用户状态、group 可用性、IP 限制。**不做**：余额 / 订阅 / 配额。这是用户拍板的"无套餐/配额门槛"。

## 两层目标

| 层级 | 目标 | 改动面 | 必做 |
|------|------|--------|------|
| **L1 功能剥离** | 精简版无支付/订阅路由与计费门槛；网关纯 API key | 中（路由 no-op + BillingGate + 中间件分版） | ✅ |
| **L2 依赖剥离** | payment package、provider SDK、service 内 payment 文件不进 slim 二进制 | 大（wire/wireinject/生成文件/service 文件分版） | ✅ |

策略：先 L1 跑通可用精简版，再 L2 抠依赖。

## 退化契约（codex-G 修正：核心是 BillingGate，不只是 SubscriptionService）

**关键发现**：真正挡请求的不是 `SubscriptionService`，而是 `BillingCacheService.CheckBillingEligibility`，被 handler 多处**直接调用且无 nil guard**：

- middleware 调 `SubscriptionService`：`api_key_auth.go:153`、`api_key_auth_google.go:81`；其中 `api_key_auth.go:214` 在 subscriptionService 缺失时**回退余额检查**（≠纯放行）。
- handler 从 context 取 `UserSubscription` 传给 billing：`gateway_handler.go:211`、`openai_gateway_handler.go:283/703/1311`、`gemini_v1beta_handler.go:203`。
- 实际拦截：`BillingCacheService.CheckBillingEligibility` @ `gateway_handler.go:227`、`openai_gateway_handler.go:716/1311`。

→ 单纯把 `subscriptionService`/`billingCacheService` 传 nil 会 **panic 或回退余额检查**，达不到"纯 API key"。

**方案（采纳 codex 建议）**：
1. 抽接口 `BillingGate { CheckEligibility(...) }`：
   - `billing_gate_full.go`(`!slim`)：调用现有 `BillingCacheService`。
   - `billing_gate_slim.go`(`slim`)：no-op，恒放行。
   - handler 各 `CheckBillingEligibility` 调用点改为走 `BillingGate`（保留接口，full 行为不变）。
2. 中间件 tag 分版：
   - `api_key_auth_full.go` / `api_key_auth_google_full.go`(`!slim`)：现有逻辑。
   - `*_slim.go`(`slim`)：只做 key 真伪/用户状态/group/IP，不碰订阅/余额；构造签名一致。

## 解耦边界

### payment（独立，易剥）
- package `internal/payment/*` + `provider/*`（含 `factory.go` 串联，`stripe.go:11`/`alipay.go:51` 直 import SDK）。
- handler：`payment_handler.go`、`payment_webhook_handler.go`、`admin/payment_handler.go`。
- 路由：`router.go:114 RegisterPaymentRoutes`；`auth.go` 微信支付 OAuth。
- **service 内 payment 文件（codex-H 关键）**：`PaymentConfigService`、`PaymentService`、`PaymentOrderExpiryService` 等——这些在 `service` 包内，**不打 tag 的话，导入 service 就会把 payment 代码带进 slim 二进制**，必须一并 tag 分版或拆子包。

### subscription（嵌入核心反代 → 退化）
- 中间件 + handler 调用点（见退化契约）。
- 路由 `admin.go:75 registerSubscriptionRoutes`。
- `SubscriptionService`、`SubscriptionExpiryService`。

## wire / 装配分版（codex-F 修正）

- `cmd/server/wire.go` 直接 import `internal/payment` 并注入 `payment.ProviderSet`（`wire.go:13`）；`handler.ProviderSet`（`handler/wire.go:139`）注册 payment/subscription handler。
- L1：保留注入，路由 no-op + BillingGate slim，先保证行为退化。
- L2 必须拆：
  - `wireinject` 文件带**组合 tag**：full `//go:build wireinject && !slim`，slim `//go:build wireinject && slim`。
  - 生成文件**互斥 tag**：`wire_gen.go` `//go:build !wireinject && !slim`，`wire_gen_slim.go` `//go:build !wireinject && slim`（否则重复 `initializeApplication` 或 slim 仍 import payment）。
  - `handler.ProviderSet` / `service.ProviderSet` / payment.ProviderSet / server 路由各拆 full/slim。
  - `provideCleanup`（`cmd/server/wire.go:72`，现接收 `SubscriptionExpiryService`/`SubscriptionService`/`PaymentOrderExpiryService`）分版。
- `handler.Handlers.Payment/Subscription` 字段 slim 下置 nil（结构体字段保留不分版，路由不注册即不解引用）。

## 兼容性 / 回滚
- 默认构建零变化（slim 代码全在 tag 后）。回滚 = 不加 `-tags slim` / 回退 commit。
- L2 失败可停在 L1（功能已满足，依赖未净）。

## 风险
- 两份 wire_gen 易漂移 → Make target 固化两版生成，CI 各跑。
- handler billing 调用点散落且无 nil guard → 必须全部走 BillingGate，逐点核对，否则 slim panic。
- `service` 包 payment 文件未 tag → slim 仍拉 SDK；验收以 `go list -deps` 为准。
- config 支付/订阅项 slim 保留但不生效（惰性），不删 config 结构。
- 前端精简非本任务硬验收项。

## 验收命令
```bash
cd backend
go test ./...                       # 完整版零回归
go build -tags slim ./... && go test -tags slim ./...
go list -deps -tags slim ./cmd/server | rg 'stripe|alipay|wechatpay|airwallex|internal/payment'   # 应为空
```

## 修订记录（codex 交叉验证采纳）
- G：核心拦截是 BillingCacheService 非 SubscriptionService；抽 BillingGate 接口 + 中间件 tag 分版；明确纯 API key 不含余额/配额。
- F：wireinject/生成文件组合 tag 互斥、provideCleanup 分版。
- H：service 包内 payment 文件须 tag；验收以 `go list -deps` 含 `internal/payment` 判定。
- E：只拆边界文件，full/slim 签名一致，CI 双跑。
