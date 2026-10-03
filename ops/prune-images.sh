#!/usr/bin/env bash
# ops/prune-images.sh —— 安全清理旧的 GHCR 镜像（仅限 $GHCR_PREFIX 前缀，按显式镜像 ID 删除）。
#
# ⛔ 严禁（本脚本绝不使用，任何人不得在此加入）：
#     docker system prune
#     docker image prune
#     docker volume prune
#     docker compose down -v
#     docker rmi $(...)              # 宽泛/命令替换式批量删除
#   本脚本只对候选集里的**显式镜像 ID**逐个执行 `docker image rm`，且候选集**只**来自 $GHCR_PREFIX。
#   不删数据卷；不动 postgres / redis / caddy。
#
# 用法：
#   ops/prune-images.sh [preview|cleanup] [--apply] [--keep N]
#     preview（默认）    只打印表格，不删除
#     cleanup            必须显式带 --apply 才真正删除；否则等同 preview
#     --keep N           保留最新的 N 个 sha-* 镜像（默认 $KEEP_IMAGES，即 3）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

MODE="preview"
APPLY=0
KEEP="$KEEP_IMAGES"

usage() {
  cat <<'USAGE'
用法：prune-images.sh [preview|cleanup] [--apply] [--keep N]

参数：
  preview           只预览，不删除（默认）
  cleanup           清理；必须同时带 --apply 才真正删除
  --apply           确认执行删除（仅对 cleanup 有效）
  --keep N          保留最新的 N 个 sha-* 镜像（默认 3）
  -h, --help        显示本帮助

保留集（任一命中即保留）：
  - 当前 app 容器运行中的镜像 ID
  - 所有容器（含已停止）引用的镜像 ID
  - $GHCR_PREFIX 下按创建时间最新的 N 个 sha-* tag
  - $RELEASES_DIR/rollback-keep.txt 中列出的 ref
USAGE
}

in_list() {
  local needle="$1"; shift
  local x
  for x in "$@"; do
    [ "$x" = "$needle" ] && return 0
  done
  return 1
}

# 收集本仓库（$GHCR_PREFIX）下的本地镜像行："id@@repo:tag@@created@@size"
collect_repo_lines() {
  docker image ls --no-trunc --format '{{.ID}}@@{{.Repository}}:{{.Tag}}@@{{.CreatedAt}}@@{{.Size}}' \
    | awk -F'@@' -v prefix="$GHCR_PREFIX" 'index($2, prefix ":") == 1'
}

print_disk() {
  local label="$1"
  echo "----- $label -----"
  df -h / || true
  echo
  docker system df || true
  echo
}

main() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      preview) MODE="preview"; shift ;;
      cleanup) MODE="cleanup"; shift ;;
      --apply) APPLY=1; shift ;;
      --keep) [ "$#" -ge 2 ] || die "--keep 需要一个数字"; KEEP="$2"; shift 2 ;;
      -*) die "未知参数：${1}（用 --help 查看用法）" ;;
      *) die "未知位置参数：${1}（用 --help 查看用法）" ;;
    esac
  done

  require_cmd docker
  [[ "$KEEP" =~ ^[0-9]+$ ]] || die "--keep 必须是数字，收到：$KEEP"

  echo "GHCR_PREFIX = $GHCR_PREFIX"
  echo "KEEP_IMAGES = $KEEP"
  echo "模式        = $MODE$([ "$MODE" = cleanup ] && { [ "$APPLY" -eq 1 ] && echo ' (--apply)' || echo '（未带 --apply，仅预览）'; })"
  echo

  print_disk "清理前磁盘"

  # ---------- 1. 构建保留集 ----------
  local -a keep_ids=()
  local id

  # 1a. 当前 app 容器运行中的镜像
  if docker inspect "$APP_CONTAINER" >/dev/null 2>&1; then
    id="$(docker inspect --format '{{.Image}}' "$APP_CONTAINER")"
    if [ -n "$id" ]; then
      keep_ids+=("$id")
      log "保留（app 容器运行中）：$id"
    fi
  else
    warn "未找到 app 容器 '$APP_CONTAINER'，跳过该项保留判定。"
  fi

  # 1b. 所有容器（含已停止）引用的镜像
  while IFS= read -r id; do
    [ -n "$id" ] || continue
    keep_ids+=("$id")
  done < <(docker ps -a -q 2>/dev/null | xargs -r docker inspect --format '{{.Image}}' 2>/dev/null || true)
  log "已收集所有容器引用镜像 ID（含已停止）：${#keep_ids[@]} 项"

  # 1c. $GHCR_PREFIX 下最新的 KEEP 个 sha-* tag
  local repo_lines
  repo_lines="$(collect_repo_lines)"
  local sha_keep_ids
  sha_keep_ids="$(printf '%s\n' "$repo_lines" \
    | awk -F'@@' '$2 ~ /:sha-[0-9a-f]+$/ {print $3"@@"$1}' \
    | sort -r \
    | awk -F'@@' '!seen[$2]++ {print $2}' \
    | head -n "$KEEP")"
  while IFS= read -r id; do
    [ -n "$id" ] || continue
    keep_ids+=("$id")
    log "保留（最新 $KEEP 个 sha-* 之一）：$id"
  done <<< "$sha_keep_ids"

  # 1d. rollback-keep.txt 中列出的 ref
  local keep_file="$RELEASES_DIR/rollback-keep.txt"
  if [ -f "$keep_file" ]; then
    local kref kid
    while IFS= read -r kref; do
      [ -n "$kref" ] || continue
      case "$kref" in \#*) continue ;; esac
      kid="$(docker image inspect --format '{{.Id}}' "$kref" 2>/dev/null || true)"
      if [ -n "$kid" ]; then
        keep_ids+=("$kid")
        log "保留（rollback-keep.txt）：$kref → $kid"
      else
        warn "rollback-keep.txt 中的 ref 在本地不存在，忽略：$kref"
      fi
    done < "$keep_file"
  fi

  echo

  # ---------- 2. 构建候选集（仅 $GHCR_PREFIX，且不在保留集内）----------
  if [ -z "$repo_lines" ]; then
    warn "未发现 $GHCR_PREFIX 前缀的本地镜像，无可清理项。"
    print_disk "清理后磁盘"
    exit 0
  fi

  printf '%-16s %-52s %-10s %s\n' "IMAGE_ID" "TAG" "SIZE" "判定"
  printf '%s\n' "--------------------------------------------------------------------------------------------------------"

  local candidate_ids=""
  local line lid ltag lsize
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    lid="$(printf '%s' "$line" | awk -F'@@' '{print $1}')"
    ltag="$(printf '%s' "$line" | awk -F'@@' '{print $2}')"
    lsize="$(printf '%s' "$line" | awk -F'@@' '{print $4}')"

    if in_list "$lid" ${keep_ids[@]+"${keep_ids[@]}"}; then
      printf '%-16s %-52s %-10s %s\n' "${lid#sha256:}" "$ltag" "$lsize" "保留"
    else
      printf '%-16s %-52s %-10s %s\n' "${lid#sha256:}" "$ltag" "$lsize" "候选"
      if ! printf '%s\n' "$candidate_ids" | grep -qxF "$lid"; then
        candidate_ids="${candidate_ids}${lid}"$'\n'
      fi
    fi
  done <<< "$repo_lines"

  echo

  if [ -z "$candidate_ids" ]; then
    ok "没有可清理的候选镜像。"
    print_disk "清理后磁盘"
    exit 0
  fi

  if [ "$MODE" != "cleanup" ] || [ "$APPLY" -ne 1 ]; then
    log "预览模式：以上「候选」镜像未删除。"
    echo "如需真正删除，请执行：  ops/prune-images.sh cleanup --apply --keep $KEEP"
    print_disk "清理后磁盘"
    exit 0
  fi

  # ---------- 3. 执行清理（逐个显式镜像 ID）----------
  warn "开始清理候选镜像（仅限 ${GHCR_PREFIX}，按显式 ID）："
  local failed=0
  while IFS= read -r lid; do
    [ -n "$lid" ] || continue
    if docker image rm "$lid"; then
      ok "已删除 $lid"
    else
      warn "删除失败：$lid"
      failed=1
    fi
  done <<< "$candidate_ids"

  echo
  print_disk "清理后磁盘"

  if [ "$failed" -ne 0 ]; then
    warn "部分镜像删除失败（可能仍被占用）。未使用任何宽泛清理命令。"
    exit 1
  fi
  ok "清理完成。"
}

main "$@"
