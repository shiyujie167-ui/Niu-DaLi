#!/usr/bin/env bash
# ops/rollback.sh —— 回滚到指定的旧镜像（服务器只拉取 + 重建 app 容器）。
#
# ⚠️  第一步：本脚本**不回滚数据库**。在执行前必须人工核对旧应用与当前数据库 schema 的兼容性，
#         否则可能出现数据损坏。schema 兼容性判断由发布操作人负责。
#
# 硬边界与 deploy.sh 相同：
#   - 永不触碰 postgres / redis / caddy 容器；
#   - 永不 `docker compose down`；只重建 app 服务（up -d --no-deps）；
#   - 永不修改停止超时参数。
#
# 用法：
#   rollback.sh <prev-sha|image-ref> [--no-backup]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

ARG_REF=""
NO_BACKUP=0

usage() {
  cat <<'USAGE'
用法：rollback.sh <prev-sha|image-ref> [--no-backup]

参数：
  <prev-sha|image-ref>   要回滚到的旧版本：完整40位sha | 7-39位短sha | ghcr.io/<owner>/niu-dali:sha-<sha>
  --no-backup            跳过备份（谨慎使用；回滚默认也先备份）
  -h, --help             显示本帮助

注意：
  - 本脚本不回滚数据库；执行前必须核对旧应用与当前数据库 schema 的兼容性。
USAGE
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --no-backup) NO_BACKUP=1; shift ;;
      -*) die "未知参数：${1}（用 --help 查看用法）" ;;
      *) if [ -n "$ARG_REF" ]; then die "只能指定一个 <prev-sha|image-ref>（收到多个）"; fi; ARG_REF="$1"; shift ;;
    esac
  done
  [ -n "$ARG_REF" ] || { usage; die "缺少 <prev-sha|image-ref> 参数"; }
}

rewrite_app_image() {
  local new_ref="$1"
  local env_file="$COMPOSE_DIR/.env"
  local backup
  local tmp

  # 先声明再赋值：避免 local 的返回码掩蔽命令替换的返回码（SC2155），使 set -e 能在失败时生效。
  backup="${env_file}.bak.$(date -u +%Y%m%dT%H%M%SZ)" \
    || die "生成 .env 备份文件名失败（date 命令异常）。修复：确认系统 date 可用后重试。"

  cp -a "$env_file" "$backup"
  log "已备份 .env → $backup"

  tmp="$(mktemp "${env_file}.tmp.XXXXXX")"
  awk -v ref="$new_ref" '
    BEGIN { done = 0 }
    /^APP_IMAGE=/ { print "APP_IMAGE=" ref; done = 1; next }
    { print }
    END { if (!done) print "APP_IMAGE=" ref }
  ' "$env_file" > "$tmp"
  chmod --reference="$env_file" "$tmp" 2>/dev/null || true
  mv -f "$tmp" "$env_file"
  ok "已原子更新 $env_file 的 APP_IMAGE → $new_ref"
}

read_current_app_image() {
  local env_file="$COMPOSE_DIR/.env"
  awk -F= '/^APP_IMAGE=/{ sub(/^APP_IMAGE=/, ""); print; exit }' "$env_file"
}

wait_healthy() {
  require_cmd curl
  local deadline status=""
  deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
  log "等待容器 $APP_CONTAINER 变为 healthy（上限 ${HEALTH_TIMEOUT_SECONDS}s）..."

  while [ "$(date +%s)" -lt "$deadline" ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$APP_CONTAINER" 2>/dev/null || printf 'missing')"
    case "$status" in
      healthy) break ;;
      *) sleep 3 ;;
    esac
  done
  [ "$status" = "healthy" ] || { warn "容器健康状态：${status}（未达 healthy）"; return 1; }
  ok "容器 $APP_CONTAINER 状态：healthy"

  local body
  body="$(curl -fsS "$HEALTH_URL_LOCAL" 2>/dev/null || true)"
  if printf '%s' "$body" | grep -Eq '"success"[[:space:]]*:[[:space:]]*true'; then
    ok "健康接口 $HEALTH_URL_LOCAL 返回 \"success\": true"
  else
    warn "健康接口 $HEALTH_URL_LOCAL 未断言到 \"success\": true。响应：$(printf '%s' "$body" | head -c 200)"
    return 1
  fi
}

run_rollback() {
  local sha="$1"
  local ref="$2"

  # 取锁之后、备份之前完成全部环境与目标校验（含 compose project/服务/容器）。
  ensure_env_ready

  local backup_id="none"
  if [ "$NO_BACKUP" -eq 1 ]; then
    warn "已跳过备份（--no-backup）。"
  else
    [ -x "$BACKUP_SCRIPT" ] || [ -f "$BACKUP_SCRIPT" ] || die "备份脚本不存在：$BACKUP_SCRIPT
修复：回滚前默认先备份。请确认 ops/backup.sh 存在并可执行，或用 --no-backup（不推荐）。"
    log "执行备份：$BACKUP_SCRIPT"
    local backup_out
    backup_out="$("$BACKUP_SCRIPT")"
    printf '%s\n' "$backup_out"
    backup_id="$(printf '%s\n' "$backup_out" | awk 'NF{last=$0} END{print last}')"
    [ -n "$backup_id" ] || backup_id="unknown"
    ok "备份完成，标识：$backup_id"
  fi

  local prev_ref prev_short
  prev_ref="$(read_current_app_image)"
  prev_short=""
  if [ -n "$prev_ref" ]; then
    # 内层展开加引号（SC2295）：GHCR_PREFIX 含 . 与 /，避免在 pattern 位置被当作 glob 元字符。
    prev_short="${prev_ref#"${GHCR_PREFIX}":}"
  fi
  log "当前 APP_IMAGE：${prev_ref:-<空>}"
  log "回滚目标：$ref"

  docker pull --platform linux/amd64 "$ref"
  local digest
  digest="$(image_digest "$ref")"
  log "digest：${digest:-<未获取到>}"

  rewrite_app_image "$ref"

  log "重建 app 服务：compose up -d --no-deps $APP_SERVICE"
  compose up -d --no-deps "$APP_SERVICE"

  if ! wait_healthy; then
    warn "回滚后健康检查未通过。按生产规则**不做自动回滚**，请人工处置。"
    cat >&2 <<EOF
------------------------------------------------------------------------
回滚失败，请人工处置。当前 APP_IMAGE 已写入：$ref
上一个 ref：${prev_ref:-<空>}
备份标识：$backup_id
如需切回上一版：
  /srv/new-api/ops/deploy.sh ${prev_short}
------------------------------------------------------------------------
EOF
    return 1
  fi

  mkdir -p "$RELEASES_DIR"
  printf '%s sha=%s ref=%s digest=%s backup=%s prev=%s\n' \
    "$(now_utc)" "$sha" "$ref" "${digest:-unknown}" "$backup_id" "${prev_ref:-}" \
    >> "$RELEASES_DIR/rollbacks.log"

  ok "回滚成功。"
  printf '  回滚前：%s\n' "${prev_ref:-<空>}"
  printf '  回滚后：%s\n' "$ref"
  printf '  备份  ：%s\n' "$backup_id"
}

main() {
  parse_args "$@"
  require_cmd docker
  require_cmd git

  warn "回滚前请务必核对：旧应用（${ARG_REF}）与当前数据库 schema 的兼容性。本脚本**不回滚数据库**。"

  local ref sha
  ref="$(resolve_image_ref "$ARG_REF")"
  sha="$(printf '%s' "$ref" | awk -F':sha-' 'NF>1{print $2; exit}')"
  [ -n "$sha" ] || sha="unknown"

  with_publish_lock run_rollback "$sha" "$ref"
}

main "$@"
