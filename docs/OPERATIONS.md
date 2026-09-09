# AgentX production operations

## Service objectives

- Availability objective: 99.9% monthly for authenticated API and Console traffic.
- Recovery point objective (RPO): 15 minutes for PostgreSQL and immutable artifact metadata.
- Recovery time objective (RTO): 60 minutes for a regional database restore and application redeploy.
- Artifact objects are immutable and versioned. Cross-region replication or a second bucket must meet the same 15-minute RPO.

These are release requirements, not claims about a provider. Record measured backup lag and drill duration after every quarterly restore rehearsal. Page the on-call owner when RPO lag exceeds 15 minutes or the critical availability alerts fire.

## Release order

1. Verify signed images and release checksums/SBOMs in CI.
2. Render the final production overlay and run `make production-preflight KUSTOMIZE_DIR=path/to/production-overlay`. Do not run it against the intentionally incomplete baseline and treat every finding as a deployment blocker.
3. Snapshot PostgreSQL and confirm object-store versioning/replication health.
4. Apply the new migration Job and wait for success.
5. Confirm the Web image was built with `VITE_DEPLOYMENT_MODE=hosted` plus the production OIDC issuer/client ID. Confirm the API has PostgreSQL TLS, secure S3 transport, HTTPS OIDC/Console origins, a Device Authorization public client, exact compliance principals in `AGENTX_COMPLIANCE_ADMIN_IDS`, an owner-approved positive `AGENTX_AUDIT_RETENTION_DAYS`, and owner-approved `AGENTX_TERMS_URL`, `AGENTX_PRIVACY_URL`, `AGENTX_SUPPORT_URL`, `AGENTX_ABUSE_EMAIL`, and `AGENTX_SECURITY_EMAIL`; roll out the API, verify `/v1/site/config`, run `/readyz` and the staging acceptance flow, then roll out Web.
6. Watch replica availability, HTTP failure ratio, PostgreSQL saturation, and object-store errors for at least 15 minutes.

Migrations are forward-first. Do not run `down` unless a restored backup has been verified and `AGENTX_ALLOW_MIGRATION_DOWN=true` is explicitly approved. A failed application rollout should normally roll back the Deployment image while leaving an additive schema in place.

## Incident and rollback

- Expected domain conflicts and missing resources return public 4xx envelopes with request IDs. Duplicate Workspace slugs, memberships, release versions, and incompatible idempotency-key reuse return 400; missing release approval targets return 404. Treat a 500 as an unexpected repository or dependency failure and correlate it by request ID rather than retrying a deterministic 4xx.
- API unavailable: inspect `/readyz`, PostgreSQL connectivity, S3 health, OIDC discovery/JWKS, and rate-limit table errors. Keep `/healthz` and `/metrics` reachable during dependency incidents.
- Authentication failures: confirm the IdP issued a signed JWT access token rather than an opaque token, compare issuer/audience with the deployed Web build, check IdP discovery/JWKS, and do not enable anonymous, legacy unscoped routes, or `AGENTX_ALLOW_HOSTED_BOOTSTRAP_AUTH` in production. AgentX does not implement token introspection; a failed OIDC token does not fall back to static credentials unless that staging-only override is explicit.
- Console configuration failures: confirm `VITE_DEPLOYMENT_MODE=hosted`, `VITE_OIDC_ISSUER`, and `VITE_OIDC_CLIENT_ID` were embedded during the image build. The expected fail-closed state is a persistent configuration error with no API traffic, business navigation, or manual token control; rebuild the image rather than enabling local fallback.
- CLI Registry failures: use HTTPS except for loopback development, remove URL credentials/query/fragment components, and verify token, Workspace/device IDs, package name, and SemVer inputs. For `--oidc`, verify `/v1/auth/config`, issuer equality, Device Authorization support, refresh grant/scope, and the public client registration. Redirects are intentionally disabled; point the CLI directly at the canonical Registry and issuer origins. A refresh failure intentionally requires a fresh login. Treat a digest mismatch as artifact corruption and do not reuse the downloaded output.
- Artifact failures: stop publishing, retain immutable objects, validate SHA-256 and signature policy, and rotate public keys with an overlap window. For cleanup failures, inspect `artifact.cleanup` outbox retries; do not delete an object manually until a global digest-reference check confirms it is unreferenced.
- Outbox failures: inspect `agentx_worker_run_failures_total` and `agentx_worker_repository_failures_total`, then check PostgreSQL connectivity and worker logs. Every increase in `agentx_worker_dead_letters_total` is a critical alert; preserve the row and its last error until the cause is corrected and replay is explicitly planned.
- Bad release: `kubectl rollout undo deployment/agentx-api -n agentx` or the Web equivalent, then rerun acceptance checks.

## Backup and restore

Use encrypted PostgreSQL backups with point-in-time recovery, daily full backups, and transaction-log shipping no less frequently than every 15 minutes. Retain 35 daily and 12 monthly recovery points. Enable bucket versioning, retention, and cross-region replication for artifacts. Restrict restore credentials to the recovery role.

Quarterly rehearsal:

1. Restore the latest database backup into an isolated database or account, never over production.
2. Restore or mount a read-only replica of the artifact bucket.
3. Apply the current migrations, start isolated API/Web instances, and run `scripts/staging-acceptance.sh` with isolated credentials.
4. Verify workspace/member/release/device/audit counts, sample artifact hashes, OIDC login, publish/download, and CLI sync/rollback.
5. Record backup timestamp, achieved RPO, start/end timestamps, achieved RTO, evidence links, and any corrective action in `docs/PRODUCTION_READINESS.md`.

The local staging database can be rehearsed without touching its source data:

```bash
AGENTX_COMPOSE_PROJECT=agentx-staging ./scripts/staging-backup-restore-drill.sh
```

The script creates a uniquely named database with the required `agentx_restore_drill_` prefix, streams a custom-format backup into it, compares exact critical-table counts, and drops only that temporary database.

Hosted workers claim outbox rows with a five-minute lease before delivery. This prevents concurrent replicas from claiming the same available row; if a process stops before acknowledgement, the event becomes claimable again after the lease expires.

## External account and data lifecycle

Workspace administrators can create, list, and revoke expiring invitations for canonical email addresses. A claimant can list and claim only invitations matching an OIDC `email` claim accompanied by an explicit boolean `email_verified: true`; missing, false, or string-valued verification claims fail closed. Claiming atomically creates the user, membership, audit event, and outbox event. Monitor invitation creation for abuse at the identity-provider and API rate-limit layers; this repository does not send invitation email.

Workspace deletion atomically records a preserved deletion audit tombstone and enqueues cleanup for every candidate digest. The cleanup worker takes the same digest-scoped lock used by publication, globally rechecks references, and deletes only an unreferenced object; retries and dead letters use the durable outbox.

Authenticated users can download a schema-versioned JSON export from `GET /v1/me/export`. `DELETE /v1/me` requires the exact `DELETE` confirmation, refuses accounts that own a Workspace or have an active legal hold, removes non-owner memberships and addressed invitations, anonymizes retained actor references, and writes anonymous deletion audit/outbox tombstones in the same transaction.

Legal holds are a platform compliance operation, not a Workspace-owner permission. Configure exact immutable OIDC `issuer|subject` values in `AGENTX_COMPLIANCE_ADMIN_IDS`; use separate named staff identities, review this allowlist on every access change, and do not authorize a shared bootstrap token in production. The API requires an authenticated configured principal:

```bash
curl -fsS -H "Authorization: Bearer $ACCESS_TOKEN" \
  'https://registry.example/v1/admin/legal-holds?status=active'
curl -fsS -X POST -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Content-Type: application/json' \
  -d '{"target_type":"account","target_id":"https://id.example|subject","reason":"case reference"}' \
  https://registry.example/v1/admin/legal-holds
curl -fsS -X POST -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Content-Type: application/json' \
  -d '{"confirmation":"RELEASE","reason":"written release approval reference"}' \
  https://registry.example/v1/admin/legal-holds/LEGAL_HOLD_UUID/release
```

Creation and release atomically persist the hold, a platform-scoped audit event, and an outbox event. Hold reasons are not written into tenant-scoped audit feeds. Account and Workspace deletion share target advisory locks with hold creation, so deletion cannot race a new hold. Release requires the exact `RELEASE` confirmation and a reason; never release without the documented approval required by the operator's legal procedure.

The audit-retention worker runs at startup and every `AGENTX_AUDIT_RETENTION_INTERVAL` (default `24h`), pruning bounded batches older than `AGENTX_AUDIT_RETENTION_DAYS`. Placement and pruning share a global advisory lock. Events attributed to an account or Workspace under active hold are preserved, as are account/Workspace deletion and legal-hold lifecycle tombstones. Alert on `audit retention cycle failed` logs. Retention duration changes require owner/legal approval and must match the published notice.

Backup expiry is outside the live-row worker. Configure lifecycle and deletion in the selected PostgreSQL backup/PITR and object-version providers, then retain provider policy exports plus observed expiry/deletion evidence. Do not add an application environment variable that merely claims backups expired. Hosted startup can enforce that a live-audit duration was selected, but it cannot prove provider backup deletion.

Before external use, publish the actual Terms, Privacy and retention notices, subprocessors, and staffed contacts. These are release gates, not defaults supplied by the repository.

## Observability

Set `OTEL_EXPORTER_OTLP_ENDPOINT` (or the trace-specific `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) to enable OTLP/HTTP export. When neither endpoint is set, the API uses the no-op tracer and makes no collector connection. Standard OpenTelemetry headers, TLS, compression, and resource environment variables are handled by the OTLP exporter.

Request spans use template routes and include status, actor, and workspace attributes. Child spans cover PostgreSQL queries, Registry release preparation, and artifact-store operations. JSON request logs carry the same request ID, route, actor, workspace, status, and duration fields. Monitor `agentx_artifact_upload_failures_total` and `agentx_drift_reports_total` alongside the HTTP and worker counters.
