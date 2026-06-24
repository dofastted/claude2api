# claude2api

> 本地优先、轻量自管的 AI API 网关。面向 Claude Code、Codex CLI、Gemini CLI，以及 OpenAI / Anthropic 兼容客户端。

[![Build](https://github.com/dofastted/claude2api/actions/workflows/backend-ci.yml/badge.svg)](https://github.com/dofastted/claude2api/actions/workflows/backend-ci.yml)
[![Security Scan](https://github.com/dofastted/claude2api/actions/workflows/security-scan.yml/badge.svg)](https://github.com/dofastted/claude2api/actions/workflows/security-scan.yml)
[![License: LGPL-3.0](https://img.shields.io/badge/License-LGPL--3.0-blue.svg)](LICENSE)

## 项目定位

claude2api 把多类上游账号收口为一个自管网关，重点是本地部署、账号隔离、客户端身份一致性和兼容协议转发。默认运行在 `simple` 模式：隐藏 SaaS 化的支付、订阅、兑换、公告入口，保留反代、账号、密钥、分组、代理和系统设置。

## 重要说明

- 仅用于技术研究、个人自用或内部自管场景。
- 使用上游账号、OAuth 凭证或 API Key 前，请自行确认符合对应服务条款、当地法律法规和组织内部规则。
- 本项目不提供上游服务额度，不托管账号，不承诺第三方商业服务可用性。
- `simple` 模式会跳过余额、套餐、订阅额度等计费校验；请只在可信环境中部署。

## 核心能力

| 能力 | 说明 |
| --- | --- |
| 多平台网关 | 提供 Anthropic Messages、OpenAI Responses、Gemini 等兼容入口。 |
| 账号管理 | 维护 Anthropic、OpenAI、Gemini、Antigravity 等账号类型。 |
| 本地鉴权 | 使用本地 API Key、用户状态、分组和 IP 限制保护网关。 |
| 调度策略 | 按平台、分组、账号可用性和代理配置选择上游账号。 |
| 客户端真实性 | 维护 Claude / Codex 客户端 UA、版本字段、会话标识和 TLS 指纹配对。 |
| 精简模式 | 默认隐藏支付、订阅、兑换、公告等 SaaS 功能入口。 |

## 环境 Profile 池

Claude 与 Codex 环境画像使用 **3 OS 槽位冻结式 Profile 池（schema v2）**：

- 每个凭证预生成并冻结 `windows` / `macos` / `linux` 三个出口槽位。
- 每个槽位固定 `device_id`，并绑定自洽的 OS、CLI 版本和 beta 能力集。
- 请求按客户端来源 OS 路由到对应槽位；并发请求复用同一槽位。
- 透传路径与 mimic 路径统一重写 `metadata.user_id.device_id` 和 `anthropic-beta`，避免版本字段不自洽。
- Profile 来源固定为模拟生成，不再学习客户端上报值。
- 管理员可在账号 UI 中按槽位手工编辑、重置或锁定关键字段。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 后端 | Go、Gin、Ent |
| 前端 | Vue 3、Vite、TailwindCSS、Pinia |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 部署 | Docker Compose / 二进制 |

## 快速部署

### Docker Compose

```bash
git clone https://github.com/dofastted/claude2api.git
cd claude2api/deploy
cp .env.example .env
```

至少设置以下环境变量：

```bash
POSTGRES_PASSWORD=change_this_secure_password
JWT_SECRET=change_this_to_a_32_byte_secret
TOTP_ENCRYPTION_KEY=change_this_to_a_32_byte_secret
RUN_MODE=simple
```

启动服务：

```bash
docker compose up -d
```

访问管理后台：

```text
http://localhost:8080
```

查看日志：

```bash
docker compose logs -f claude2api
```

### 本地开发部署

```bash
cd deploy
cp .env.example .env
```

确保 `.env` 至少包含：

```bash
POSTGRES_PASSWORD=change_this_secure_password
RUN_MODE=simple
SERVER_PORT=8080
```

启动本地依赖和服务：

```bash
docker compose -f docker-compose.dev.yml --env-file .env up -d
```

健康检查：

```bash
curl http://127.0.0.1:8080/health
```

前端开发服务器：

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

## 常用验证

```bash
# 后端目标测试
cd backend
go test ./internal/config ./internal/setup

# 前端类型检查
cd frontend
corepack pnpm@9.15.9 run typecheck

# 前端关键测试
cd frontend
corepack pnpm@9.15.9 exec vitest run src/router/__tests__/guards.spec.ts src/stores/__tests__/auth.spec.ts
```

## Nginx 反代提示

Codex CLI 会使用带下划线的 header。使用 Nginx 反代时，需要在 `http` 块启用：

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
├── docs/legal/               # 控制台合规提示文档
└── .github/workflows/        # GitHub Actions 构建与安全检查
```

## License

本项目基于 [GNU Lesser General Public License v3.0](LICENSE) 或更高版本发布。

## 致谢

感谢 Linux.do 社区在需求讨论、问题反馈、测试验证和使用经验分享中的支持。感谢原 sub2api 项目提供的基础代码与社区经验。
