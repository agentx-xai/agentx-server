#!/usr/bin/env bash
set -euo pipefail

: "${AGENTX_COMPOSE_PROJECT:=agentx-staging}"
: "${AGENTX_SOURCE_DATABASE:=agentx}"
: "${AGENTX_DATABASE_USER:=agentx}"

compose=(docker compose -p "$AGENTX_COMPOSE_PROJECT" -f docker-compose.staging.yml)
postgres_id="$("${compose[@]}" ps -q postgres)"
if [ -z "$postgres_id" ]; then
  echo "staging PostgreSQL container is not running" >&2
  exit 1
fi

drill_database="agentx_restore_drill_$(date -u +%Y%m%d%H%M%S)_$$"
case "$drill_database" in
  agentx_restore_drill_*) ;;
  *) echo "unsafe drill database name" >&2; exit 1 ;;
esac

cleanup() {
  docker exec "$postgres_id" dropdb --if-exists --force --username "$AGENTX_DATABASE_USER" "$drill_database" >/dev/null
}
trap cleanup EXIT

docker exec "$postgres_id" createdb --username "$AGENTX_DATABASE_USER" "$drill_database"
docker exec "$postgres_id" pg_dump --username "$AGENTX_DATABASE_USER" --format=custom "$AGENTX_SOURCE_DATABASE" \
  | docker exec -i "$postgres_id" pg_restore --username "$AGENTX_DATABASE_USER" --dbname "$drill_database" --exit-on-error

count_query="SELECT table_name || '=' || row_count FROM ("
separator=""
for table in users workspaces memberships workspace_invitations legal_holds packages releases artifacts devices device_packages policies manifests audit_events outbox idempotency_keys rate_limit_windows goose_db_version; do
  count_query+="${separator}SELECT '$table' AS table_name, count(*)::text AS row_count FROM $table"
  separator=" UNION ALL "
done
count_query+=") counts ORDER BY table_name;"

source_counts="$(docker exec "$postgres_id" psql --username "$AGENTX_DATABASE_USER" --dbname "$AGENTX_SOURCE_DATABASE" --tuples-only --no-align --command "$count_query")"
restored_counts="$(docker exec "$postgres_id" psql --username "$AGENTX_DATABASE_USER" --dbname "$drill_database" --tuples-only --no-align --command "$count_query")"
if [ "$source_counts" != "$restored_counts" ]; then
  echo "restored database counts differ from source" >&2
  diff <(printf '%s\n' "$source_counts") <(printf '%s\n' "$restored_counts") || true
  exit 1
fi

schema_version="$(docker exec "$postgres_id" psql --username "$AGENTX_DATABASE_USER" --dbname "$drill_database" --tuples-only --no-align --command 'SELECT max(version_id) FROM goose_db_version WHERE is_applied')"
echo "restore drill passed: database=$drill_database schema_version=$schema_version"
printf '%s\n' "$restored_counts"
