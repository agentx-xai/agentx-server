#!/usr/bin/env bash
set -euo pipefail

: "${AGENTX_API_URL:=http://localhost:8081}"
: "${AGENTX_WEB_URL:=http://localhost:5178}"
: "${AGENTX_MAILPIT_API_URL:=http://localhost:18025}"
: "${AGENTX_COMPOSE_PROJECT:=agentx-staging}"
: "${AGENTX_TOKEN:?set AGENTX_TOKEN to an OIDC or staging bootstrap bearer token}"
: "${AGENTX_CLI_BIN:=$(pwd)/../agentx-cli/target/debug/agentx}"

auth=(-H "Authorization: Bearer ${AGENTX_TOKEN}")
run_id="$(date +%s)-$$"
artifact_dir="$(mktemp -d)"
cli_home="$(mktemp -d)"
trap 'rm -rf "${artifact_dir}" "${cli_home}"' EXIT
test -x "${AGENTX_CLI_BIN}"

echo "readiness and authentication"
curl --fail --silent "${AGENTX_API_URL}/readyz" >/dev/null
unauthorized_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${AGENTX_API_URL}/v1/workspaces")"
test "${unauthorized_status}" = "401"

echo "Web proxy survives API container replacement"
compose=(docker compose -p "${AGENTX_COMPOSE_PROJECT}" -f docker-compose.staging.yml)
web_container_before="$("${compose[@]}" ps -q web)"
api_container_before="$("${compose[@]}" ps -q api)"
test -n "${web_container_before}"
test -n "${api_container_before}"
"${compose[@]}" up -d --force-recreate --no-deps api >/dev/null
for _ in $(seq 1 30); do
  if curl --fail --silent "${AGENTX_API_URL}/readyz" >/dev/null; then
    break
  fi
  sleep 1
done
curl --fail --silent "${AGENTX_API_URL}/readyz" >/dev/null
api_container_after="$("${compose[@]}" ps -q api)"
test -n "${api_container_after}"
test "${api_container_after}" != "${api_container_before}"
test "$("${compose[@]}" ps -q web)" = "${web_container_before}"
curl --fail --silent "${AGENTX_WEB_URL}/v1/site/config" | jq -e '.terms_url | startswith("http")' >/dev/null

workspace_id="${AGENTX_WORKSPACE_ID:-}"
if [ -z "${workspace_id}" ]; then
  workspace_id="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" \
    -X POST "${AGENTX_API_URL}/v1/workspaces" \
    -d "{\"name\":\"Staging acceptance ${run_id}\",\"slug\":\"staging-${run_id}\"}" | jq -r '.id')"
fi
test -n "${workspace_id}"

echo "artifact publish and S3 download"
package="acceptance-${run_id}"
artifact="agentx-staging-artifact-${run_id}"
printf '# %s\n' "${artifact}" >"${artifact_dir}/SKILL.md"
tar -czf "${artifact_dir}/package.tar.gz" -C "${artifact_dir}" SKILL.md
release="$(curl --fail --silent "${auth[@]}" \
  -H "Idempotency-Key: ${run_id}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/packages/${package}/releases" \
  -F 'version=1.0.0' -F "artifact=@${artifact_dir}/package.tar.gz;filename=skill.tar.gz;type=application/gzip")"
digest="$(jq -r '.sha256' <<<"${release}")"
replay_digest="$(curl --fail --silent "${auth[@]}" \
  -H "Idempotency-Key: ${run_id}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/packages/${package}/releases" \
  -F 'version=1.0.0' -F "artifact=@${artifact_dir}/package.tar.gz;filename=skill.tar.gz;type=application/gzip" | jq -r '.sha256')"
test "${replay_digest}" = "${digest}"
printf '# Different %s\n' "${artifact}" >"${artifact_dir}/SKILL.md"
tar -czf "${artifact_dir}/different.tar.gz" -C "${artifact_dir}" SKILL.md
mismatch_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${auth[@]}" \
  -H "Idempotency-Key: ${run_id}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/packages/${package}/releases" \
  -F 'version=1.0.0' -F "artifact=@${artifact_dir}/different.tar.gz;filename=skill.tar.gz;type=application/gzip")"
test "${mismatch_status}" = "400"
downloaded_digest="$(curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/packages/${package}/1.0.0/download" | shasum -a 256 | awk '{print $1}')"
test "${downloaded_digest}" = "${digest}"

echo "manifest, device, and drift round trip"
curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" \
  -X PUT "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/manifest" \
  -d "{\"document\":{\"version\":1,\"packages\":[{\"name\":\"${package}\",\"version\":\"1.0.0\",\"sha256\":\"${digest}\"}]}}" >/dev/null
device_key="device-${run_id}"
device_body="{\"name\":\"staging-${run_id}\",\"agent\":\"agentx\",\"installed_packages\":{\"${package}\":\"${digest}\"}}"
device_id="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: ${device_key}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/devices" \
  -d "${device_body}" | jq -r '.id')"
replayed_device_id="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: ${device_key}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/devices" -d "${device_body}" | jq -r '.id')"
test -n "${device_id}"
test "${replayed_device_id}" = "${device_id}"
device_mismatch_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${auth[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: ${device_key}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/devices" -d "{\"name\":\"different-${run_id}\"}")"
test "${device_mismatch_status}" = "400"
drift_count="$(curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/drift" | jq -r '.count')"
test "${drift_count}" = "0"

echo "CLI token login, sync, rollback, and convergence"
cli_device="cli-${run_id}"
curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: cli-${run_id}" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/devices" \
  -d "{\"id\":\"${cli_device}\",\"name\":\"CLI ${run_id}\",\"agent\":\"agentx\",\"installed_packages\":{}}" >/dev/null
printf '%s\n' "${AGENTX_TOKEN}" | (cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" registry login "${AGENTX_API_URL}" --token-stdin --workspace "${workspace_id}")
credentials="$(find "${cli_home}" -type f -name credentials.json -print -quit)"
test -n "${credentials}"
credential_mode="$(stat -f '%Lp' "${credentials}" 2>/dev/null || stat -c '%a' "${credentials}")"
test "${credential_mode}" = "600"
plan="$(cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" agent plan --device "${cli_device}")"
grep -q "install ${package} ${digest}" <<<"${plan}"
(cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" agent sync --device "${cli_device}" --target codex)
skill_file="${cli_home}/.codex/skills/${package}/SKILL.md"
grep -q "${artifact}" "${skill_file}"
(cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" agent rollback --device "${cli_device}" --target codex)
test ! -e "${skill_file}"
drift_after_rollback="$(curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/drift")"
jq -e --arg device "${cli_device}" --arg package "${package}" \
  '.items | any(.device_id == $device and .package == $package and .kind == "missing")' \
  <<<"${drift_after_rollback}" >/dev/null
(cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" agent sync --device "${cli_device}" --target codex)
test -f "${skill_file}"
final_drift_count="$(curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/drift" | jq -r '.count')"
test "${final_drift_count}" = "0"
(cd "${cli_home}" && HOME="${cli_home}" "${AGENTX_CLI_BIN}" registry logout)
test ! -e "${credentials}"

echo "metrics"
metrics="$(curl --fail --silent "${AGENTX_API_URL}/metrics")"
grep -q agentx_http_requests_total <<<"${metrics}"
grep -q agentx_worker_dead_letters_total <<<"${metrics}"

echo "invitation email delivery and revocation"
invitation_email="acceptance-${run_id}@example.invalid"
invitation_id="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" \
  -X POST "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/invitations" \
  -d "{\"email\":\"${invitation_email}\",\"role\":\"viewer\",\"expires_in_seconds\":3600}" | jq -r '.id')"
test -n "${invitation_id}"
for _ in $(seq 1 30); do
  invitation_message="$(curl --fail --silent "${AGENTX_MAILPIT_API_URL}/api/v1/messages" | jq -c --arg email "${invitation_email}" '.messages | map(select(any(.To[]?; .Address == $email))) | first // empty')"
  if [ -n "${invitation_message}" ]; then
    break
  fi
  sleep 1
done
test -n "${invitation_message:-}"
jq -e '.Subject == "AgentX workspace invitation"' <<<"${invitation_message}" >/dev/null
message_id="$(jq -r '.ID' <<<"${invitation_message}")"
test -n "${message_id}"
curl --fail --silent "${AGENTX_MAILPIT_API_URL}/api/v1/message/${message_id}" | jq -e '.Text | contains("http://localhost:5178")' >/dev/null
curl --fail --silent "${auth[@]}" -X DELETE "${AGENTX_API_URL}/v1/workspaces/${workspace_id}/invitations/${invitation_id}" >/dev/null

echo "shared S3 artifact retention and last-reference garbage collection"
printf '# GC %s\n' "${run_id}" >"${artifact_dir}/SKILL.md"
tar -czf "${artifact_dir}/gc.tar.gz" -C "${artifact_dir}" SKILL.md
gc_workspace_a="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" -X POST "${AGENTX_API_URL}/v1/workspaces" -d "{\"name\":\"GC A ${run_id}\",\"slug\":\"gc-a-${run_id}\"}" | jq -r '.id')"
gc_workspace_b="$(curl --fail --silent "${auth[@]}" -H "Content-Type: application/json" -X POST "${AGENTX_API_URL}/v1/workspaces" -d "{\"name\":\"GC B ${run_id}\",\"slug\":\"gc-b-${run_id}\"}" | jq -r '.id')"
gc_digest="$(curl --fail --silent "${auth[@]}" -H "Idempotency-Key: gc-a-${run_id}" -X POST "${AGENTX_API_URL}/v1/workspaces/${gc_workspace_a}/packages/gc/releases" -F 'version=1.0.0' -F "artifact=@${artifact_dir}/gc.tar.gz;filename=skill.tar.gz;type=application/gzip" | jq -r '.sha256')"
gc_digest_b="$(curl --fail --silent "${auth[@]}" -H "Idempotency-Key: gc-b-${run_id}" -X POST "${AGENTX_API_URL}/v1/workspaces/${gc_workspace_b}/packages/gc-copy/releases" -F 'version=1.0.0' -F "artifact=@${artifact_dir}/gc.tar.gz;filename=skill.tar.gz;type=application/gzip" | jq -r '.sha256')"
test "${gc_digest}" = "${gc_digest_b}"
curl --fail --silent "${auth[@]}" -X DELETE "${AGENTX_API_URL}/v1/workspaces/${gc_workspace_a}" >/dev/null
sleep 2
retained_digest="$(curl --fail --silent "${auth[@]}" "${AGENTX_API_URL}/v1/workspaces/${gc_workspace_b}/packages/gc-copy/1.0.0/download" | shasum -a 256 | awk '{print $1}')"
test "${retained_digest}" = "${gc_digest}"
curl --fail --silent "${auth[@]}" -X DELETE "${AGENTX_API_URL}/v1/workspaces/${gc_workspace_b}" >/dev/null
minio_id="$("${compose[@]}" ps -q minio)"
test -n "${minio_id}"
docker exec "${minio_id}" mc alias set acceptance http://localhost:9000 agentx agentx-staging-secret >/dev/null
for _ in $(seq 1 30); do
  if ! docker exec "${minio_id}" mc stat "acceptance/agentx-artifacts/${gc_digest}" >/dev/null 2>&1; then
    gc_removed=true
    break
  fi
  sleep 1
done
test "${gc_removed:-false}" = "true"
echo "staging acceptance passed for workspace ${workspace_id}"
