# Phase 1: CC 请求识别与 429 细分类 - 技术设计

## 1. 架构概览

本设计在现有 sub2api 架构基础上增强两个核心能力：

1. **Claude Code 请求识别收紧**：在 `ClaudeCodeValidator` 基础上，明确「真实 Claude Code 请求」与「需要 mimicry 的第三方客户端」的边界。
2. **Claude 429 状态细分类**：在 `RateLimitService` 中增加 Claude 专用的 429 分类器，区分四类限流状态。

### 设计原则

- **最小侵入**：不改变现有 SSE event block 状态机、账号调度流程。
- **向后兼容**：R1/R2/R3 能力保持正常工作，不影响 OpenAI/Gemini 等非 Claude 平台。
- **硬编码优先**：关键字段（UA pattern、429 关键字）硬编码在代码中，不依赖外部配置。
- **可观测性**：判定结果可通过日志/调试模式查询，便于运营诊断。

---

## 2. R4.1 Claude Code 请求识别与改写边界收紧

### 2.1 当前实现分析

**已有能力**（`claude_code_validator.go`）：
- `ClaudeCodeValidator.Validate()` 验证请求是否来自 Claude Code CLI。
- 验证策略包括：UA 匹配、system prompt 相似度、headers 检查、metadata.user_id 格式验证。
- 验证结果通过 `IsClaudeCodeClient(ctx)` 存储在请求上下文中。

**当前问题**：
- `gateway_handler.go` 和 `gateway_service.go` 中的 mimicry 逻辑（system prompt 替换、tool name 改写、metadata 修改）分散在多个位置。
- 对「真实 Claude Code 请求」的识别边界不够清晰，部分场景下仍会触发不必要的改写。

### 2.2 设计方案

#### 2.2.1 识别函数强化

在 `claude_code_validator.go` 中增强现有 `ClaudeCodeValidator.Validate()` 函数：

```go
// IsRealClaudeCodeRequest 判断是否为真实 Claude Code 请求（不需要 mimicry）
// 条件：
// 1. UA 匹配 claude-cli/x.x.x
// 2. 对于 /messages 请求：
//    - system prompt 包含 Claude Code 标识（计费归因块或相似度检查）
//    - X-App header 存在
//    - anthropic-beta header 存在
//    - anthropic-version header 存在
//    - metadata.user_id 格式合法
// 3. 对于非 /messages 请求：UA 匹配即可
func (v *ClaudeCodeValidator) IsRealClaudeCodeRequest(r *http.Request, body map[string]any) bool {
	// 复用现有 Validate() 逻辑
	return v.Validate(r, body)
}
```

**关键点**：
- 现有 `Validate()` 逻辑已经足够严格，可以直接复用。
- 为清晰语义，新增 `IsRealClaudeCodeRequest()` 作为别名函数，明确「真实请求」的概念。

#### 2.2.2 Mimicry 边界清晰化

**改写位置**：
1. **System prompt 替换**：`gateway_service.go:4860` 附近的 `isClaudeCode` 判断。
2. **Tool name 改写**：`gateway_service.go` 中的工具名随机化逻辑。
3. **Metadata user_id 修改**：`gateway_service.go:9814` 附近的 metadata 改写。
4. **Cache control 处理**：Claude mimicry 相关的 cache_control 改写。

**改进策略**：
```go
// 在 GatewayService.Forward() 或相关函数中
isRealClaudeCode := IsClaudeCodeClient(ctx)

// System prompt 改写
if !isRealClaudeCode {
	// 执行 mimicry：替换 system prompt
}

// Tool name 改写
if !isRealClaudeCode {
	// 执行 mimicry：随机化 tool name 或 PascalCase 转换
}

// Metadata user_id 改写
if !isRealClaudeCode {
	// 执行 mimicry：修改 metadata.user_id
}
```

**关键点**：
- 真实 Claude Code 请求**跳过** mimicry 流程，保持客户端原始语义。
- 第三方客户端（`isRealClaudeCode == false`）**进入** mimicry 流程。
- R1（UA 自动拉取）、R3（TLS 家族化）策略与 mimicry 正交，继续生效。

#### 2.2.3 上下文状态增强

在 `ctxkey` 包中新增上下文键（如果不存在）：

```go
// ctxkey.IsClaudeCodeClient 已存在
// 无需新增，复用现有 IsClaudeCodeClient 即可
```

**关键点**：
- 复用现有 `IsClaudeCodeClient` 上下文状态。
- 确保 `SetClaudeCodeClientContext()` 在 mimicry 逻辑**之前**调用（当前已满足，见 `gateway_handler.go:181`）。

#### 2.2.4 日志与可观测性

在 mimicry 相关逻辑中增加调试日志：

```go
if !isRealClaudeCode {
	logger.Debug("claude_mimicry_triggered",
		"account_id", account.ID,
		"action", "system_prompt_replacement",
		"reason", "not_real_claude_code_client")
}
```

**关键点**：
- 日志级别为 Debug，避免生产环境噪音。
- 记录触发 mimicry 的原因和具体动作。

---

## 3. R4.3 Claude 上游 429 与限流状态细分类

### 3.1 当前实现分析

**已有能力**（`ratelimit_service.go`）：
- `RateLimitService.HandleUpstreamError()` 处理上游错误，包括 429。
- 对 429 响应的处理较粗粒度：解析 `retry-after` header，设置账号冷却时间。
- 支持模型级限流（`model_rate_limits`）存储在 `Account.Extra` 中。

**当前问题**：
- 未区分 Claude 不同类型的 429/限流状态（普通 429、Extra Usage、Opus 周限、5h 窗口）。
- 冷却时间策略无法根据限流类型调整。

### 3.2 设计方案

#### 3.2.1 429 分类器

在 `ratelimit_service.go` 中新增 Claude 专用分类函数：

```go
// ClaudeRateLimitType 表示 Claude 429 限流类型
type ClaudeRateLimitType string

const (
	ClaudeRateLimitTypeUnknown       ClaudeRateLimitType = "unknown"         // 未识别（默认 1h）
	ClaudeRateLimitTypeExtraUsage    ClaudeRateLimitType = "extra_usage"     // Extra Usage required（24h）
	ClaudeRateLimitTypeOpusWeekly    ClaudeRateLimitType = "opus_weekly"     // Opus 周限（168h）
	ClaudeRateLimitType5HourWindow   ClaudeRateLimitType = "5h_window"       // 5h 窗口限流（5h）
	ClaudeRateLimitTypeGeneric       ClaudeRateLimitType = "generic"         // 普通 429（1h）
)

// classifyClaudeRateLimit 分类 Claude 429 响应
// 参考 CRS 的 _classifyClaudeRateLimit 实现
func (s *RateLimitService) classifyClaudeRateLimit(statusCode int, headers http.Header, body []byte) (ClaudeRateLimitType, time.Duration) {
	if statusCode != 429 {
		return ClaudeRateLimitTypeUnknown, 0
	}

	bodyStr := string(body)

	// 类型 B：Extra Usage required（优先级最高）
	if strings.Contains(bodyStr, "extra usage required") {
		return ClaudeRateLimitTypeExtraUsage, 24 * time.Hour
	}

	// 类型 C：Opus 周限
	// 关键字：claude-opus.*models per week
	if regexp.MustCompile(`(?i)claude-opus.*models per week`).MatchString(bodyStr) {
		return ClaudeRateLimitTypeOpusWeekly, 168 * time.Hour
	}

	// 类型 D：5h 窗口限流
	// 关键字：5 hour window
	if strings.Contains(bodyStr, "5 hour window") {
		return ClaudeRateLimitType5HourWindow, 5 * time.Hour
	}

	// 类型 A：无 retry-after / x-ratelimit-reset header 的普通 429
	retryAfter := headers.Get("retry-after")
	rateLimitReset := headers.Get("x-ratelimit-reset")
	if retryAfter == "" && rateLimitReset == "" {
		return ClaudeRateLimitTypeGeneric, 1 * time.Hour
	}

	// 其他：未识别（回退到解析 retry-after）
	return ClaudeRateLimitTypeUnknown, 0
}
```

**关键点**：
- 关键字段硬编码：`"extra usage required"`、`"claude-opus.*models per week"`、`"5 hour window"`。
- 分类优先级：Extra Usage > Opus 周限 > 5h 窗口 > 普通 429 > 未识别。
- 返回值包含限流类型和建议冷却时长。

#### 3.2.2 集成到 HandleUpstreamError

在 `RateLimitService.HandleUpstreamError()` 中集成分类器：

```go
func (s *RateLimitService) HandleUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte, models ...string) bool {
	// ... 现有逻辑 ...

	// Claude 平台专用：429 细分类
	if account.Platform == PlatformClaude && statusCode == 429 {
		rateLimitType, cooldownDuration := s.classifyClaudeRateLimit(statusCode, headers, body)
		
		// 日志记录
		logger.Info("claude_429_classified",
			"account_id", account.ID,
			"type", rateLimitType,
			"cooldown_duration", cooldownDuration,
			"body_snippet", truncateString(string(body), 256))
		
		// 根据分类结果设置冷却时间
		if cooldownDuration > 0 {
			until := time.Now().Add(cooldownDuration)
			reason := fmt.Sprintf("Claude rate limit: %s", rateLimitType)
			
			// 设置账号临时不可调度
			if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
				logger.Error("failed_to_set_temp_unschedulable", "error", err)
			}
			
			// 缓存更新（如果存在）
			if s.tempUnschedCache != nil {
				state := &TempUnschedState{
					TempUnschedUntil: until,
					ErrorMessage:     reason,
				}
				s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state)
			}
			
			return true // shouldDisable = true
		}
	}

	// ... 现有逻辑（回退到通用 429 处理）...
}
```

**关键点**：
- 仅对 `PlatformClaude` 执行分类逻辑，不影响其他平台。
- 分类结果记录到日志，便于运营诊断。
- 冷却时长优先使用分类器建议值，回退到现有 `retry-after` 解析逻辑。

#### 3.2.3 日志字段标准化

在 usage log 或 ops 上下文中记录 429 分类结果：

```go
// 在 setOpsUpstreamError() 或相关函数中
type OpsUpstreamErrorEvent struct {
	// ... 现有字段 ...
	ClaudeRateLimitType string `json:"claude_rate_limit_type,omitempty"` // 新增
}

// 设置上下文
event := OpsUpstreamErrorEvent{
	// ... 现有字段 ...
	ClaudeRateLimitType: string(rateLimitType),
}
```

**关键点**：
- 新增字段向后兼容（使用 `omitempty`）。
- 便于后续 Phase 3 的管理员诊断视图查询。

---

## 4. 数据模型

### 4.1 Account 模型（无变更）

复用现有 `Account.Extra` 字段存储模型级限流状态：

```json
{
  "model_rate_limits": {
    "claude-opus-4": {
      "rate_limit_reset_at": "2026-06-22T12:00:00Z"
    }
  }
}
```

### 4.2 TempUnschedState 模型（扩展）

在 `ErrorMessage` 字段中包含 429 分类信息：

```json
{
  "temp_unsched_until": "2026-06-22T12:00:00Z",
  "error_message": "Claude rate limit: extra_usage"
}
```

**关键点**：
- 无需新增字段，复用现有 `ErrorMessage`。
- 便于后续解析（如 `strings.Contains(msg, "extra_usage")`）。

### 4.3 OpsUpstreamErrorEvent 模型（扩展）

新增 `ClaudeRateLimitType` 字段：

```go
type OpsUpstreamErrorEvent struct {
	Platform            string    `json:"platform"`
	AccountID           int64     `json:"account_id"`
	AccountName         string    `json:"account_name"`
	UpstreamStatusCode  int       `json:"upstream_status_code"`
	UpstreamURL         string    `json:"upstream_url"`
	Passthrough         bool      `json:"passthrough"`
	Kind                string    `json:"kind"`
	Message             string    `json:"message"`
	ClaudeRateLimitType string    `json:"claude_rate_limit_type,omitempty"` // 新增
	Timestamp           time.Time `json:"timestamp"`
}
```

---

## 5. 关键边界与约束

### 5.1 平台隔离

- **R4.1**：仅影响 `PlatformClaude` 且账号类型为 OAuth/SetupToken 的请求。
- **R4.3**：仅对 `PlatformClaude` 的 429 响应执行分类逻辑。
- **其他平台**：OpenAI、Gemini、Antigravity 等平台的现有逻辑保持不变。

### 5.2 SSE 状态机兼容性

- 不引入新的旁路代理或流式响应处理机制。
- 复用现有 `HandleUpstreamError()` 和 `SetTempUnschedulable()` 流程。
- 不改变 SSE event block 的解析和重试逻辑。

### 5.3 R1/R2/R3 兼容性

- **R1（UA 自动拉取）**：与 mimicry 边界正交，继续生效。
- **R2（WebSocket 一键全开）**：不涉及请求识别和 429 分类，继续生效。
- **R3（TLS 家族化）**：与 mimicry 边界正交，继续生效。

### 5.4 硬编码字段

**Claude Code 请求识别**：
- UA pattern：`^claude-cli/\d+\.\d+\.\d+`（已硬编码）
- 计费归因块前缀：`x-anthropic-billing-header`（已硬编码）
- 必需 headers：`X-App`、`anthropic-beta`、`anthropic-version`（已硬编码）

**429 分类关键字**：
- Extra Usage：`"extra usage required"`
- Opus 周限：`"claude-opus.*models per week"`（正则）
- 5h 窗口：`"5 hour window"`
- 来源：CRS `_classifyClaudeRateLimit` 函数

---

## 6. 测试策略

### 6.1 单元测试

**R4.1 Claude Code 请求识别**：
- 测试真实 Claude Code 请求不触发 mimicry。
- 测试第三方客户端触发 mimicry。
- 测试边界条件（UA 匹配但 system prompt 不匹配）。

**R4.3 429 分类器**：
- 测试四类 429 样本的分类准确性。
- 测试关键字大小写不敏感（`(?i)` 正则）。
- 测试优先级（Extra Usage > Opus 周限 > 5h 窗口 > 普通 429）。

### 6.2 集成测试

**R4.1**：
- 使用真实 Claude Code UA 和 system prompt，验证请求不被改写。
- 使用模拟第三方客户端 UA，验证 mimicry 生效。

**R4.3**：
- 使用四类 429 响应样本，验证账号冷却时长正确。
- 验证日志中记录了 `rate_limit_type` 字段。

### 6.3 回归测试

- 验证 OpenAI/Gemini 平台的现有 429 处理逻辑不受影响。
- 验证 R1/R2/R3 能力继续正常工作。

---

## 7. 风险与缓解

### 7.1 Claude Code 请求识别误判

**风险**：
- 误判为真实 Claude Code → 请求形态不足，可能被上游识别。
- 误判为第三方客户端 → 过度改写，协议差异。

**缓解**：
- 先保守识别（复用现有 `Validate()` 逻辑，条件已经足够严格）。
- 增加调试日志，便于根据反馈迭代。

### 7.2 429 分类关键字变化

**风险**：
- Claude 官方调整 429 响应格式，导致分类失效。

**缓解**：
- 硬编码关键字并在代码注释中标注来源（CRS）。
- 定期回归测试（从 usage log 中抽样真实 429 响应）。
- 回退机制：分类失败时降级到现有 `retry-after` 解析逻辑。

### 7.3 性能影响

**风险**：
- 429 分类器增加字符串匹配和正则运算，可能影响性能。

**缓解**：
- 分类器仅在 `statusCode == 429 && Platform == Claude` 时触发，命中率低。
- 关键字匹配使用 `strings.Contains()`（O(n)），正则编译为全局变量（避免重复编译）。

---

## 8. 部署与回滚

### 8.1 部署策略

- **灰度**：先在测试账号上验证，再全量发布。
- **监控指标**：
  - `claude_429_classified` 日志的分类结果分布。
  - 账号临时不可调度的原因分布（`error_message` 字段）。
  - 真实 Claude Code 请求的 mimicry 触发率（应为 0）。

### 8.2 回滚策略

- **R4.1**：如果发现误判，可通过配置项（如 `disable_claude_code_detection`）临时禁用识别逻辑，回退到全部执行 mimicry。
- **R4.3**：如果分类逻辑异常，回退到现有 `retry-after` 解析逻辑（分类器返回 `ClaudeRateLimitTypeUnknown` 时已自动回退）。

---

## 9. 后续扩展（Phase 2/3）

本设计为后续 Phase 预留接口：

- **Phase 2（R4.2）**：账号级 header profile 学习 → 可复用 `IsClaudeCodeClient` 判定结果，决定是否学习 headers。
- **Phase 2（R4.4）**：Warmup 类请求本地拦截 → 可复用 `IsClaudeCodeClient` 判定结果，决定是否拦截。
- **Phase 3（R4.5）**：管理员诊断视图 → 查询 `OpsUpstreamErrorEvent.ClaudeRateLimitType` 字段。
- **Phase 3（R4.6）**：状态文案增强 → 根据 `ClaudeRateLimitType` 生成用户友好的错误提示。

---

## 10. 参考资料

- CRS 源码：`claude-relay-service/src/services/` 目录
- sub2api 现有实现：
  - `backend/internal/service/claude_code_validator.go`
  - `backend/internal/service/ratelimit_service.go`
  - `backend/internal/handler/gateway_handler.go`
  - `backend/internal/service/gateway_service.go`
