# Phase 1: CC 请求识别与 429 细分类 (R4.1+R4.3) - PRD

## 1. 背景与目标

### 任务定位

本任务是 **sub2api Claude Code 反代真实性增强** 的 Phase 1，聚焦两个核心需求：

- **R4.1**：Claude Code 请求识别与改写边界收紧（P0，M 工作量）
- **R4.3**：Claude 上游 429 与限流状态细分类（P1，M 工作量）

这是在 R1（UA 自动拉取）、R2（WebSocket 一键全开）、R3（TLS 家族化）基础上的进一步增强，为后续 Phase 2（账号级画像与本地拦截）和 Phase 3（运营诊断与收口）奠定基础。

### 目标成果

1. **真实 Claude Code 请求不被过度改写**：明确识别真实 Claude Code 客户端，避免不必要的 system prompt、tool name、cache_control 等改写。
2. **第三方客户端继续 mimicry**：非真实 Claude Code 客户端仍按现有策略进行请求形态伪装。
3. **Claude 429/限流状态可细分**：区分普通 429、Extra Usage required、Opus 周限、5h 窗口限流等四类状态。

### 成功指标

- 真实 Claude Code 请求不会触发 system prompt 全量替换。
- 真实 Claude Code 请求不会触发不必要的 tool name 随机化或 PascalCase 改写。
- 真实 Claude Code 请求的 metadata user id 不会被无差别修改。
- 第三方客户端仍能按现有 mimicry 策略补齐必要 Claude Code 请求形态。
- Claude 429 响应至少能区分四类状态：无 reset header 的普通 429、Extra Usage required、Opus weekly limit、5h window rate limit。
- 不回退 sub2api 现有 SSE event block 状态机，不引入新的旁路代理。

---

## 2. 功能需求详解

### R4.1 Claude Code 请求识别与改写边界收紧

#### 业务价值

减少真实 Claude Code 请求被过度改写造成的协议差异，让 sub2api 只在必要时执行 mimicry，降低被识别为第三方代理的风险。

#### 当前问题

sub2api 已在 `GatewayService.Forward` 中支持 Claude mimicry：对 OAuth/SetupToken 账号且非真实 Claude Code 客户端的请求，执行 system prompt、metadata user id、cache_control、工具名等改写。当前问题是 Claude 专用策略分散在通用网关链路中，真实 Claude Code 请求识别边界需要继续收紧。

#### CRS 参考实现

CRS 在 Claude relay service 中集中处理 Claude Code 请求形态：区分真实 Claude Code 请求和需要伪装的客户端，对非真实请求才替换 system、补 metadata、处理 billing header 与 beta header。

#### 改进方案

- 明确定义「真实 Claude Code 请求」判定条件，包括 UA、Claude Code 相关 headers、metadata、请求入口和账号类型。
- 将判定结果作为请求上下文中的显式状态，供后续 system、metadata、tool name、cache_control、beta header 处理复用。
- 对真实 Claude Code 请求默认保留客户端原始语义，只做安全必要的协议修正。
- 对第三方客户端继续进入现有 mimicry 流程，并保持 R1/R3 的 UA 与 TLS 家族化配对策略。

#### 验收标准

- 真实 Claude Code 请求不会触发 system prompt 全量替换。
- 真实 Claude Code 请求不会触发不必要的 tool name 随机化或 PascalCase 改写。
- 真实 Claude Code 请求的 metadata user id 不会被无差别修改。
- 第三方客户端请求仍进入现有 mimicry 流程，且不影响 R1/R3 策略。
- 可通过日志/调试模式查看「真实 Claude Code 请求」判定状态。

---

### R4.3 Claude 上游 429 与限流状态细分类

#### 业务价值

区分 Claude 不同类型的 429/限流状态，为账号级冷却策略、管理员诊断、用户错误提示提供细粒度信息。

#### 当前问题

sub2api 当前对 Claude 429 响应的处理较粗粒度，未区分普通 429、Extra Usage required、Opus 周限、5h 窗口限流等不同场景。这导致：

- 账号冷却策略无法根据限流类型调整（如 5h 窗口限流应冷却 5 小时，而非固定 1 小时）。
- 管理员无法快速判断账号不可用的具体原因。
- 用户收到的错误提示模糊，无法指导后续操作。

#### CRS 参考实现

CRS 在 `_classifyClaudeRateLimit` 函数中解析 429 响应的 body、headers（`retry-after`、`x-ratelimit-reset` 等），根据关键字段细分四类状态：

- **类型 A**：无 `retry-after` / `x-ratelimit-reset` header → 普通 429（1 小时冷却）。
- **类型 B**：body 含 `extra usage required` → Extra Usage 限制（24 小时冷却）。
- **类型 C**：body 含 `claude-opus.*models per week` → Opus 周限（168 小时冷却）。
- **类型 D**：body 含 `5 hour window` → 5h 窗口限流（5 小时冷却）。

#### 改进方案

- 在 Claude 响应处理链路中，增加 429/限流状态分类器。
- 解析 429 响应的 body 和 headers，根据关键字段映射到细分状态。
- 将分类结果存储到请求上下文或日志中，供后续冷却策略、诊断视图、错误提示使用。
- 保持 sub2api 现有 SSE event block 状态机不变，不引入新的旁路代理。

#### 验收标准

- 四类 Claude 429/限流样本（无 reset header、Extra Usage、Opus 周限、5h 窗口）可稳定分类。
- 分类结果可在日志中查询到（如 `rate_limit_type: extra_usage_required`）。
- 分类逻辑不依赖外部配置文件，关键字段硬编码在代码中以提升稳定性。
- 不影响 OpenAI/Gemini 等非 Claude 平台的 429 处理逻辑。

---

## 3. 非目标

- **不实现账号级 header profile 学习**：这是 Phase 2（R4.2）的范围。
- **不实现 warmup/title/suggestion 本地拦截**：这是 Phase 2（R4.4）的范围。
- **不实现管理员诊断视图与冷却覆盖**：这是 Phase 3（R4.5）的范围。
- **不实现账号状态文案增强**：这是 Phase 3（R4.6）的范围。
- **不改造 OpenAI/Gemini 等非 Claude 平台逻辑**：本任务仅聚焦 Claude。

---

## 4. 技术约束

- **保持 sub2api 现有 SSE event block 状态机不变**：不引入新的旁路代理或流式响应处理机制。
- **不回退 R1/R2/R3 能力**：UA 自动拉取、WebSocket 一键全开、TLS 家族化框架需保持正常工作。
- **关键字段硬编码**：429 分类逻辑中的关键字段（如 `extra usage required`、`5 hour window`）硬编码在代码中，不依赖外部配置文件。
- **兼容现有账号调度逻辑**：Claude Code 请求识别与 429 分类结果需融入现有账号调度、冷却、健康检查流程，不引入新的并行调度器。

---

## 5. 依赖与风险

### 依赖

- **现有 mimicry 流程**：R4.1 需要理解 sub2api 当前的 system prompt、metadata、tool name、cache_control 改写逻辑。
- **Claude 429 响应样本**：R4.3 需要四类真实 Claude 429 响应样本用于测试（可从历史日志或手动触发获取）。

### 风险

- **Claude Code 请求识别误判**：如果识别条件过于宽松，可能将第三方客户端误判为真实 Claude Code，导致请求形态不足；如果条件过严，可能将真实 Claude Code 误判为第三方客户端，导致过度改写。
  - **缓解措施**：先保守识别（只识别高置信度的真实 Claude Code 请求），后续根据日志反馈迭代。
- **429 分类关键字段变化**：Claude 官方可能调整 429 响应的 body 或 headers 格式，导致分类失效。
  - **缓解措施**：硬编码关键字段并在代码注释中标注来源，定期回归测试。

---

## 6. 验收清单

### R4.1 Claude Code 请求识别与改写边界收紧

- [ ] 明确定义「真实 Claude Code 请求」判定函数或上下文状态。
- [ ] 真实 Claude Code 请求不触发 system prompt 全量替换。
- [ ] 真实 Claude Code 请求不触发不必要的 tool name 随机化。
- [ ] 真实 Claude Code 请求的 metadata user id 不被无差别修改。
- [ ] 第三方客户端请求仍进入现有 mimicry 流程。
- [ ] 可通过日志/调试模式查看判定状态。

### R4.3 Claude 上游 429 与限流状态细分类

- [ ] 四类 Claude 429/限流样本可稳定分类（普通 429、Extra Usage、Opus 周限、5h 窗口）。
- [ ] 分类结果可在日志中查询到（如 `rate_limit_type: extra_usage_required`）。
- [ ] 分类逻辑不依赖外部配置文件，关键字段硬编码在代码中。
- [ ] 不影响 OpenAI/Gemini 等非 Claude 平台的 429 处理逻辑。
- [ ] 不引入新的旁路代理或流式响应处理机制。

### 全局

- [ ] R1/R2/R3 能力保持正常工作（UA 自动拉取、WebSocket 一键全开、TLS 家族化）。
- [ ] 不影响 OpenAI/Gemini 等非 Claude 平台的现有逻辑。
- [ ] 代码有充分的单元测试覆盖（Claude Code 请求识别 + 429 分类）。
- [ ] 有集成测试验证真实 Claude Code 请求与第三方客户端请求的处理差异。

---

## 7. 里程碑

- **M1**：真实 Claude Code 请求不再被过度改写（R4.1 完成）。
- **M2**：四类 Claude 429/限流样本可稳定分类（R4.3 完成）。

---

## 8. 参考资料

- 父 PRD：`.trellis/tasks/06-21-proxy-realism-enhancement/prd-claude-code-enhancement.md`
- CRS 架构对比：`.trellis/tasks/06-21-proxy-realism-enhancement/claude-relay-vs-sub2api.md`
