# AgentX Server Product Guide

[中文](PRODUCT.md) | English

## Purpose

The Server is the hosted control plane for AgentX. It gives a team one Workspace Manifest, immutable package artifacts, explicit membership roles, policy enforcement, device state, Drift, reconcile planning, audit history, and asynchronous outbox processing. The CLI remains responsible for local native Agent files.

## Workspace model

Each Workspace has members with roles:

- `viewer`: read Workspace, releases, devices, Drift, and audit data.
- `developer`: register devices, send heartbeats, and publish releases.
- `admin`: update policies and manifests, manage members, and approve releases.
- `owner`: delete the Workspace and perform owner-level lifecycle operations.

Every scoped service authorizes the caller before reading or writing data. Repositories must implement workspace-aware interfaces; the service returns an error instead of falling back to a global collection.

## Manifest and policy

A manifest document normally contains package entries with `name`, `version`, and `sha256`:

```json
{
  "version": 1,
  "packages": [{"name": "review-skill", "version": "1.2.3", "sha256": "<digest>"}]
}
```

The current policy supports `require_signature` and `require_approval`. Signature enforcement requires a configured Ed25519 public key. Approval puts a release in `pending_approval`; scoped download is blocked until an administrator approves it.

## Devices and reconciliation

Devices are registered inside a Workspace and report an Agent, status, and installed package digest map through heartbeat. Drift compares that map with the current Workspace Manifest and reports `missing`, `changed`, and `unexpected` packages; an intentionally empty Manifest therefore identifies every installed package as unexpected. A reconcile plan returns `install`, `update`, or `remove` actions and the manifest revision used to generate them. The CLI downloads only the required artifact, verifies its digest, stores a previous local state, and then sends heartbeat.

## Persistence

The file backend is intended for a single development node. It persists workspaces, memberships, policies, manifests, packages, devices, audit, idempotency records, and outbox events as JSON with atomic replacement and in-process synchronization. Deleting a Workspace removes all of its local records. Hosted mode uses PostgreSQL; package objects can be stored in the local filesystem or S3/MinIO. The SQL migration is [`migrations/001_initial.sql`](migrations/001_initial.sql).

## API and limits

Business routes are under `/v1`. Common scoped routes are:

| Method | Path | Role |
| --- | --- | --- |
| `GET` | `/v1/workspaces` | authenticated user |
| `GET/PUT` | `/v1/workspaces/:id/manifest` | viewer/admin |
| `GET/PUT` | `/v1/workspaces/:id/policies` | viewer/admin |
| `POST` | `/v1/workspaces/:id/packages/:name/releases` | developer |
| `POST` | `/v1/workspaces/:id/packages/:name/:version/approve` | admin |
| `GET/POST` | `/v1/workspaces/:id/devices` | viewer/developer |
| `GET` | `/v1/workspaces/:id/drift` | viewer |
| `GET` | `/v1/workspaces/:id/devices/:device_id/plan` | viewer |

JSON request bodies are capped at 2 MiB and artifact uploads at 51 MiB. Package names are restricted to safe ASCII identifiers and versions use semantic-version syntax. Collection routes support `limit` and cursor pagination. Idempotency fingerprints bind a key to the original package, version, signature, and artifact bytes.

## Security and operations

Use HTTPS, OIDC or short-lived JWTs, least-privilege Workspace roles, a secret manager, and private artifact storage in production. Never put bearer tokens, cloud credentials, refresh tokens, or executable untrusted MCP commands in a manifest. Keep `AGENTX_ALLOW_LEGACY_UNSCOPED=false` in hosted deployments. Monitor `/readyz`, `/metrics`, request IDs, audit events, and dead-letter outbox entries.

Staging Compose provisions PostgreSQL, MinIO, Dex OIDC, API, and console. Before production, run the staging acceptance script, backup/restore drill, artifact verification, OIDC key rotation, and a second-device sync/rollback test.

## Build and release

```bash
go test ./...
go vet ./...
go build ./cmd/app
go build ./cmd/migrate
```

A `vMAJOR.MINOR.PATCH` tag runs these checks before publishing Server, Migrate, OpenAPI, migrations, and `SHA256SUMS`. Consumers should verify the checksum file before installation.
