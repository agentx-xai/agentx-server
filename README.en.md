# AgentX Server

[中文](README.md) | English

AgentX Server is the Registry/API for team-scoped Agent environments. It stores Workspace memberships, policies, versioned manifests, immutable artifacts, devices, heartbeats, Drift, reconcile plans, audit events, and retryable outbox jobs.

## Run locally

Go 1.25 or newer is required. The default file backend is suitable for development:

```bash
cp .env.example .env
go run ./cmd/migrate
AGENTX_DATA_DIR=./data go run ./cmd/app
```

The service listens on `http://localhost:8080`. Check `/healthz`, `/readyz`, and `/metrics`. Use `AGENTX_DATABASE_URL` for PostgreSQL hosted mode. `AGENTX_ARTIFACT_STORE=s3` enables S3/MinIO artifacts.

## Authentication and workspaces

API protection supports a static Bearer token, HMAC JWT, or OIDC discovery/JWKS. Workspace routes authorize the caller as `viewer`, `developer`, `admin`, or `owner`. Hosted deployments expose scoped routes such as `/v1/workspaces/{id}/packages` and `/v1/workspaces/{id}/devices`.

Legacy unscoped device, package, audit, and Drift routes are disabled by default. Enable them explicitly for a development installation with `AGENTX_ALLOW_LEGACY_UNSCOPED=true`; this flag should remain false in production.

## API examples

```bash
agentx registry login http://localhost:8080 --token "$AGENTX_TOKEN" --workspace "$AGENTX_WORKSPACE_ID"
agentx registry publish review-skill 1.2.3 ./review-skill.tar --signature "$SIGNATURE"
agentx registry pull review-skill 1.2.3 --output ./review-skill.tar
agentx team pull --output agentx.yaml
```

Workspace package publishing accepts an `Idempotency-Key`. Reusing a key with the same request replays the stored response; using it for a different package, version, signature, or artifact is rejected. Requests are bounded in size and artifacts are limited to 51 MiB.

## Policy and storage

The policy document can require verified Ed25519 signatures and administrator approval. Pending releases cannot be downloaded through scoped artifact routes. PostgreSQL uses foreign-key cascades for Workspace-owned records. The file backend mirrors those cascades and uses atomic file replacement for local persistence.

The outbox worker retries event delivery and moves events beyond the retry limit to a dead-letter state with the final error. Audit writes are append-only. Artifact downloads verify content-addressed SHA-256 before serving file-backed objects.

## Staging

Clone `agentx-server`, `agentx-cli`, and `agentx-website` into the same parent directory. The staging Compose stack builds the console from the adjacent `agentx-website/console` checkout, and the acceptance script uses the adjacent CLI build by default.

```bash
docker compose -f docker-compose.staging.yml up -d --build
AGENTX_TOKEN=... AGENTX_WORKSPACE_ID=... ./scripts/staging-acceptance.sh
```

## Verification and release

```bash
gofmt -w $(find . -name '*.go')
go test ./...
go vet ./...
go build ./cmd/app
go build ./cmd/migrate
go build ./cmd/production-preflight
```

Push a tag matching `vMAJOR.MINOR.PATCH` to run the tag workflow. The release workflow builds Server and Migrate binaries, packages OpenAPI and migrations, and publishes `SHA256SUMS`. Verify downloads with `sha256sum -c SHA256SUMS`. See [`PRODUCT.en.md`](PRODUCT.en.md) and [`SECURITY.md`](SECURITY.md) for the full operating model.
