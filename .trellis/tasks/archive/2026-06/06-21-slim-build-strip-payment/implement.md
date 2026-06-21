# 子2 执行计划：build tag 精简版

> 配套 `prd.md` + `design.md`。分两层：L1 功能剥离（先跑通可用精简版）→ L2 依赖剥离（payment 库不进二进制）。每步带验证与回滚点。

## 前置
- [ ] 分支：`feat/slim-build`（从 main）。
- [ ] 基线：`cd backend && go build ./... && go test ./...` 全绿。
- [ ] 确认 build tag 命名 `slim`，并在 spec 记一条「生产代码分版约定」。

---

## 阶段 L1 — 功能剥离（可用精简版）

### L1.1 路由注册分版
- [ ] 把 payment 路由注册抽成 tag 分版：`register_payment_full.go`(`!slim`, 现有 `RegisterPaymentRoutes` 内容) / `register_payment_slim.go`(`slim`, no-op)。
- [ ] 同法处理 `registerSubscriptionRoutes`（admin 订阅路由）与 `auth.go` 微信支付 OAuth 两条路由。
- [ ] `router.go` / `admin.go` 调用点保持不变，由 tag 决定空实现。
- **验证**：`go build -tags slim ./... && go build ./...` 均通过。

### L1.2 网关鉴权退化（codex-G：核心是 BillingGate，不只 SubscriptionService）
- [ ] L1.2a 抽接口 `BillingGate{ CheckEligibility(...) }`：`billing_gate_full.go`(`!slim`) 调现有 `BillingCacheService`；`billing_gate_slim.go`(`slim`) no-op 恒放行。
- [ ] L1.2b 把 handler 各 `BillingCacheService.CheckBillingEligibility` 调用点（`gateway_handler.go:227`、`openai_gateway_handler.go:716/1311` 等）改为走 `BillingGate`（full 行为不变，**逐点核对无遗漏 → 否则 slim panic**）。
- [ ] L1.2c 中间件 tag 分版：`api_key_auth_full.go`/`api_key_auth_google_full.go`(`!slim`) 保留现有；`*_slim.go`(`slim`) 仅做 key 真伪/用户状态/group/IP，**不碰订阅/余额**（构造签名一致）。注意 `api_key_auth.go:214` 现有 nil-subscription 回退余额逻辑不能被 slim 继承。
- **验证**：`go build -tags slim ./...`；slim 启动后有效/无效 key 各打一条核心反代请求，验证放行/拒绝且无 panic。
- **review gate**：退化安全红线——slim 仍校验 key 真伪，非无鉴权放行。

### L1.3 冒烟
- [ ] `go build -tags slim -o /tmp/sub2api-slim ./cmd/server` 启动成功。
- [ ] OpenAI / Anthropic / Gemini 至少一条核心反代链路请求成功。
- [ ] 确认无 `/payment`、`/admin/subscriptions`、支付 webhook 路由。
- **回滚点**：到此精简版功能可用；L2 失败可停在 L1（功能已满足，仅依赖未抠净）。

---

## 阶段 L2 — 依赖剥离（payment 库不进精简版二进制）

### L2.1 wire 分版（codex-F：组合 tag + 互斥生成文件）
- [ ] `wireinject` 文件带组合 tag：full `//go:build wireinject && !slim`，slim `//go:build wireinject && slim`。
- [ ] 生成文件互斥：`wire_gen.go` `//go:build !wireinject && !slim`，`wire_gen_slim.go` `//go:build !wireinject && slim`（防重复 `initializeApplication` / slim 仍 import payment）。
- [ ] `cmd/server/wire.go` 分 full(注入 `payment.ProviderSet` + payment/subscription provider) / slim(不注入)。
- [ ] `handler.ProviderSet` / `service.ProviderSet` / 路由各拆 full/slim。
- [ ] `provideCleanup`（`cmd/server/wire.go:72`，现接收 `SubscriptionExpiryService`/`SubscriptionService`/`PaymentOrderExpiryService`）分版。
- [ ] 用 Make target 固化两版 wire 生成。
- [ ] `handler.Handlers.Payment/Subscription` 字段 slim 下置 nil（不分结构体）。

### L2.1b service 包内 payment 文件分版（codex-H：易漏点）
- [ ] 给 `service` 包内 `PaymentConfigService`/`PaymentService`/`PaymentOrderExpiryService` 等 payment 相关文件打 tag（或拆子包），否则**导入 service 即把 payment 代码带进 slim 二进制**。
- **验证**：`go build -tags slim ./cmd/server`；`go list -deps -tags slim ./cmd/server | rg 'internal/payment|stripe|alipay|wechatpay|airwallex'` **应为空**。

### L2.2 装配签名（按需，彻底去依赖时）
- [ ] 若 L2.1 后 `SetupRouter`/`http.go`/`RegisterGatewayRoutes` 仍强拉 subscription/payment 依赖，则对这些签名做 tag 分版去参；否则保留签名传退化实现。
- **验证**：`go list -deps -tags slim` 不含 payment 链路。

### L2.3 依赖确认
- [ ] `go mod why` / `go list -deps -tags slim` 确认支付 provider 第三方库未被 slim 主链路拉入。
- **回滚点**：L2 任一步出问题，可回退到 L1 的可用精简版。

---

## 阶段 M — 构建集成与文档
- [ ] Makefile 加两版 target：`build`（完整版）、`build-slim`（`-tags slim`），及对应 wire 生成 target。
- [ ] 文档（DEV_GUIDE / deploy 文档）说明两版构建方式与精简版能力差异。
- [ ] CI 增加 slim 构建检查（至少 `go build -tags slim`）。

## 收尾
- [ ] 完整版：`go build ./... && go vet ./... && go test ./...` 全绿（零回归）。
- [ ] 精简版：`go build -tags slim ./...` 通过 + 冒烟通过 + 依赖检查通过。
- [ ] 3.3 spec：固化「slim 生产代码分版约定」「退化鉴权契约」「wire 双版本维护」。
- [ ] 3.4 提交。

## 验证命令汇总
```bash
cd backend
# 完整版零回归
go build ./... && go vet ./... && go test ./...
# 精简版
go build -tags slim ./...
go build -tags slim -o /tmp/sub2api-slim ./cmd/server
# 依赖剥离确认（应无输出）
go list -deps -tags slim ./cmd/server | grep -E 'internal/payment|stripe|airwallex|wxpay|alipay'
```
