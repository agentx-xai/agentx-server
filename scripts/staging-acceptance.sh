#!/usr/bin/env bash
set -euo pipefail
: "${AGENTX_API_URL:=http://localhost:8081}"
: "${AGENTX_TOKEN:?set AGENTX_TOKEN to an OIDC bearer token}"
: "${AGENTX_WORKSPACE_ID:?set AGENTX_WORKSPACE_ID to an existing workspace}"
auth=(-H "Authorization: Bearer ${AGENTX_TOKEN}" -H "Content-Type: application/json")
echo "readiness"
curl --fail --silent "${AGENTX_API_URL}/readyz" >/dev/null
echo "manifest round trip"
curl --fail --silent "${auth[@]}" -X PUT "${AGENTX_API_URL}/v1/workspaces/${AGENTX_WORKSPACE_ID}/manifest" -d '{"document":{"packages":[]}}' >/dev/null
curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${AGENTX_WORKSPACE_ID}/manifest" >/dev/null
echo "metrics"
curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/metrics" | grep -q agentx_http_requests_total
echo "device recovery protocol"
device_id="${AGENTX_DEVICE_ID:-00000000-0000-0000-0000-000000000002}"
curl --fail --silent "${auth[@]}" -X POST "${AGENTX_API_URL}/v1/workspaces/${AGENTX_WORKSPACE_ID}/devices/${device_id}/heartbeat" -d '{"agent":"agentx","installed_packages":{}}' >/dev/null
curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${AGENTX_WORKSPACE_ID}/devices/${device_id}/plan" >/dev/null
echo "staging acceptance passed"
