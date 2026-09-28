#!/bin/zsh
set -euo pipefail
cd "${0:A:h}"
set -a
source ./local.env
set +a
case "${SQL_DSN:-}" in
  postgres://*|postgresql://*) ;;
  *) echo "local.env must configure SQL_DSN for PostgreSQL" >&2; exit 1 ;;
esac
case "${LOG_SQL_DSN:-}" in
  ""|postgres://*|postgresql://*) ;;
  *) echo "LOG_SQL_DSN must use PostgreSQL or remain unset" >&2; exit 1 ;;
esac
unset SQLITE_PATH
exec ./new-api --log-dir "$HOME/.new-api-local/logs"
