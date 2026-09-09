# AgentX 生产就绪审计

本文档是对外 SaaS 发布的权威门禁清单。只有所有 P0/P1 项均有当前证据且完整验收通过后，才能标记生产就绪。

## 当前结论

状态：**未达到生产发布条件**。

本轮已完成 hosted 配置失败关闭、verified-email invitation/claim、账户导出/删除、平台 legal hold 与审计保留 worker、Artifact 内容扫描与签名密钥轮换、S3 最后引用清理、CLI 实际安装/回滚及模块边界拆分、Registry OAuth Device Authorization/refresh/Workspace/session 生命周期、版本化数据库迁移、领域写入事务化、完整 RBAC 矩阵、生产 Web 镜像、Kubernetes 基线、最终 overlay 失败关闭预检、恢复演练和供应链门禁。真实 staging 已验证 PostgreSQL、MinIO、Dex OIDC、双用户邀请领取、账户重建、legal hold 创建/阻断/释放、共享/末引用 Artifact、CLI 文件 sync/rollback/再次收敛、CLI Device Flow 和浏览器控制台主流程。公共 SaaS 发布仍受所有者批准的实际保留期限与备份到期证据、真实法律文档/联系信息、许可证、远端仓库和生产资源配置阻塞。

## 发布门禁

| 领域 | 等级 | 状态 | 权威证据/缺口 |
| --- | --- | --- | --- |
| CLI 可复现安装与回滚 | P0 | 已完成 | 严格校验名称/target/本地路径/Git ref；全部输入预检后暂存 Skill；版本 2 lock 覆盖 Git revision、Skills、Rules、MCP；journal 完整回滚 Skills/Rules/MCP/lockfile；27 个 Rust 测试及真实 staging token 登录、计划、文件 sync、rollback、Drift 和再次收敛通过 |
| CLI hosted 认证与 Registry 安全 | P0 | 已完成 | OAuth Device Flow、refresh、Workspace 列表/选择、logout 与 token stdin 已实现；公开 discovery 不含 secret；远端 HTTPS（loopback 除外）、issuer 一致性、禁止 URL 凭据/query/fragment/跳转、超时/响应边界、输入预校验、凭据原子 `0600` 写入、错误净化及下载 SHA-256 写前校验均有自动测试和真实 Dex 证据 |
| Workspace 租户隔离与 RBAC | P0 | 已完成 | 跨主体 HTTP 越权测试、PostgreSQL 用户过滤、真实 OIDC owner 数据库核对，以及四种角色、20 项操作的表驱动 HTTP 权限矩阵均通过 |
| Hosted 认证与传输失败关闭 | P0 | 已完成 | API 配置测试覆盖缺失数据库/OIDC/CLI client/S3/CORS、通配来源、旧路由、PostgreSQL TLS、S3 secure、HTTPS issuer/origin 拒绝；hosted 默认拒绝 API token/HMAC，OIDC 失败不会降级。Web production 默认 hosted，构建缺少 issuer/client ID 时失败；隔离 staging 必须显式启用 bootstrap/insecure override |
| Artifact 完整性与签名策略 | P0 | 已完成 | Hosted 只接受 gzip tar Skill；服务端和 CLI 拒绝 traversal、链接、可执行文件、凭据文件及超限内容；Ed25519 新旧公钥重叠和旧钥退役拒绝测试通过 |
| PostgreSQL + S3 持久化 | P0 | 已完成基线 | staging 已通过 artifact/S3、manifest、workspace scoped device、drift；Goose up/down 保护已验证；当前 schema 8 已通过隔离数据库 restore drill，含 `legal_holds` 在内的 17 张关键表精确计数一致，生产需按季度复演 |
| Collection 扩展性与限流 | P0 | 已完成 | Hosted workspace、member、release、device、audit 通过 PostgreSQL `LIMIT/OFFSET` 分页；Web 遍历全部 cursor，201 项浏览器回归通过；两实例共享数据库限流测试、429/503 和 probe bypass 测试通过 |
| Web Console SaaS 登录与操作 | P0 | 已完成 | `oidc-client-ts` Code + PKCE、sessionStorage、自动 silent renew、单次 401 续期重试已实现；由 `/v1/me` 和 membership 推导角色并隐藏越权/非法状态操作；mock/真实 Dex、201 项分页、viewer gating、IdP 故障清理及首个 Workspace 流程通过 |
| 可观测性与恢复 | P1 | 已完成基线 | readiness、JSON tenant-aware 请求日志、HTTP/upload/Drift/worker 指标、可选 OTLP HTTP/PostgreSQL/Registry/Artifact span、Prometheus ServiceMonitor/告警、99.9% SLO、RPO 15 分钟、RTO 60 分钟和隔离恢复演练均有证据；生产需连接实际 collector/pager 并按季度复演 |
| 供应链与发布 | P1 | 已完成流水线 | CI 运行 RustSec、govulncheck、npm audit、源码 secret、OpenAPI/Kubernetes、容器 HIGH/CRITICAL 扫描和 SPDX SBOM；tag 发布向 Web archive/image 注入同一组生产 OIDC 参数，并生成 GHCR provenance/SBOM attestation、keyless 镜像签名和 checksum bundle；本地两镜像扫描为 0 HIGH/CRITICAL |
| 生产部署基线 | P0 | 已完成模板 | Kustomize 包含 TLS Ingress、ExternalSecret、3 副本 API、2 副本 Web、滚动策略、PDB、HPA、资源边界、非 root/read-only rootfs、迁移 Job 和 Prometheus 规则；typed YAML preflight 会拒绝未替换哨兵、示例/loopback endpoint、可变镜像、staging override、明文 Secret 及缺失 hosted/ExternalSecret 项；最终生产 overlay 与云端资源仍待 owner 提供 |
| API 契约 | P0 | 已完成 | OpenAPI 0.8.0 覆盖 36 个 path/45 个 HTTP operation，包括账户 export/delete 与平台 legal-hold create/list/release，并严格定义 team manifest、policy、Drift `extra`、RBAC、gzip Skill、分页、幂等和稳定错误；路由 conformance 与 Redocly 验证通过 |
| 成员外部邀请 | P0 | 已完成 | Admin 可创建、列出、撤销带角色和期限的 canonical-email invitation；只有 `email_verified` 为显式布尔 `true` 且 email 匹配的 OIDC 用户可列出/领取；claim 原子写入 user/membership/audit/outbox，真实 Dex 双用户及 PostgreSQL 测试通过；仓库不虚构邮件投递 |
| Artifact 删除生命周期 | P0 | 已完成 | Workspace 删除保留 audit tombstone 并按 digest 原子写入 cleanup outbox；worker 与发布共享 digest lock、全局复核引用，仅在最后引用消失后删除对象；真实 MinIO 验收覆盖共享保留和末引用删除 |
| 账户与保留生命周期 | P0 | 实现完成/发布仍阻塞 | 账户 JSON export/delete、owner/hold 阻断、actor/email 清理、平台 compliance allowlist、legal hold create/list/release、并发删除锁与 audit retention worker 已有 unit/HTTP/真实 PostgreSQL/Dex 证据；hosted 对 admin ID 和正数天数失败关闭。仍缺 owner/legal 批准的生产期限、发布承诺和云提供商备份到期证据 |
| 法律与外部联系 | P0 | 阻塞 | Hosted 已要求并公开渲染 Terms/Privacy/Support URL 与 abuse/security email，缺失或 production 非 HTTPS 时拒绝启动；但仍缺所有者批准的真实文档、保留/删除承诺、subprocessor 清单及 staffed 联系方式，不能由实现方虚构 |
| 开源许可证 | P1 | 已完成 | 根 `LICENSE` 采用组织 `.github` 已公布的 MIT License（Copyright 2026 AgentX contributors），与组织首页声明一致 |
| 远端代码仓库 | P0 | 已完成 | CLI、Server 和 Website/Console 分别发布到 `agentx-xai/agentx-cli`、`agentx-xai/agentx-server` 和 `agentx-xai/agentx-website` 的 `main` 分支 |

## 2026-09-09 审计记录

- 基线通过：Rust 6 个测试、Go 全量测试与 `go vet`、Vue production build、Playwright 2 个浏览器流程。
- 修复：新增 `AGENTX_DEPLOYMENT_MODE=hosted` 及启动时的强校验，阻止数据库模式在无 OIDC 的情况下以 `anonymous` 共享身份运行。
- 修复：CORS 改为配置白名单；hosted 禁止 `*`；不可信预检返回带 request ID 的稳定错误信封。
- 修复：认证中间件改由已验证配置注入；可信代理默认关闭，限流值不再在路由层直接读取环境变量。
- 加固：HTTP 服务增加读取、写入、空闲和 header 大小边界。
- 修复：升级 `pgx`、`go-jose`、`golang-jwt`、`x/text`、Vite 和 Playwright，消除扫描发现的 4 个 Go 可达漏洞及 4 个 Node 漏洞；Go 最低版本同步为 1.25。
- 门禁：CI 新增 RustSec、Go vulnerability database 和 npm 高危依赖审计；本地三项扫描均为 0 个已知漏洞。
- 文档：同步 `.env.example`、staging compose、中英文 README、产品文档和安全说明。
- 验收：完整 staging compose 构建并启动；迁移成功，API readiness 通过，真实 PostgreSQL/S3 发布下载、manifest、device、drift 流程通过。
- 验收：Playwright 通过真实 Dex 公共 PKCE 客户端登录，API 使用受限 backchannel 获取 discovery/JWKS，并完成 OIDC 用户 workspace/device 操作。
- 修复：所有 API 错误信封现返回非空 request ID；staging 实测 401 响应已验证。
- P0 修复：Gin usecase 上下文现在回退到 `http.Request.Context`，OIDC/API token principal 能进入领域授权；此前所有已认证请求会被错误折叠为共享的 `anonymous` 身份。
- 验收：跨主体 workspace 读取返回 403，另一主体的 workspace 列表不泄漏；真实 Dex 登录后的 `/v1/me` 与 PostgreSQL owner membership 均为同一 OIDC issuer/subject。
- 一致性：Idempotency-Key 绑定 artifact fingerprint；PostgreSQL 以事务 advisory lock 原子写入 release 与幂等记录。8 路并发测试得到 1 次创建、7 次 replay，不同请求复用同键被拒绝。
- 事务：workspace 发布强制 Idempotency-Key；PostgreSQL 在同一事务写入 release、幂等响应、audit 和 outbox，并发测试断言四类记录均恰好一次。
- 数据保护：无效签名不再盲删可能已被其他 release 引用的共享内容寻址 artifact。
- Artifact：hosted 上传仅接受带根目录 `SKILL.md` 的 gzip tar，并拒绝路径逃逸、重复项、链接、设备文件、可执行文件、凭据名称和压缩/解压大小超限；CLI 目录发布生成确定性 archive。
- CLI：`agent sync --target` 在所有 Artifact 预检通过后才原子替换真实 Agent Skill 目录，并保留同文件系统的一代备份；`agent rollback --target` 恢复文件而非只改状态。
- 验收：CLI HTTP 集成测试覆盖 plan、Artifact 下载、SHA-256、安全解包、heartbeat 和文件回滚；真实 PostgreSQL/S3 staging 验证同步后 drift=0、回滚后 drift=1 并重新生成安装计划。
- 迁移：`server/cmd/migrate` 改用 Goose 版本表和 PostgreSQL session advisory lock，支持 `up/status/version` 及需要显式备份确认的 `down`；重复 `up`、`down -> up` 和不可逆数据保护已实测。
- 租户隔离：设备 ID 改为 workspace scoped 文本键，`device_packages` 使用复合外键；真实 PostgreSQL 验证两个 workspace 可各自注册 `laptop-2` 且包状态不串租户。
- 事务：新增共享 `UnitOfWork`；workspace、membership、policy、manifest、device 和 approval 的领域记录、audit 与 outbox 现在复用同一 PostgreSQL 事务。故障注入证明 outbox 失败不会留下部分 workspace/audit，成功记录的 `xmin` 一致。
- 验收：真实 staging 核对 policy、manifest、device、approval 的领域记录与 audit `xmin` 一致，并存在六条预期 outbox 事件。
- 授权：新增四种角色、20 项操作的表驱动 HTTP RBAC 矩阵，覆盖成员、包、发布、下载、审批、设备、heartbeat、drift、audit、policy、manifest、reconcile 和 workspace 删除。
- Web 认证：用 `oidc-client-ts` 替换手写 PKCE，状态和 token 仅保存在 `sessionStorage`；启用自动 silent renew、过期/续期失败事件和一次性 401 续期重试，移除编译期 `VITE_API_TOKEN`。
- Web 部署：新增多阶段生产镜像，以非 root `nginx-unprivileged` 提供 SPA、同源代理 `/v1`，并设置 CSP、nosniff、frame、referrer 和 permissions 安全头。真实 Dex 登录、刷新恢复会话、workspace/device 操作及桌面/移动端视觉检查通过。
- 关闭可靠性：API 收到关闭信号后取消并等待 outbox/审计保留 worker，最多 drain 5 秒后记录告警并继续退出；Go 全量测试通过。
- OIDC 续期：独立 Playwright 场景用短生命周期会话验证真实 refresh-token grant 在过期前替换 token；token endpoint 不可达时验证续期错误、到期清理 sessionStorage 和重新登录状态。
- Artifact 密钥轮换：新增 `AGENTX_ARTIFACT_PUBLIC_KEYS` 公钥环，允许新旧 Ed25519 公钥短期重叠，并保留单钥环境变量兼容；测试验证两把钥匙均可验签且移除旧钥后旧签名被拒绝。
- 分页：workspace、membership、release、device 和 audit 的 HTTP collection 现在先解析 cursor/limit，再由 PostgreSQL 执行 `COUNT`、`LIMIT` 和 `OFFSET`；真实数据库测试验证 total 和第二页边界。Drift/Reconcile 的完整读取保留为内部计算路径。
- 限流：新增迁移 `003_shared_rate_limits.sql`；hosted 实例通过 PostgreSQL 原子 upsert 共享每 IP 分钟配额。双实例测试验证共享计数，HTTP 测试验证 429、后端故障 503 和健康/指标端点旁路。
- 恢复：新增只向 `agentx_restore_drill_*` 临时数据库恢复的安全演练脚本；初始 staging 以 schema version 3 恢复并核对当时的 14 张关键表计数一致，临时库随后删除；当前 schema 8 脚本已加入 `workspace_invitations` 与 `legal_holds`，共核对 17 张表。
- 部署：新增 Kubernetes Kustomize 基线，覆盖 TLS/cert-manager Ingress、External Secrets、迁移 Job、不可用副本为 0 的 rolling update、PDB、HPA、zone/host spreading、资源上下限、非 root/read-only rootfs 和 Prometheus Operator 规则。
- 部署预检：新增 `make production-preflight KUSTOMIZE_DIR=...`，使用 typed YAML 解码最终 Kustomize 输出；单元测试覆盖有效 production manifest，以及 placeholder/example domain、可变 image、staging override、明文 Secret、缺少 external secret 和 compliance issuer 错误。仓库的未完成 baseline 实测按预期失败并逐项报告 3 个可变镜像、示例域名和 owner-controlled 哨兵。
- 本轮预检门禁：新增 Go package/command 已通过全量 `go test ./...` 与 `go vet ./...`，Rust/Go/Web、9 个 Playwright、OpenAPI 和 Kustomize 阶段通过；live PostgreSQL、govulncheck、npm audit、源码 secret、重建镜像 HIGH/CRITICAL 扫描及 SBOM 均通过。RustSec smart-HTTP 在线 fetch 曾遇到 GitHub I/O 错误；统一 audit wrapper 仅在本地 advisory DB commit 与 GitHub API 当前 `main` commit 精确一致时允许 `--no-fetch`，当前/故意过期两条分支均已实测。最终在线路径恢复并以 1,242 条 advisory 扫描 192 个 crate，无发现。
- 供应链：PR CI 新增 server/Web image build、Trivy HIGH/CRITICAL 阻断和 SPDX SBOM；tag 工作流发布带 BuildKit provenance/SBOM attestation 的 GHCR 镜像，以 Sigstore OIDC keyless 签名 image digest 和 `SHA256SUMS` bundle。升级运行时 Alpine 包后，本地两镜像扫描均为 0 HIGH/CRITICAL。
- API 契约：OpenAPI 从 0.1.0 经 0.2.0、0.3.0、0.3.1、0.4.0、0.5.0 更新到 0.6.0，覆盖 33 个 path/40 个 operation；新增公开且不含 secret 的 `/v1/site/config` 和邀请 create/list/revoke/claim，契约继续明确 bearer auth、最低角色、分页 envelope、确定性 gzip tar Skill、安全拒绝规则、签名、release/device 幂等、Workspace scoped 文本 device ID、稳定 conflict/not-found 和非敏感内部错误。
- 错误契约：Artifact digest/version 下载的 404 从空响应改为带 request ID 的稳定错误 envelope。
- Web 修复：OIDC 用户尚无 Workspace 时返回干净空集合，不再请求 hosted 已禁用的 `/v1/devices`、`/v1/packages`、`/v1/audit-events` 和 `/v1/drift`；浏览器测试覆盖创建首个 Workspace 后切换到 scoped API。
- OAuth 契约：Web Console 现在以 access token 而不是面向客户端的 ID token 调用 API；续期测试断言刷新后的 access token 同时更新应用 bearer token 和 `oidc-client-ts` session。
- IdP 兼容：生产 Web 构建支持 `VITE_OIDC_SCOPE` 和常见的 `VITE_OIDC_AUDIENCE` authorization 参数，以便外部 IdP 签发匹配 API audience 的 access token；tag release 复用的 build workflow 现在显式接收 issuer/client/redirect URI/scope/audience，确保发布的 Web archive 与容器镜像均为可登录的 hosted 产物，必填值缺失时失败。
- 租户隔离：Device、Registry、Drift 和 Reconcile 的 workspace 操作在 adapter 缺少 scoped repository 时失败关闭，不再回退到全局列表；Drift 现实际采用 Workspace Manifest 的期望摘要。
- 错误可见性：本地 file 模式无法跨 JSON 文件事务化，legacy device/release 写入若后续审计失败会返回明确的 `committed but audit append failed` 错误；hosted PostgreSQL 路径继续保持全事务回滚。
- Worker：Run loop 会记录并计数 claim/ack 持久化错误；`/metrics` 新增 handler failure、retry、dead-letter 和 repository failure 计数，Kubernetes 对重复 cycle failure 和每个新增 dead-letter 告警。默认消费器将已交付 outbox 事件写入结构化日志，不再静默丢弃。
- 可观测性修复：全局认证中间件现在和 OpenAPI、ServiceMonitor 一致地放行 `/metrics`；带 API/OIDC 认证配置的回归测试及 staging 无凭据抓取均覆盖该行为。
- 多副本 Worker：PostgreSQL outbox claim 改为单条原子 `UPDATE ... RETURNING` 并设置五分钟 lease；两个独立连接并发 claim 测试证明同一事件只被一个 worker 取得，进程崩溃后事件可在 lease 到期重试。该并发测试使用自动删除的临时 PostgreSQL schema，与同时运行的 staging worker 隔离；ack 更新还会校验目标行确实存在。
- 门禁：根 `Makefile` 现在把 Rust/Go/Web、Console/OIDC/config 三组 Playwright、OpenAPI 和 Kubernetes 渲染纳入 `make test`，并以 `make release-check` 增加依赖审计、源码 secret scan、真实 PostgreSQL、容器扫描和 SPDX SBOM；GitHub CI 同步执行 Redocly、Kubernetes render 与源码 secret gate。
- 范围收敛：本地 Compose 删除了没有任何 server adapter 消费的 Redis profile，避免把未实现的缓存能力误报为可用功能；已实现并实测的 MinIO/S3 profile 保留。
- Auth 攻击面：删除未使用且只解析、不验证 JWT 签名的导出 helper；所有 bearer token 入口继续强制经过静态 token 常量时间比较、HMAC 验签或 OIDC JWKS 验签。
- IdP 契约：运维和部署文档现明确 hosted API 只接受 issuer/audience 匹配、经 discovery/JWKS 验签的 JWT access token，不实现 opaque token introspection；生产 IdP 必须签发对应 API JWT。
- 此前门禁快照：`make test` 全部通过（当时为 Rust 9 个测试、Clippy、Go 全量测试与 vet、Web build、Console 2 个和 OIDC 3 个 Playwright、OpenAPI、Kubernetes render）；真实 PostgreSQL repository 与 staging PostgreSQL/S3/API/CLI 验收通过，当时镜像上的真实 Dex hosted Playwright 通过。
- 最终安全证据：RustSec 以 1,242 条 advisory 扫描 192 个 crate dependency 无发现，npm 为 0，`govulncheck` 为 0 个可达漏洞；重新构建的 server/Web 镜像均为 0 HIGH/CRITICAL，SPDX 分别记录 87/72 个 package。
- 此前运行快照：staging schema version 为 4；当时 143 条 outbox 全部处理，pending/dead-letter 均为 0；worker run/handler/retry/dead-letter/repository failure 五项指标均为 0；API 日志无 5xx 或 worker failure。
- 初始提交审计：最终候选清单为 165 个源代码、配置、测试和文档文件；`server/app`、`server/migrate`、Playwright result 及各语言 build output 已明确忽略。候选中无超过 10 MiB 的文件或二进制文件，Trivy filesystem secret scan 无发现。
- 此前 Lock 快照：当时更新 progress skill 后连续两次生成的版本 2 `agentx.lock` SHA-256 均为 `5a6a34dba36f2de3f0720df4e2d21eff8d76109601da8fcc511fb120dc636443`；本轮最终值见下方最新证据。
- 幂等性：migration 004 为共享 key 表增加 resource type；Device 注册和 Release 发布都要求 key、原子保存原始响应，并拒绝跨操作或不同 fingerprint 复用。HTTP 与 PostgreSQL 8 路并发测试通过。
- 可观测性：生产 logger 默认 JSON；请求记录模板 route、actor、workspace、request ID、状态和耗时。新增 Artifact 上传失败及 Drift 报告指标，并用真实日志解析测试覆盖字段。
- Tracing：仅在标准 OTLP endpoint 环境变量存在时启用 OTLP/HTTP exporter；HTTP、pgx、Registry prepare 和 Artifact Store span 均由 in-memory recorder 测试验证。
- 错误保护：领域层以 typed error 标记可公开的 validation/authorization/not-found；未知数据库、对象存储、OIDC、文件、panic 及 readiness cause 只写日志，对外为带 request ID 的稳定 envelope。故障 repository 与 panic 回归测试验证敏感 cause 不进入响应。
- 依赖响应：首次 tracing 依赖扫描发现新发布的 OpenTelemetry/gRPC advisory，随后升级到 OpenTelemetry 1.44.0 和 gRPC 1.83.1；`govulncheck` 与 fresh Trivy database 均重新验证为 0。
- 错误语义：file/PostgreSQL repository 以可组合 sentinel 区分 conflict、not-found 和幂等冲突；usecase 不再匹配错误字符串。重复 Workspace slug、membership、release version 稳定返回公开 400，缺失审批目标返回 404；HTTP、file 和真实 PostgreSQL 回归测试均通过。
- 此前验收快照：重建 staging API 后，API/CLI PostgreSQL+S3 验收与真实 Dex 浏览器验收再次通过；hosted PostgreSQL 实测 duplicate Workspace、duplicate release 和 missing approval 分别返回 400/400/404 且均带 request ID。重建窗口内 API 无 5xx，当时 143 条 outbox 全部交付。
- CLI P0：本地 Manifest 现在全局拒绝不安全/重复名称、未知 target、绝对或逃逸的本地 Skill/Rule 路径、链接和缺少 ref 的 Git 来源；目录哈希加入路径/内容长度边界。安装先生成具体路径和 MCP 命令计划，暂存并复核所有 Skill，再以完整 journal 覆盖 Skills、共享 Rules 标记块、MCP 原生配置和 lockfile；失败自动恢复，`rollback` 恢复整代状态。版本 2 lock 同时记录请求 ref、解析 commit、Skill/Rule 哈希和 MCP 声明，`diff --target` 检查四类状态。
- CLI Hosted Auth：新增 `registry login --oidc` Device Authorization、refresh token 自动续期、`workspaces`、`use`、`logout`、`--token-stdin` 和 `AGENTX_TOKEN`。真实 Dex 验收完成 code/login，并验证 Workspace 列表/选择、team pull、refresh metadata、`0600` 和 logout；mock 测试覆盖 discovery issuer/endpoint、refresh 失败重登指引及 JSON/Artifact 大小拒绝。OAuth/provider 错误现限制为单行 1024 字符，登录提示在轮询前显式 flush。
- Hosted 传输：生产默认要求 PostgreSQL `sslmode=require|verify-ca|verify-full`、`AGENTX_S3_SECURE=true`、HTTPS OIDC issuer/backchannel 与 Console origin；只有 staging compose 显式设置 `AGENTX_ALLOW_INSECURE_HOSTED=true`。CLI OIDC public client 是 hosted 必填项。
- Verified invitation：OIDC 与 HMAC principal 保留显式布尔 `email_verified`；false、缺失或字符串 claim 均不能列出/领取邀请。Admin 可创建、查看历史及撤销 15 分钟至 30 天的邀请；领取按 canonical email 匹配并原子写入 user/membership/audit/outbox。真实 Dex `invitee@example.com` 双用户流程通过。
- Artifact GC：Workspace 删除收集候选 digest、保留删除 audit tombstone 并原子写入 cleanup outbox；worker 持 digest lock 全局复核 release 引用。发布在上传至引用提交期间持同一把锁，失败上传也安全清理/入队。真实 MinIO 验证共享 digest 在首个 Workspace 删除后保留、末引用删除后移除。
- Hosted 法律/联系配置：`AGENTX_TERMS_URL`、`AGENTX_PRIVACY_URL`、`AGENTX_SUPPORT_URL`、`AGENTX_ABUSE_EMAIL`、`AGENTX_SECURITY_EMAIL` 为 hosted 必填，production URL 强制 HTTPS；`/v1/site/config` 只暴露公开值并由 Console 渲染。Kubernetes 使用未替换即启动失败的 sentinel，但真实文档和 staffed 联系人仍由 owner/legal 提供。
- 账户生命周期：`GET /v1/me/export` 返回版本化 identity/membership/invitation/audit summary；`DELETE /v1/me` 要求精确确认，owner 或 active hold 时阻断，并在同一事务删除 membership/invitation/user、匿名化 actor/outbox personal ID、写入匿名 tombstone。Console 和真实 Dex 删除后重建空账户均通过。
- Legal hold：migration 008 新增 durable hold；只有 `AGENTX_COMPLIANCE_ADMIN_IDS` 的精确 OIDC principal 可 create/list/release，tenant principal 实测 403。hold/domain/platform-audit/outbox 原子提交；account/Workspace deletion 与 hold placement 共享 advisory lock，release 需要精确确认和原因。真实 PostgreSQL 覆盖 rollback、阻断、释放和删除，真实 Dex 覆盖两种 target。
- 审计保留：hosted 要求正数 `AGENTX_AUDIT_RETENTION_DAYS`；worker 在启动和配置间隔按批清理，placement/pruning 共享全局锁，active account/Workspace hold 数据以及 account/workspace deletion 和 hold lifecycle tombstone 永久排除。测试证明过期 unheld event 删除而 held/tombstone 保留。
- 外部 SaaS 审计：账户/hold/实时审计保留机制已实现并验收；仍需 owner/legal 选择生产天数并发布承诺，且由 PostgreSQL/S3 提供商给出真实 backup/version expiry 配置与删除证据。该证据不能由应用环境变量替代；真实 Terms/Privacy/subprocessor/support/abuse/security 信息仍为 P0 发布阻塞。
- Team Manifest：CLI 改用独立 `agentx.team.yaml` 并在网络请求前严格验证；服务端共享 parser 只接受版本 1、唯一安全包名、SemVer 和 64 位小写 SHA-256。更新时验证同 Workspace 的 Release 版本、摘要与可下载状态；Reconcile 确定排序，已保存空 Manifest 不再回退，Drift 新增 `extra`。
- Policy/Approval：Policy 只接受 `require_signature`/`require_approval` 布尔值，未配置验证器时拒绝启用签名强制；审批 repository 只允许 `pending_approval -> approved`，重复或已发布审批返回稳定 400，且回归测试确认不重复写 Audit/outbox。
- 并发：PostgreSQL 原子发布不再持有进程级全局 mutex；并发测试证明不同 Workspace/key 的 Artifact prepare 可并行，文件 fallback 仍在 lookup/save 临界区串行。幂等 key 的非空和 255 字节边界同时在 usecase 强制。
- Hosted Auth：hosted 默认拒绝静态 API token/HMAC 配置；OIDC 验证失败不再自动降级。仅 staging compose 显式设置 `AGENTX_ALLOW_HOSTED_BOOTSTRAP_AUTH=true`，生产文档明确禁止该 override。
- Web 完整性：Workspace、member、release、device、Drift 和 Audit 均遍历 `next_cursor`；控制台由 `/v1/me` 与当前 membership 推导角色，viewer 不再看到写操作，仅 `pending_approval` 向 admin 显示 Approve，pending 不显示 Download。201 项分页/viewer Playwright 回归通过。
- CLI 边界：`application.rs` 从 2,428 行降至 957 行；manifest/lock/reconcile 与纯校验进入 `domain`，Clap 进入 `interface`，artifact/filesystem/installer/Registry/source/target 进入独立 infrastructure adapter。安装 staging、snapshot、atomic replacement、journal 和设备目录回滚由 installer adapter 负责。
- CLI Registry 安全：远端 Registry 强制 HTTPS（loopback HTTP 除外），拒绝 URL 内嵌凭据、query、fragment 和 HTTP redirect；设置 10 秒连接/300 秒请求超时，在请求或文件变更前校验 token、Workspace/device ID、包名与 SemVer，凭据原子写入且 Unix mode 为 `0600`，pull 在写输出前验证 archive 与服务端 SHA-256。
- Web 失败关闭：新增显式 `VITE_DEPLOYMENT_MODE`，production 默认 hosted、development 默认 local；hosted Docker 构建缺少 OIDC issuer/client ID 时明确退出失败。运行配置缺失时仅显示持久错误，不发送 API 请求、不暴露手工 token、不显示业务导航；hosted logout 清空已加载租户数据，未认证时不显示 Create Workspace，legacy fallback 仅限 local。
- 发布补全：修复 reusable build workflow 生成未注入 OIDC 的 hosted Web archive；tag workflow 现在把 production issuer/client/redirect URI/scope/audience 传入 archive build，静态产物检查确认这些公开参数已编译进入 bundle。CI 新增 Redocly、Kubernetes render 和源码 secret scan，`release-check` 同步纳入源码扫描。
- Compliance 配置：hosted 启动现在要求每个 `AGENTX_COMPLIANCE_ADMIN_IDS` 项精确匹配已配置的 OIDC issuer 且 subject 非空，并拒绝重复项，避免配置了永远无法认证的管理员或误用其他 issuer。
- 最终候选门禁：带 live PostgreSQL URL 的 `make release-check` 全量通过，覆盖 `make test`、依赖与源码 secret 审计、完整 PostgreSQL repository gate、重建容器 HIGH/CRITICAL 扫描及 SPDX SBOM；OpenAPI 0.8.0 route conformance/Redocly lint 仍只有 4 个既有 warning。随后重建命名 staging，API/CLI 验收及 3 个真实 Dex 场景全部通过；账户场景实测 tenant admin API 403、account/Workspace hold 阻断与 release、export、删除、personal identifier 清理及同 IdP identity 空账户重建。
- 本轮运行证据：最终 staging schema version 为 8；612 条 audit、652 条 outbox 均已处理，pending/dead-letter 为 0；34 条 legal hold 均已释放，tenant-scoped hold audit 为 0。`account@example.com` user/invitation 和已删除 principal 在 audit actor/metadata、outbox payload、legal-hold target/actor 中的引用均为 0；`outbox_lease_*` 临时 schema 与 `agentx_restore_drill_*` 临时数据库均为 0；真实 MinIO 再次通过共享 digest retained 和末引用 garbage collection。重建 API 容器后不重建 Web，Nginx 仍通过 Docker DNS 动态解析并成功代理 `/v1/site/config`。
- 本轮恢复证据：schema version 8 从保存的 staging 卷恢复到唯一的 `agentx_restore_drill_*` 临时数据库，含 `workspace_invitations`、`legal_holds` 在内的 17 张关键表逐表精确计数一致，脚本退出时删除临时数据库。
- 本轮安全证据：`make audit` 在线刷新后，RustSec 以 1,242 条 advisory 扫描 192 个 crate dependency 无发现，`govulncheck` 无可达漏洞，npm audit 为 0；重新构建的 server/Web 镜像均为 0 HIGH/CRITICAL，SPDX 分别记录 87/72 个 package。
- 最终候选审计：203 个候选文件，最大 50,822 bytes，无二进制或媒体 archive，无超过 10 MiB 文件；build output、SBOM、Playwright result 与依赖目录均由 `.gitignore` 排除，Trivy filesystem secret scan 无发现；workflow 通过 Ruby YAML 解析与 `actionlint` v1.7.7。
- 远端发布：CLI、Server 和 Website/Console 维持独立仓库与独立发布流水线；跨端 staging 验收要求三个仓库位于同一父目录。
- Lock 一致性：所有 skill 变更后从仓库根目录连续两次执行 `cargo run --manifest-path cli/Cargo.toml -- lock`，`agentx.lock` SHA-256 均为 `794855e20ae9d57b307072a0fb83a1ee6a80173c6333c23a2db262bf06a5de2b`；全部 8 个仓库 AgentX skill 均通过 skill-creator `quick_validate.py`。

## 下一轮优先级

1. 由所有者/法律团队批准 `AGENTX_AUDIT_RETENTION_DAYS` 的生产值、发布保留/删除承诺，并从选定 PostgreSQL/S3 提供商取得 backup/version expiry 配置和实际删除证据。
2. 由所有者/法律团队提供 Terms、Privacy、subprocessor 以及 support/abuse/security 联系方式。
3. 在生产账户替换 Kubernetes 示例域名、OIDC/S3/SecretStore 引用和 image digest，并接通告警 pager、PITR 与跨区域对象复制。
4. 维护三个公开仓库的独立版本号、发布说明和供应链证据。
5. 对最终生产 overlay 执行 `make production-preflight KUSTOMIZE_DIR=...`，通过后再进行 server-side dry-run 和 apply。
6. 首次生产发布后执行真实域名的 smoke、OIDC、publish/download、CLI sync/rollback 和灾备验收。
