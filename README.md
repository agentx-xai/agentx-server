# AgentX Server

AgentX Registry/API 服务，提供 Workspace、成员权限、Manifest、不可变 Artifact、设备 heartbeat、Drift、Reconcile plan、审批、审计和 outbox worker。

## 本地运行

```bash
AGENTX_DATA_DIR=./data go run ./cmd/app
```

## Hosted staging

```bash
docker compose -f docker-compose.staging.yml up -d --build
AGENTX_TOKEN=... AGENTX_WORKSPACE_ID=... ./scripts/staging-acceptance.sh
```

API 契约见 [`openapi.yaml`](openapi.yaml)，产品和使用文档见 [`PRODUCT.md`](PRODUCT.md)。

声明式管理 Codex、Claude Code 等 AI Agent 的 Skills 和工作环境。

## 当前状态

Rust CLI 已实现本地 Skill 生命周期、Codex/Claude 的 Rules 和 MCP 配置适配，以及 Registry `login`、`publish`、`pull`；通过 `--workspace` 使用 workspace scoped Registry，`team pull`/`team push` 可同步 workspace 团队 manifest。Go API 提供 workspace/membership、策略执行、版本化 manifest、Registry、审计、Drift、设备 heartbeat、cursor 分页和 scoped artifact 下载；Vue 3 控制台支持 workspace、成员角色、manifest、策略、Registry、设备、Drift 和 Audit 操作。

```bash
cd cli
cargo run -- init
cargo run -- doctor
cargo run -- install --yes
cargo run -- diff
```

CLI 远程凭据保存在用户配置目录，拉取时会重新校验 SHA-256。服务端可使用 PostgreSQL、API token、HMAC JWT 或配置 `AGENTX_OIDC_ISSUER` 启用 OIDC discovery/JWKS 验证，并支持 Ed25519 artifact 签名验证。

## 服务端

```bash
cd server
AGENTX_DATA_DIR=../data go run ./cmd/app
```

API 文档见 [`server/openapi.yaml`](server/openapi.yaml)，本地单节点部署见 [`docker-compose.yml`](docker-compose.yml)。开源贡献规则见 [`CONTRIBUTING.md`](CONTRIBUTING.md)，安全问题见 [`SECURITY.md`](SECURITY.md)。

可选依赖使用本机已有镜像启动：`docker compose --profile cache up -d`，或 `docker compose --profile object-store up -d`。默认 API 不依赖 Redis/MinIO。

Hosted artifact storage 可通过 `AGENTX_ARTIFACT_STORE=s3` 启用 S3/MinIO 适配器，并设置 `AGENTX_S3_ENDPOINT`、`AGENTX_S3_ACCESS_KEY`、`AGENTX_S3_SECRET_KEY`、`AGENTX_S3_BUCKET` 和可选的 `AGENTX_S3_SECURE=true`。发布、workspace、策略、manifest、设备和成员变更会写入 outbox，由内置 worker 重试处理。

策略文档可使用 `{"require_signature":true}` 强制签名，或使用 `{"require_approval":true}` 将新版本置为 `pending_approval`；审批前 workspace 下载会被拒绝。

Hosted Registry 使用前先执行 `server/migrations/001_initial.sql`。常用远程命令：

```bash
agentx registry login https://registry.example --token "$AGENTX_TOKEN"
agentx registry login https://registry.example --token "$AGENTX_TOKEN" --workspace "$AGENTX_WORKSPACE_ID"
agentx registry publish my-skill 1.2.3 ./my-skill.tar --signature "$SIGNATURE"
agentx registry pull my-skill 1.2.3 --output ./my-skill.tar
agentx team pull --output agentx.yaml
agentx team push --input agentx.yaml
```

设备闭环命令会从 workspace manifest 生成计划，下载并校验 artifact，写入本地设备缓存，再用 heartbeat 上报实际状态；每次同步会保存上一状态，可执行回滚：

```bash
agentx agent plan --device laptop-2
agentx agent sync --device laptop-2
agentx agent rollback --device laptop-2
```

生产前可用 staging compose 启动 PostgreSQL、MinIO、Dex OIDC、API 和控制台，并运行验收脚本。脚本要求一个通过 OIDC 登录得到的 bearer token 和 workspace ID：

```bash
docker compose -f docker-compose.staging.yml up -d --build
AGENTX_TOKEN=... AGENTX_WORKSPACE_ID=... ./scripts/staging-acceptance.sh
AGENTX_DATABASE_URL='postgres://agentx:agentx-staging@localhost:5433/agentx' ./scripts/backup-restore.sh backup
./scripts/staging-key-rotation.sh
```

服务提供 `/readyz`、`/metrics`，outbox 事件超过最大重试次数会进入 dead-letter 并记录最后错误；`AGENTX_RATE_LIMIT_PER_MINUTE` 可调整 API 限流。
