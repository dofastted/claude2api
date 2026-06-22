# claude2api

面向 Claude Code、Codex CLI、Gemini CLI 与 OpenAI 兼容客户端的本地优先 AI API 网关。

claude2api 由原 sub2api 代码线演进而来，现在项目目标已切换为：优先提供轻量、可自管、适合个人和内部团队使用的反代网关。默认启动即为精简模式，不再以 SaaS 分发、支付订阅运营为项目介绍重点。

## 重要说明

- 本项目只用于技术研究、个人自用或内部自管场景。
- 使用上游账号、OAuth 凭证或 API Key 时，请自行确认符合对应服务条款、当地法律法规和组织内部规则。
- 项目不提供任何上游服务额度，不托管账号，不承诺第三方商业服务可用性。
- 默认精简模式会跳过计费、余额和订阅校验；请只在可信环境中部署。

## 当前定位

claude2api 的核心是把多类上游账号统一成稳定的 API 网关：

- 对外提供 Anthropic、OpenAI Responses、Gemini 等兼容入口。
- 通过本地 Web 管理后台维护账号、API Key、代理、分组和系统设置。
- 为 Claude / Codex OAuth 账号固定客户端环境，减少同一账号在不同设备、UA、TLS 指纹之间来回漂移。
- 支持更接近真实官方客户端的请求身份，包括客户端 UA、版本字段、会话标识和 TLS 指纹配对。
- 精简模式隐藏支付、订阅、兑换、公告等 SaaS 功能，保留核心反代和管理能力。

## 默认运行模式

默认运行模式为 `simple`。

```bash
RUN_MODE=simple
```

精简模式行为：

- 隐藏或禁用支付、订阅、兑换、公告等 SaaS 功能入口。
- 网关鉴权保留 API Key 校验、用户状态、分组和 IP 限制。
- 跳过余额、套餐、订阅额度等计费校验。
- 使用记录仍可记录，但不作为扣费依据。
- 管理后台的 `Settings` 仍可访问。

如需恢复完整 SaaS 功能，可显式设置：

```bash
RUN_MODE=standard
```

## 功能概览

### 账号与网关

- 多账号管理：Anthropic、OpenAI、Gemini、Antigravity 等账号类型。
- API Key 管理：为客户端分配本地 API Key。
- 分组与调度：按平台、分组、账号可用性选择上游账号。
- 代理配置：为不同账号配置网络代理。
- OpenAI Responses / Codex WebSocket 支持。
- Anthropic Messages 与 token 计数请求转发。
- Gemini 兼容路径转发。

### 环境 Profile 池

近期任务已将 Claude 与 Codex 环境隔离升级为按并发槽位工作的 Profile 池：

- Claude OAuth / Setup Token 账号支持 `claude_environment_profile_pool`，并兼容旧 `claude_environment_profile`。
- OpenAI OAuth / Codex 账号支持 `codex_environment_profile_pool`，并兼容旧 `codex_environment_profile`。
- 一个账号的可用槽位数来自账号 `concurrency`；`concurrency=5` 表示最多 5 个并发 Profile 槽位。
- 请求按 linux / windows / macos / desktop 环境绑定槽位；同环境请求优先复用匹配槽位，空槽首次绑定后不自动改绑。
- 当前凭据冷却、限流或槽位耗尽时，调度可切换到下一个可用凭据的匹配环境槽位。
- 管理员可在账号 UI 中查看、重置和锁定 Profile 池。

### 客户端真实性增强

项目保留并继续维护反代真实性相关能力：

- Claude / Codex 客户端 UA 与版本字段一致性。
- Codex WebSocket 相关开关集中管理。
- Claude CLI、Claude Desktop、Codex CLI、Codex Desktop 等客户端家族的 UA 与 TLS 指纹配对。
- 遥测、settings、diagnostics 等非模型路径按本地策略处理，避免误转发到模型上游。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 后端 | Go、Gin、Ent |
| 前端 | Vue 3、Vite、TailwindCSS、Pinia |
| 数据库 | PostgreSQL |
| 缓存/队列 | Redis |
| 部署 | Docker Compose / 二进制部署 |

## 快速部署

### Docker Compose

```bash
git clone https://github.com/dofastted/claude2api.git
cd claude2api/deploy
cp .env.example .env
```

编辑 `.env`，至少设置：

```bash
POSTGRES_PASSWORD=change_this_secure_password
JWT_SECRET=change_this_to_a_32_byte_secret
TOTP_ENCRYPTION_KEY=change_this_to_a_32_byte_secret
RUN_MODE=simple
```

启动：

```bash
docker compose up -d
```

访问：

```text
http://localhost:8080
```

查看日志：

```bash
docker compose logs -f sub2api
```

### 本地开发部署

```bash
cd deploy
cp .env.example .env
```

确保 `.env` 中至少包含：

```bash
POSTGRES_PASSWORD=change_this_secure_password
RUN_MODE=simple
SERVER_PORT=8080
```

启动本地开发版：

```bash
docker compose -f docker-compose.dev.yml --env-file .env up -d
```

后端健康检查：

```bash
curl http://127.0.0.1:8080/health
```

前端开发服务器可单独启动：

```bash
cd frontend
corepack pnpm@9.15.9 install
VITE_DEV_PROXY_TARGET=http://127.0.0.1:8080 corepack pnpm@9.15.9 run dev -- --host 127.0.0.1
```

## 从源码构建

前端：

```bash
cd frontend
corepack pnpm@9.15.9 install
corepack pnpm@9.15.9 run build
```

后端：

```bash
cd backend
go build -tags embed -o bin/claude2api ./cmd/server
```

运行：

```bash
RUN_MODE=simple ./backend/bin/claude2api
```

## 精简版 build tag

运行模式 `RUN_MODE=simple` 是默认启动行为。

如果需要在编译层面剥离支付和订阅相关代码，可使用 Go build tag：

```bash
cd backend
go build -tags slim ./cmd/server
```

说明：

- 默认构建不加 `slim` tag，仍包含完整代码路径。
- `-tags slim` 构建用于精简产物，目标是剥离 payment / subscription 相关依赖和路由。
- 无论是否使用 `slim` build tag，默认运行模式仍是 `simple`。

## 常用命令

```bash
# 后端测试
cd backend
go test ./internal/config ./internal/setup

# 前端目标测试
cd frontend
corepack pnpm@9.15.9 exec vitest run src/router/__tests__/guards.spec.ts src/stores/__tests__/auth.spec.ts

# 前端类型检查
cd frontend
corepack pnpm@9.15.9 run typecheck
```

## Nginx 反代提示

Codex CLI 会使用带下划线的 header。使用 Nginx 反代时，需要在 `http` 块中启用：

```nginx
underscores_in_headers on;
```

否则 `session_id` 等 header 可能被 Nginx 丢弃，影响粘性会话和环境固定能力。

## 目录结构

```text
claude2api/
├── backend/                  # Go 后端服务
│   ├── cmd/server/           # 服务入口
│   ├── ent/                  # Ent schema 与生成代码
│   └── internal/             # 配置、服务、路由、处理器等内部模块
├── frontend/                 # Vue 管理后台
│   └── src/
├── deploy/                   # Docker Compose、配置示例和部署脚本
├── docs/                     # 补充文档
└── .trellis/                 # 项目任务和开发规范
```

## License

本项目基于 [GNU Lesser General Public License v3.0](LICENSE) 或更高版本发布。

## 社区支持

感谢 Linux.do 社区在需求讨论、问题反馈、测试验证和使用经验分享中的支持。
