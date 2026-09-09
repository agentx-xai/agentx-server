# Kubernetes production baseline

This directory is a Kustomize baseline, not a turnkey cloud account. Before applying it:

1. Replace both image tags with immutable release digests. Build the Web image with the public `VITE_OIDC_ISSUER`, `VITE_OIDC_CLIENT_ID`, and `VITE_OIDC_REDIRECT_URI` values. Set `VITE_OIDC_SCOPE` and the common provider-specific `VITE_OIDC_AUDIENCE` parameter when the IdP requires them to mint a signed JWT access token for the API. Register `AGENTX_OIDC_CLI_CLIENT_ID` as a public client, enable OAuth 2.0 Device Authorization and refresh-token grants, and allow the provider's documented device callback URI. AgentX validates JWTs through discovery/JWKS and does not introspect opaque access tokens.
2. Patch the public host, OIDC issuer, S3 endpoint/bucket, trusted proxy CIDRs, and CORS origin in `configmap.yaml` and `ingress.yaml`. Replace every `REPLACE_WITH_*` sentinel with owner-approved HTTPS Terms, Privacy, and Support URLs, staffed abuse/security addresses, exact OIDC `issuer|subject` compliance-admin principals, and the approved audit-retention day count; unchanged sentinels intentionally make hosted startup fail.
3. Install an ingress controller, cert-manager, External Secrets Operator, Metrics Server, and Prometheus Operator. Change their class/store/label names where your platform differs.
4. Create `production-secret-store` and the four remote secret values referenced by `external-secret.yaml`. Never replace the `ExternalSecret` with committed plaintext credentials.
5. Provision PostgreSQL with point-in-time recovery and private S3-compatible storage. Restrict both at the network/IAM layer. Configure and capture provider evidence for backup expiration separately; `AGENTX_AUDIT_RETENTION_DAYS` governs live audit rows, not backups or object versions.

The release workflow additionally requires repository variables `PRODUCTION_OIDC_ISSUER`, `PRODUCTION_OIDC_CLIENT_ID`, and `PRODUCTION_WEB_ORIGIN`; optional `PRODUCTION_OIDC_SCOPE` and `PRODUCTION_OIDC_AUDIENCE` values are forwarded to both the packaged hosted Console and its container image. Missing required values fail the tag release. It publishes `agentx-server` and `agentx-web` to GHCR with provenance and SBOM attestations, scans them for high/critical vulnerabilities, and keyless-signs their digests. Verify a deployment digest before rollout:

```bash
cosign verify \
  --certificate-identity-regexp='^https://github.com/.+/.github/workflows/release.yml@refs/tags/v' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  ghcr.io/agentx-xai/agentx-server@sha256:...
```

Render and validate before deployment:

```bash
kubectl kustomize deploy/kubernetes > /tmp/agentx-production.yaml
make production-preflight KUSTOMIZE_DIR=path/to/production-overlay
kubectl apply --dry-run=server -f /tmp/agentx-production.yaml
```

Run `production-preflight` against the final production overlay, not this intentionally incomplete baseline. It rejects unreplaced sentinels, example or loopback endpoints, mutable image references, staging escape hatches, plaintext Kubernetes Secrets, missing hosted settings, and missing ExternalSecret mappings. It supplements API startup validation; it cannot prove that provider-side secrets, DNS, TLS, backup expiry, or legal approvals exist.

Run the migration Job with the new image and wait for completion before updating the Deployments. Goose takes a PostgreSQL advisory lock, so only one migration applies. Delete/recreate the completed Job when its image changes.

```bash
kubectl apply -f deploy/kubernetes/namespace.yaml
kubectl apply -k deploy/kubernetes --prune -l app.kubernetes.io/part-of=agentx
kubectl -n agentx wait --for=condition=complete job/agentx-migrate --timeout=5m
kubectl -n agentx rollout status deployment/agentx-api --timeout=5m
kubectl -n agentx rollout status deployment/agentx-web --timeout=5m
```

The API uses three replicas, a zero-unavailable rolling strategy, zone/host spreading, a disruption budget, resource bounds, and shared PostgreSQL rate limits. The Web tier uses two replicas and a disruption budget. TLS terminates at Ingress; `/v1` remains same-origin through the Web reverse proxy.

Operational targets and restore/rollback procedures are in [`docs/OPERATIONS.md`](../../docs/OPERATIONS.md).
