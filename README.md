# AgentX Server

[English](README.en.md) | 中文

<p align="center"><img src="https://raw.githubusercontent.com/agentx-xai/.github/main/profile/agentx-mark.svg" alt="AgentX" width="88"></p>

<p align="center">
  <a href="https://github.com/agentx-xai/agentx-server/actions/workflows/ci.yml"><img src="https://github.com/agentx-xai/agentx-server/actions/workflows/ci.yml/badge.svg" alt="Server CI"></a>
  <a href="https://github.com/agentx-xai/agentx-server/releases"><img src="https://img.shields.io/github/v/release/agentx-xai/agentx-server" alt="Latest release"></a>
  <a href="https://github.com/agentx-xai/agentx-server/blob/main/LICENSE"><img src="https://img.shields.io/github/license/agentx-xai/agentx-server" alt="MIT license"></a>
</p>

<p align="center">面向团队 Workspace 的 AgentX Registry、策略和设备管理服务。</p>

AgentX Server 是 Registry/API 服务，为 AI Agent 环境提供团队级的版本、权限和设备管理。它保存 Workspace Manifest、不可变 Artifact、设备 heartbeat、Drift、Reconcile plan、审批、审计事件和 outbox 任务。

## 能力

- Workspace、成员角色和策略管理
- Manifest 与不可变包版本的发布、下载和 SHA-256 校验
- 可选 Ed25519 Artifact 签名校验与审批流
- 设备注册、heartbeat、Drift 查询和自动修复计划
- 文件存储或 S3/MinIO Artifact 存储
- API Token、HMAC JWT 和 OIDC discovery/JWKS 认证
- OpenAPI 契约、分页查询和审计事件

## 本地运行

要求 Go 1.24+。单节点默认使用文件存储：

```bash
cp .env.example .env
go run ./cmd/migrate
AGENTX_DATA_DIR=./data go run ./cmd/app
```

服务默认监听 `http://localhost:8080`：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

也可以直接使用 Makefile：

```bash
make test
make build
make migrate
```

生产前建议先阅读 [`PRODUCT.md`](PRODUCT.md) 和 [`SECURITY.md`](SECURITY.md)，并使用 staging Compose 验证 PostgreSQL、MinIO、Dex OIDC、API 和控制台的组合部署。

## 配置

| 变量 | 用途 |
| --- | --- |
| `AGENTX_DATA_DIR` | 文件仓库和 outbox 的本地目录 |
| `AGENTX_DATABASE_URL` | PostgreSQL 连接；设置后使用数据库仓库 |
| `AGENTX_API_TOKEN` | 单节点 Bearer Token |
| `AGENTX_JWT_SECRET` | HMAC JWT 验证密钥 |
| `AGENTX_OIDC_ISSUER` | OIDC issuer，用于 discovery/JWKS 验证 |
| `AGENTX_ARTIFACT_STORE` | `file` 或 `s3` |
| `AGENTX_S3_ENDPOINT` / `AGENTX_S3_BUCKET` | S3/MinIO 存储配置 |
| `AGENTX_ALLOW_LEGACY_UNSCOPED` | 明确开启旧版无 workspace API；生产环境应保持 `false` |

生产部署不应把凭据写入 Manifest 或 Artifact。详细示例见 [`.env.example`](.env.example)。

## API

完整契约见 [`openapi.yaml`](openapi.yaml)。常用接口包括：

- `/healthz`、`/readyz`：存活与就绪检查
- `/v1/workspaces`：Workspace 和成员管理
- `/v1/workspaces/{id}/manifest`：团队 Manifest 版本
- `/v1/packages`：包版本列表、发布和下载
- `/v1/workspaces/{id}/devices`：设备、heartbeat 和 reconcile plan
- `/v1/drift`、`/v1/audit-events`：漂移和审计查询

CLI 通过 Registry API 使用服务端：

```bash
agentx registry login http://localhost:8080 --token "$AGENTX_TOKEN"
agentx registry publish review-skill 1.2.3 ./review-skill.tar
agentx registry pull review-skill 1.2.3 --output ./review-skill.tar
agentx team pull --output agentx.yaml
```

## Staging

Staging Compose 会启动 PostgreSQL、MinIO、Dex OIDC、API 和控制台：

```bash
docker compose -f docker-compose.staging.yml up -d --build
AGENTX_TOKEN=... AGENTX_WORKSPACE_ID=... ./scripts/staging-acceptance.sh
AGENTX_DATABASE_URL='postgres://agentx:agentx-staging@localhost:5433/agentx' ./scripts/backup-restore.sh backup
./scripts/staging-key-rotation.sh
```

CI 会执行 `go test ./...`、`go vet ./...` 和两个服务二进制的构建。产品说明见 [`PRODUCT.md`](PRODUCT.md)，安全问题见 [`SECURITY.md`](SECURITY.md)。

推送形如 `v0.1.2` 的 Tag 会触发 `.github/workflows/release.yml`，先运行测试、`go vet` 和二进制构建，再创建 GitHub Release 并上传 Server、Migrate、OpenAPI、migrations 压缩包和 SHA-256 校验和。Tag 可通过 GitHub Actions 的 `Tag` workflow 从指定分支创建。
