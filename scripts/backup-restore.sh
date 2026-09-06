#!/usr/bin/env bash
set -euo pipefail
: "${AGENTX_DATABASE_URL:?set AGENTX_DATABASE_URL}"
: "${BACKUP_FILE:=./agentx-staging.sql}"
: "${AGENTX_CONTAINER_DATABASE_URL:=postgres://agentx:agentx-staging@localhost:5432/agentx}"
case "${1:-backup}" in
  backup)
    if command -v pg_dump >/dev/null 2>&1; then pg_dump --format=custom --file "$BACKUP_FILE" "$AGENTX_DATABASE_URL"; else docker compose -f docker-compose.staging.yml exec -T postgres pg_dump --format=custom "$AGENTX_CONTAINER_DATABASE_URL" > "$BACKUP_FILE"; fi
    echo "wrote $BACKUP_FILE" ;;
  restore)
    if command -v pg_restore >/dev/null 2>&1; then pg_restore --clean --if-exists --dbname "$AGENTX_DATABASE_URL" "$BACKUP_FILE"; else docker compose -f docker-compose.staging.yml exec -T postgres pg_restore --clean --if-exists --dbname "$AGENTX_CONTAINER_DATABASE_URL" < "$BACKUP_FILE"; fi
    echo "restored $BACKUP_FILE" ;;
  *) echo "usage: $0 backup|restore" >&2; exit 2 ;;
esac
