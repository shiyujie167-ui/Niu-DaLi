#!/usr/bin/env bash
# ops/pull-image.sh —— 从 GHCR 拉取指定镜像（默认 linux/amd64），可选记录发布台账。
#
# 用法：
#   ops/pull-image.sh <sha|image-ref> [--record]
#     <完整40位sha> | <7-39位短sha> | ghcr.io/<owner>/niu-dali:sha-<sha>
#     --record   把 "<UTC 时间> <ref> <digest>" 追加到 $RELEASES_DIR/pulled.log
#
# GHCR 为 public：本脚本不需要任何 GitHub 凭据，也不需要 docker login。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
用法：pull-image.sh <sha|image-ref> [--record]

参数：
  <sha|image-ref>   完整40位sha | 7-39位短sha | ghcr.io/<owner>/niu-dali:sha-<sha>
  --record          记录到 $RELEASES_DIR/pulled.log
  -h, --help        显示本帮助
USAGE
}

main() {
  local record=0
  local arg=""

  while [ "$#" -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --record) record=1; shift ;;
      -*) die "未知参数：${1}（用 --help 查看用法）" ;;
      *) if [ -n "$arg" ]; then die "只能指定一个 <sha|image-ref>（收到多个）"; fi; arg="$1"; shift ;;
    esac
  done

  [ -n "$arg" ] || { usage; die "缺少 <sha|image-ref> 参数"; }

  require_cmd docker
  local ref
  ref="$(resolve_image_ref "$arg")"
  log "解析镜像引用：$arg → $ref"

  docker pull --platform linux/amd64 "$ref"

  local digest
  digest="$(image_digest "$ref")"
  if [ -n "$digest" ]; then
    ok "已拉取：$ref"
    printf 'digest: %s\n' "$digest"
  else
    warn "已拉取 ${ref}，但未能读取 RepoDigest（可能未进行 registry 校验）。"
  fi

  if [ "$record" -eq 1 ]; then
    mkdir -p "$RELEASES_DIR"
    printf '%s %s %s\n' "$(now_utc)" "$ref" "${digest:-unknown}" >> "$RELEASES_DIR/pulled.log"
    ok "已记录到 $RELEASES_DIR/pulled.log"
  fi
}

main "$@"
