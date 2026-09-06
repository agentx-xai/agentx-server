#!/usr/bin/env bash
set -euo pipefail
: "${AGENTX_OIDC_URL:=http://localhost:5556/dex}"
before="$(curl --fail --silent "${AGENTX_OIDC_URL}/keys")"
docker compose -f docker-compose.staging.yml up -d --force-recreate dex >/dev/null
for _ in $(seq 1 30); do
  if after="$(curl --silent "${AGENTX_OIDC_URL}/keys" 2>/dev/null)" && [ -n "$after" ]; then break; fi
  sleep 1
done
after="${after:-}"
if [ "$before" = "$after" ]; then echo "OIDC JWKS did not rotate" >&2; exit 1; fi
echo "OIDC JWKS rotated"
