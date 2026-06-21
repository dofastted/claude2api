# Phase 1: CC 请求识别与 429 细分类 - 执行计划

## 1. 执行概览

本计划将分两个并行批次执行：

- **批次 1（R4.1）**：Claude Code 请求识别与改写边界收紧
- **批次 2（R4.3）**：Claude 上游 429 与限流状态细分类

每个批次包含：代码实现 → 单元测试 → 集成测试 → 验证。

---

## 2. 批次 1：Claude Code 请求识别与改写边界收紧（R4.1）

### 2.1 代码实现

#### 步骤 1.1：增强 ClaudeCodeValidator（可选）

**文件**：`backend/internal/service/claude_code_validator.go`

**任务**：
- 新增 `IsRealClaudeCodeRequest()` 函数作为 `Validate()` 的语义别名（可选，提升代码可读性）。

**实现**：
```go
// IsRealClaudeCodeRequest 判断是否为真实 Claude Code 请求（不需要 mimicry）
// 这是 Validate() 的语义别名，明确「真实请求」的概念
func (v *ClaudeCodeValidator) IsRealClaudeCodeRequest(r *http.Request, body map[string]any) bool {
	return v.Validate(r, body)
}
```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 1.2：收紧 System Prompt 改写边界

**文件**：`backend/internal/service/gateway_service.go`

**任务**：
- 找到 system prompt 改写逻辑（约 `gateway_service.go:4860` 附近）。
- 增加 `isRealClaudeCode` 判断，跳过真实 Claude Code 请求。

**当前代码分析**：
```bash
grep -n "isClaudeCode.*isClaudeCodeClient" backend/internal/service/gateway_service.go
```

**实现要点**：
- 在 system prompt 替换前增加判断：
  ```go
  isRealClaudeCode := IsClaudeCodeClient(ctx)
  if !isRealClaudeCode {
      // 执行 mimicry：替换 system prompt
  }
  ```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 1.3：收紧 Tool Name 改写边界

**文件**：`backend/internal/service/gateway_service.go`

**任务**：
- 找到 tool name 随机化或 PascalCase 转换逻辑。
- 增加 `isRealClaudeCode` 判断，跳过真实 Claude Code 请求。

**搜索关键字**：
```bash
grep -n "tool.*name\|PascalCase\|randomize" backend/internal/service/gateway_service.go | head -20
```

**实现要点**：
- 在 tool name 改写前增加判断：
  ```go
  isRealClaudeCode := IsClaudeCodeClient(ctx)
  if !isRealClaudeCode {
      // 执行 mimicry：改写 tool name
  }
  ```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 1.4：收紧 Metadata user_id 改写边界

**文件**：`backend/internal/service/gateway_service.go`

**任务**：
- 找到 metadata.user_id 改写逻辑（约 `gateway_service.go:9814` 附近）。
- 增加 `isRealClaudeCode` 判断，跳过真实 Claude Code 请求。

**实现要点**：
- 在 metadata 改写前增加判断：
  ```go
  isRealClaudeCode := IsClaudeCodeClient(ctx)
  if !isRealClaudeCode {
      // 执行 mimicry：修改 metadata.user_id
  }
  ```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 1.5：增加调试日志

**文件**：`backend/internal/service/gateway_service.go`

**任务**：
- 在 mimicry 触发点增加调试日志，记录触发原因。

**实现**：
```go
if !isRealClaudeCode {
	logger.Debug("claude_mimicry_triggered",
		"account_id", account.ID,
		"action", "system_prompt_replacement", // 或其他 action
		"reason", "not_real_claude_code_client")
	// 执行 mimicry
}
```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

### 2.2 单元测试

#### 步骤 1.6：测试 IsRealClaudeCodeRequest

**文件**：`backend/internal/service/claude_code_validator_test.go`（新增或扩展）

**测试用例**：
1. 真实 Claude Code 请求（完整 UA + system prompt + headers）→ 返回 `true`
2. 第三方客户端（UA 不匹配）→ 返回 `false`
3. 边界条件（UA 匹配但 system prompt 不匹配）→ 返回 `false`

**验证命令**：
```bash
go test -v ./backend/internal/service -run TestIsRealClaudeCodeRequest
```

---

#### 步骤 1.7：测试 Mimicry 边界

**文件**：`backend/internal/service/gateway_service_test.go`（新增或扩展）

**测试用例**：
1. 真实 Claude Code 请求不触发 system prompt 替换。
2. 真实 Claude Code 请求不触发 tool name 改写。
3. 真实 Claude Code 请求不触发 metadata.user_id 修改。
4. 第三方客户端触发以上三类 mimicry。

**验证命令**：
```bash
go test -v ./backend/internal/service -run TestClaudeMimicryBoundary
```

---

### 2.3 集成测试

#### 步骤 1.8：端到端验证

**工具**：Postman / curl / 自动化测试脚本

**测试场景**：
1. 使用真实 Claude Code UA 和 system prompt 发送 `/v1/messages` 请求。
2. 验证上游收到的请求体不包含 mimicry 改写痕迹（通过日志或抓包）。
3. 使用模拟第三方客户端 UA 发送相同请求。
4. 验证上游收到的请求体包含 mimicry 改写（system prompt、tool name、metadata）。

**验证命令**：
```bash
# 启动本地服务
make run-local

# 发送测试请求（真实 Claude Code）
curl -X POST http://localhost:8080/v1/messages \
  -H "User-Agent: claude-cli/2.1.22" \
  -H "X-App: claude-cli" \
  -H "anthropic-beta: tools-2024-01-01" \
  -H "anthropic-version: 2023-06-01" \
  -d '{ ... }'

# 发送测试请求（第三方客户端）
curl -X POST http://localhost:8080/v1/messages \
  -H "User-Agent: custom-client/1.0.0" \
  -d '{ ... }'
```

---

### 2.4 验证清单（R4.1）

- [ ] `IsRealClaudeCodeRequest()` 函数实现并编译通过
- [ ] System prompt 改写增加 `isRealClaudeCode` 判断
- [ ] Tool name 改写增加 `isRealClaudeCode` 判断
- [ ] Metadata user_id 改写增加 `isRealClaudeCode` 判断
- [ ] 调试日志记录 mimicry 触发原因
- [ ] 单元测试覆盖识别边界和 mimicry 边界
- [ ] 集成测试验证真实 Claude Code 请求不被改写
- [ ] 集成测试验证第三方客户端继续触发 mimicry
- [ ] R1/R2/R3 能力回归测试通过

---

## 3. 批次 2：Claude 上游 429 与限流状态细分类（R4.3）

### 3.1 代码实现

#### 步骤 2.1：定义 429 分类类型

**文件**：`backend/internal/service/ratelimit_service.go`

**任务**：
- 新增 `ClaudeRateLimitType` 类型和常量。

**实现**：
```go
// ClaudeRateLimitType 表示 Claude 429 限流类型
type ClaudeRateLimitType string

const (
	ClaudeRateLimitTypeUnknown       ClaudeRateLimitType = "unknown"         // 未识别（默认）
	ClaudeRateLimitTypeExtraUsage    ClaudeRateLimitType = "extra_usage"     // Extra Usage required（24h）
	ClaudeRateLimitTypeOpusWeekly    ClaudeRateLimitType = "opus_weekly"     // Opus 周限（168h）
	ClaudeRateLimitType5HourWindow   ClaudeRateLimitType = "5h_window"       // 5h 窗口限流（5h）
	ClaudeRateLimitTypeGeneric       ClaudeRateLimitType = "generic"         // 普通 429（1h）
)
```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 2.2：实现 429 分类器

**文件**：`backend/internal/service/ratelimit_service.go`

**任务**：
- 新增 `classifyClaudeRateLimit()` 函数。

**实现**：
```go
var claudeOpusWeeklyPattern = regexp.MustCompile(`(?i)claude-opus.*models per week`)

// classifyClaudeRateLimit 分类 Claude 429 响应
// 参考 CRS 的 _classifyClaudeRateLimit 实现
// 来源：claude-relay-service/src/services/claude-relay.ts
func (s *RateLimitService) classifyClaudeRateLimit(statusCode int, headers http.Header, body []byte) (ClaudeRateLimitType, time.Duration) {
	if statusCode != 429 {
		return ClaudeRateLimitTypeUnknown, 0
	}

	bodyStr := strings.ToLower(string(body))

	// 类型 B：Extra Usage required（优先级最高）
	// 关键字：extra usage required（大小写不敏感）
	if strings.Contains(bodyStr, "extra usage required") {
		return ClaudeRateLimitTypeExtraUsage, 24 * time.Hour
	}

	// 类型 C：Opus 周限
	// 关键字：claude-opus.*models per week（正则，大小写不敏感）
	if claudeOpusWeeklyPattern.MatchString(bodyStr) {
		return ClaudeRateLimitTypeOpusWeekly, 168 * time.Hour
	}

	// 类型 D：5h 窗口限流
	// 关键字：5 hour window（大小写不敏感）
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

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 2.3：集成到 HandleUpstreamError

**文件**：`backend/internal/service/ratelimit_service.go`

**任务**：
- 在 `HandleUpstreamError()` 中集成分类器。
- 找到 Claude 平台的 429 处理逻辑，增加细分类调用。

**搜索位置**：
```bash
grep -n "Platform.*Claude.*429\|statusCode == 429" backend/internal/service/ratelimit_service.go
```

**实现要点**：
```go
// 在 HandleUpstreamError() 中
if account.Platform == PlatformClaude && statusCode == 429 {
	rateLimitType, cooldownDuration := s.classifyClaudeRateLimit(statusCode, headers, body)
	
	// 记录日志
	logger.Info("claude_429_classified",
		"account_id", account.ID,
		"account_name", account.Name,
		"type", rateLimitType,
		"cooldown_duration", cooldownDuration,
		"body_snippet", truncateString(string(body), 256))
	
	// 根据分类结果设置冷却时间
	if cooldownDuration > 0 {
		until := time.Now().Add(cooldownDuration)
		reason := fmt.Sprintf("Claude rate limit: %s", rateLimitType)
		
		if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
			logger.Error("failed_to_set_temp_unschedulable", "account_id", account.ID, "error", err)
		}
		
		if s.tempUnschedCache != nil {
			state := &TempUnschedState{
				TempUnschedUntil: until,
				ErrorMessage:     reason,
			}
			if err := s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state); err != nil {
				logger.Warn("failed_to_set_temp_unsched_cache", "account_id", account.ID, "error", err)
			}
		}
		
		return true // shouldDisable = true
	}
}

// 继续现有逻辑（回退到通用 429 处理）
```

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

#### 步骤 2.4：扩展 OpsUpstreamErrorEvent

**文件**：`backend/internal/service/gateway_service.go`（或 ops 相关文件）

**任务**：
- 在 `OpsUpstreamErrorEvent` 结构体中新增 `ClaudeRateLimitType` 字段。

**实现**：
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

**任务**：
- 在设置 ops 上下文时填充 `ClaudeRateLimitType` 字段。

**验证命令**：
```bash
go build ./backend/internal/service/
```

---

### 3.2 单元测试

#### 步骤 2.5：测试 429 分类器

**文件**：`backend/internal/service/ratelimit_service_test.go`（新增或扩展）

**测试用例**：
1. **Extra Usage**：body 包含 `"extra usage required"` → 返回 `extra_usage` + 24h
2. **Opus 周限**：body 包含 `"claude-opus-4 models per week"` → 返回 `opus_weekly` + 168h
3. **5h 窗口**：body 包含 `"5 hour window"` → 返回 `5h_window` + 5h
4. **普通 429**：无 `retry-after` 和 `x-ratelimit-reset` header → 返回 `generic` + 1h
5. **未识别**：有 `retry-after` header 但 body 无关键字 → 返回 `unknown` + 0
6. **大小写不敏感**：`"EXTRA USAGE REQUIRED"` → 返回 `extra_usage`
7. **优先级**：body 同时包含多个关键字 → 按优先级返回（Extra Usage 优先）

**验证命令**：
```bash
go test -v ./backend/internal/service -run TestClassifyClaudeRateLimit
```

---

#### 步骤 2.6：测试 HandleUpstreamError 集成

**文件**：`backend/internal/service/ratelimit_service_test.go`（新增或扩展）

**测试用例**：
1. Claude 平台 + 429 + Extra Usage body → 账号设置 24h 冷却
2. Claude 平台 + 429 + Opus 周限 body → 账号设置 168h 冷却
3. Claude 平台 + 429 + 5h 窗口 body → 账号设置 5h 冷却
4. Claude 平台 + 429 + 普通 body → 账号设置 1h 冷却
5. 非 Claude 平台 + 429 → 不触发分类器（走现有逻辑）

**验证命令**：
```bash
go test -v ./backend/internal/service -run TestHandleUpstreamError_Claude429
```

---

### 3.3 集成测试

#### 步骤 2.7：端到端验证

**工具**：Mock 429 响应 / 集成测试脚本

**测试场景**：
1. 配置测试账号，触发 Extra Usage 429 响应。
2. 验证账号被设置为临时不可调度，冷却时间为 24 小时。
3. 查询日志，验证 `claude_429_classified` 日志记录了 `type: extra_usage`。
4. 查询 ops 上下文，验证 `ClaudeRateLimitType` 字段为 `"extra_usage"`。
5. 重复以上步骤，测试其他三类 429 响应。

**验证命令**：
```bash
# 触发 429 响应（使用测试脚本或手动触发）
# 查询日志
grep "claude_429_classified" /path/to/logs/sub2api.log

# 查询账号状态
# SQL: SELECT temp_unsched_until, error_message FROM accounts WHERE id = ?
```

---

### 3.4 验证清单（R4.3）

- [ ] `ClaudeRateLimitType` 类型和常量定义完成
- [ ] `classifyClaudeRateLimit()` 函数实现并编译通过
- [ ] 正则表达式 `claudeOpusWeeklyPattern` 编译为全局变量
- [ ] `HandleUpstreamError()` 集成分类器逻辑
- [ ] 分类结果记录到日志（`claude_429_classified`）
- [ ] `OpsUpstreamErrorEvent` 新增 `ClaudeRateLimitType` 字段
- [ ] 单元测试覆盖四类 429 样本和优先级
- [ ] 单元测试覆盖 HandleUpstreamError 集成逻辑
- [ ] 集成测试验证四类 429 冷却时长正确
- [ ] 集成测试验证日志和 ops 上下文字段正确
- [ ] 非 Claude 平台的 429 处理逻辑不受影响

---

## 4. 全局验证

### 4.1 回归测试

**范围**：
- R1（UA 自动拉取）能力正常工作。
- R2（WebSocket 一键全开）能力正常工作。
- R3（TLS 家族化）能力正常工作。
- OpenAI/Gemini 平台的现有 429 处理逻辑不受影响。

**验证命令**：
```bash
# 运行全量测试
go test -v ./backend/...

# 运行回归测试套件（如果存在）
make test-regression
```

---

### 4.2 性能测试

**工具**：Go benchmark / wrk / ab

**测试场景**：
1. 基准测试：1000 QPS，无 429 响应 → 延迟和吞吐量基线。
2. 429 分类测试：1000 QPS，50% 请求返回 429 → 延迟增长 < 5%。

**验证命令**：
```bash
# Go benchmark
go test -bench=BenchmarkClassifyClaudeRateLimit -benchmem ./backend/internal/service

# wrk 压测
wrk -t4 -c100 -d30s http://localhost:8080/v1/messages
```

---

### 4.3 日志审查

**检查项**：
1. `claude_mimicry_triggered` 日志仅在第三方客户端请求时出现。
2. `claude_429_classified` 日志在 Claude 429 响应时出现，且 `type` 字段正确。
3. 账号临时不可调度的 `error_message` 字段包含正确的限流类型。

**验证命令**：
```bash
# 查询 mimicry 日志
grep "claude_mimicry_triggered" /path/to/logs/sub2api.log

# 查询 429 分类日志
grep "claude_429_classified" /path/to/logs/sub2api.log

# 统计分类结果分布
grep "claude_429_classified" /path/to/logs/sub2api.log | jq '.type' | sort | uniq -c
```

---

## 5. 交付清单

### 5.1 代码文件

- [ ] `backend/internal/service/claude_code_validator.go`（扩展 IsRealClaudeCodeRequest）
- [ ] `backend/internal/service/gateway_service.go`（收紧 mimicry 边界）
- [ ] `backend/internal/service/ratelimit_service.go`（429 分类器 + 集成）
- [ ] `backend/internal/service/gateway_service.go`（扩展 OpsUpstreamErrorEvent）

### 5.2 测试文件

- [ ] `backend/internal/service/claude_code_validator_test.go`（识别边界测试）
- [ ] `backend/internal/service/gateway_service_test.go`（mimicry 边界测试）
- [ ] `backend/internal/service/ratelimit_service_test.go`（429 分类器测试）

### 5.3 文档

- [ ] `prd.md`（需求文档）
- [ ] `design.md`（技术设计）
- [ ] `implement.md`（本文档）
- [ ] 代码注释（关键函数和硬编码字段）

### 5.4 测试报告

- [ ] 单元测试覆盖率报告（目标 > 80%）
- [ ] 集成测试结果（真实 Claude Code 请求 + 第三方客户端 + 四类 429 样本）
- [ ] 回归测试结果（R1/R2/R3 + 其他平台）
- [ ] 性能测试结果（延迟增长 < 5%）

---

## 6. 回滚计划

### 6.1 R4.1 回滚

**触发条件**：
- 真实 Claude Code 请求被误判为第三方客户端，导致过度改写。
- 第三方客户端被误判为真实 Claude Code，导致请求形态不足。

**回滚步骤**：
1. 在 `gateway_service.go` 中临时禁用 mimicry 边界判断：
   ```go
   // 临时禁用：所有请求都执行 mimicry
   isRealClaudeCode := false // 原: IsClaudeCodeClient(ctx)
   ```
2. 重新部署服务。
3. 监控日志，确认回滚生效。

### 6.2 R4.3 回滚

**触发条件**：
- 429 分类器误判，导致冷却时间异常（如普通 429 被误判为 168h）。
- 分类器性能问题，导致响应延迟增加 > 10%。

**回滚步骤**：
1. 在 `ratelimit_service.go` 中禁用分类器：
   ```go
   // 临时禁用：跳过分类器，直接返回 unknown
   if account.Platform == PlatformClaude && statusCode == 429 {
       rateLimitType := ClaudeRateLimitTypeUnknown
       cooldownDuration := time.Duration(0)
       // 继续现有逻辑（解析 retry-after）
   }
   ```
2. 重新部署服务。
3. 监控日志，确认回滚生效。

---

## 7. 里程碑验证

### M1：真实 Claude Code 请求不再被过度改写（R4.1 完成）

**验收标准**：
- 使用真实 Claude Code UA 和 system prompt 发送 10 个测试请求。
- 验证日志中无 `claude_mimicry_triggered` 记录。
- 验证上游收到的请求体保留客户端原始语义（通过抓包或日志）。

**验收人**：开发负责人

**验收日期**：待定

---

### M2：四类 Claude 429/限流样本可稳定分类（R4.3 完成）

**验收标准**：
- 使用四类 429 响应样本各触发 10 次。
- 验证日志中 `claude_429_classified` 的 `type` 字段准确率 100%。
- 验证账号冷却时长符合设计（Extra Usage 24h、Opus 周限 168h、5h 窗口 5h、普通 429 1h）。

**验收人**：开发负责人

**验收日期**：待定

---

## 8. 后续工作（Phase 2/3）

本执行计划完成后，后续 Phase 可基于以下接口扩展：

- **Phase 2（R4.2）**：账号级 header profile 学习 → 在 `IsClaudeCodeClient == true` 时学习 headers。
- **Phase 2（R4.4）**：Warmup 类请求本地拦截 → 在 `IsClaudeCodeClient == true` 时识别并拦截。
- **Phase 3（R4.5）**：管理员诊断视图 → 查询 `OpsUpstreamErrorEvent.ClaudeRateLimitType` 和日志。
- **Phase 3（R4.6）**：状态文案增强 → 根据 `ClaudeRateLimitType` 生成用户友好提示。

---

## 9. 参考命令速查

```bash
# 编译检查
go build ./backend/...

# 运行单元测试
go test -v ./backend/internal/service -run TestClaudeCode
go test -v ./backend/internal/service -run TestClassifyClaudeRateLimit

# 运行全量测试
go test -v ./backend/...

# 查看测试覆盖率
go test -cover ./backend/internal/service

# 启动本地服务
make run-local

# 查询日志
grep "claude_mimicry_triggered" /path/to/logs/sub2api.log
grep "claude_429_classified" /path/to/logs/sub2api.log

# 性能测试
go test -bench=BenchmarkClassifyClaudeRateLimit -benchmem ./backend/internal/service
wrk -t4 -c100 -d30s http://localhost:8080/v1/messages
```
