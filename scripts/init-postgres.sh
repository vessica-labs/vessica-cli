#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ADMIN_URL="${VES_POSTGRES_ADMIN_URL:-postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable}"
CONTROL_PASSWORD="${VES_CONTROL_DATABASE_PASSWORD:-vessica_control_dev}"
KNOWLEDGE_PASSWORD="${VES_KNOWLEDGE_DATABASE_PASSWORD:-vessica_knowledge_dev}"

psql "$ADMIN_URL" \
  --set=control_password="$CONTROL_PASSWORD" \
  --set=knowledge_password="$KNOWLEDGE_PASSWORD" \
  --file="$ROOT/scripts/init-postgres.sql"
