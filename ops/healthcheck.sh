#!/usr/bin/env bash
# ops/healthcheck.sh —— 只读健康巡检。本脚本**不修改任何东西**（不重启、不改文件、不删镜像）。
#
# 断言：
#   - app 容器 State.Health.Status == healthy 且 RestartCount == 0；
#   - $HEALTH_URL_LOCAL 返回 HTTP 200 且 body 含 "success": true；
#   - postgres、redis 容器 healthy；
#   - caddy 容器 running；
#   - public 模式下，若设置了 PUBLIC_HEALTH_URL，再校验一次。
#
# 用法：ops/healthcheck.sh [local|public]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-newapi-prod-postgres-1}"
REDIS_CONTAINER="${REDIS_CONTAINER:-newapi-prod-redis-1}"
CADDY_CONTAINER="${CADDY_CONTAINER:-newapi-prod-caddy-1}"
PUBLIC_HEALTH_URL="${PUBLIC_HEALTH_URL:-}"

MODE="local"

usage() {
  cat <<'USAGE'
用法：healthcheck.sh [local|public]

  local（默认）   只校验本机（容器健康 + $HEALTH_URL_LOCAL）
  public          在 local 基础上，若设置了 PUBLIC_HEALTH_URL，再校验一次对外地址
  -h, --help      显示本帮助

环境变量（可覆盖）：
  APP_CONTAINER / POSTGRES_CONTAINER / REDIS_CONTAINER / CADDY_CONTAINER
  HEALTH_URL_LOCAL / PUBLIC_HEALTH_URL
USAGE
}

FAILS=0
declare -a ROWS=()

record() {
  # record <name> <status:OK|FAIL> <detail>
  local name="$1" status="$2" detail="$3"
  ROWS+=("$(printf '%-26s %-6s %s' "$name" "$status" "$detail")")
  [ "$status" = "OK" ] || FAILS=$((FAILS + 1))
}

check_health_status() {
  # check_health_status <name> <container> [expect_running]
  local name="$1" cname="$2" expect_running="${3:-}"
  local status
  status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$cname" 2>/dev/null || printf 'missing')"

  if [ "$expect_running" = "running-only" ]; then
    local state
    state="$(docker inspect --format '{{.State.Status}}' "$cname" 2>/dev/null || printf 'missing')"
    if [ "$state" = "running" ]; then
      record "$name" "OK" "running"
    else
      record "$name" "FAIL" "state=$state"
    fi
    return
  fi

  if [ "$status" = "healthy" ]; then
    record "$name" "OK" "healthy"
  else
    record "$name" "FAIL" "health=$status"
  fi
}

check_app_specifics() {
  local cname="$APP_CONTAINER"
  if ! docker inspect "$cname" >/dev/null 2>&1; then
    record "app($cname) exists" "FAIL" "container not found"
    return
  fi
  local restarts
  restarts="$(docker inspect --format '{{.RestartCount}}' "$cname" 2>/dev/null || printf 'unknown')"
  if [ "$restarts" = "0" ]; then
    record "app RestartCount" "OK" "0"
  else
    record "app RestartCount" "FAIL" "restarts=$restarts"
  fi
}

check_local_http() {
  require_cmd curl
  local code body
  code="$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL_LOCAL" 2>/dev/null || true)"
  body="$(curl -s "$HEALTH_URL_LOCAL" 2>/dev/null || true)"
  if [ "$code" = "200" ] && printf '%s' "$body" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true'; then
    record "http $HEALTH_URL_LOCAL" "OK" "200 + success:true"
  else
    record "http $HEALTH_URL_LOCAL" "FAIL" "code=${code:-none}"
  fi
}

check_public_http() {
  if [ -z "$PUBLIC_HEALTH_URL" ]; then
    record "PUBLIC_HEALTH_URL" "OK" "未设置，跳过"
    return
  fi
  require_cmd curl
  local code body
  code="$(curl -s -o /dev/null -w '%{http_code}' "$PUBLIC_HEALTH_URL" 2>/dev/null || true)"
  body="$(curl -s "$PUBLIC_HEALTH_URL" 2>/dev/null || true)"
  if [ "$code" = "200" ] && printf '%s' "$body" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true'; then
    record "http $PUBLIC_HEALTH_URL" "OK" "200 + success:true"
  else
    record "http $PUBLIC_HEALTH_URL" "FAIL" "code=${code:-none}"
  fi
}

main() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      local) MODE="local"; shift ;;
      public) MODE="public"; shift ;;
      *) die "未知参数：${1}（用 --help 查看用法）" ;;
    esac
  done

  require_cmd docker

  log "健康巡检（只读），模式：$MODE"

  check_app_specifics
  check_health_status "app($APP_CONTAINER)" "$APP_CONTAINER"
  check_health_status "postgres($POSTGRES_CONTAINER)" "$POSTGRES_CONTAINER"
  check_health_status "redis($REDIS_CONTAINER)" "$REDIS_CONTAINER"
  check_health_status "caddy($CADDY_CONTAINER)" "$CADDY_CONTAINER" "running-only"
  check_local_http
  if [ "$MODE" = "public" ]; then
    check_public_http
  fi

  echo
  printf '%s\n' "----------------------------------------------------------------"
  local row
  for row in ${ROWS[@]+"${ROWS[@]}"}; do
    printf '%s\n' "$row"
  done
  printf '%s\n' "----------------------------------------------------------------"

  if [ "$FAILS" -eq 0 ]; then
    ok "全部检查通过。"
    exit 0
  fi
  die "存在 $FAILS 项失败，请人工排查。"
}

main "$@"
