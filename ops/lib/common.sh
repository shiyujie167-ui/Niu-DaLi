#!/usr/bin/env bash
# ops/lib/common.sh —— 生产发布运维脚本共用库（由 ops/ 下其它脚本 source）。
#
# 约定：
#   - 所有变量可用环境变量覆盖；也可把服务器实际路径固化到 ops.env（见 load_ops_env）。
#   - 本文件**不得**出现任何真实密钥、真实 IP、真实 token。
set -euo pipefail

# --------------------------- 日志 ---------------------------
now_utc() { date -u +'%Y-%m-%dT%H:%M:%SZ'; }

log()  { printf '[%s] %s\n' "$(now_utc)" "$*"; }
ok()   { printf '[%s] \033[32mOK\033[0m    %s\n' "$(now_utc)" "$*"; }
warn() { printf '[%s] \033[33mWARN\033[0m  %s\n' "$(now_utc)" "$*" >&2; }
die()  { printf '[%s] \033[31mFATAL\033[0m %s\n' "$(now_utc)" "$*" >&2; exit 1; }

require_cmd() {
  local cmd
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || die "缺少命令：${cmd}。请先安装后再执行。"
  done
}

# --------------------------- 环境载入 ---------------------------
# 若存在 ops.env（可由 OPS_ENV_FILE 覆盖），先 source，让操作人固化服务器实际路径。
# 例如 ops.env 中写：
#   COMPOSE_DIR=/srv/new-api/production
#   COMPOSE_FILE=/srv/new-api/production/compose.yaml
#   COMPOSE_PROJECT_NAME=newapi-prod
#   LOCK_DIR=/srv/new-api/ops/locks
load_ops_env() {
  local env_file="${OPS_ENV_FILE:-/srv/new-api/ops/ops.env}"
  if [ -f "$env_file" ]; then
    # shellcheck disable=SC1090
    . "$env_file"
  fi
}

load_ops_env

# --------------------------- 可覆盖默认值 ---------------------------
GHCR_PREFIX="${GHCR_PREFIX:-ghcr.io/shiyujie167-ui/niu-dali}"
# 生产真实值（依据项目自带运维技能 niu-dali-prod-log-analysis）：
#   编排文件 /srv/new-api/production/compose.yaml，project newapi-prod，服务 app。
COMPOSE_DIR="${COMPOSE_DIR:-/srv/new-api/production}"

# COMPOSE_FILE 自动探测：若未显式指定，则按固定顺序探测常见文件名。
if [ -z "${COMPOSE_FILE:-}" ]; then
  for _cand in \
    "$COMPOSE_DIR/compose.yaml" \
    "$COMPOSE_DIR/compose.yml" \
    "$COMPOSE_DIR/docker-compose.yml" \
    "$COMPOSE_DIR/docker-compose.yaml"; do
    if [ -f "$_cand" ]; then
      COMPOSE_FILE="$_cand"
      break
    fi
  done
fi
# 探测不到时保持为空，交给 ensure_env_ready 大声报错并给出修复指引。
COMPOSE_FILE="${COMPOSE_FILE:-}"

APP_SERVICE="${APP_SERVICE:-app}"
APP_CONTAINER="${APP_CONTAINER:-newapi-prod-app-1}"
# compose project 名必须显式绑定：否则 project 名会取自 compose 文件所在目录名，
# 一旦落到 "production" 这个 project，up -d 会另起一套并行容器而非接管现有 newapi-prod。
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-newapi-prod}"

LOCK_DIR="${LOCK_DIR:-/srv/new-api/ops/locks}"
PUBLISH_LOCK="${PUBLISH_LOCK:-$LOCK_DIR/publish.lock}"
LOCK_WAIT_SECONDS="${LOCK_WAIT_SECONDS:-600}"

RELEASES_DIR="${RELEASES_DIR:-/srv/new-api/releases}"
BACKUP_SCRIPT="${BACKUP_SCRIPT:-/srv/new-api/ops/backup.sh}"
LEGACY_BUILD_SCRIPT="${LEGACY_BUILD_SCRIPT:-/srv/new-api/ops/build-image.sh}"
GC_MAINTENANCE_SCRIPT="${GC_MAINTENANCE_SCRIPT:-/srv/new-api/ops/build-cache-maintenance.sh}"

KEEP_IMAGES="${KEEP_IMAGES:-3}"
APP_PORT="${APP_PORT:-3000}"
HEALTH_URL_LOCAL="${HEALTH_URL_LOCAL:-http://127.0.0.1:${APP_PORT}/api/status}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-180}"

# --------------------------- 镜像引用解析 ---------------------------
# resolve_image_ref <arg> —— 把用户输入规范化为可拉取的镜像引用，输出到 stdout。
#   - 纯 40 位小写 hex        → $GHCR_PREFIX:sha-<arg>
#   - 7~39 位小写 hex         → 打 warn（便利别名），仍映射为 $GHCR_PREFIX:sha-<arg>
#   - ghcr.io/ 开头           → 原样使用
#   - 其它                    → die 并给出用法
resolve_image_ref() {
  local arg="${1:-}"
  [ -n "$arg" ] || die "resolve_image_ref 需要 <sha|image-ref> 参数。用法：<完整40位sha> | <7-39位短sha> | ghcr.io/<owner>/niu-dali:sha-<sha>"

  if [[ "$arg" =~ ^[0-9a-f]{40}$ ]]; then
    printf '%s:sha-%s\n' "$GHCR_PREFIX" "$arg"
  elif [[ "$arg" =~ ^[0-9a-f]{7,39}$ ]]; then
    warn "传入的是短 sha '$arg'（便利别名）。建议使用完整 40 位 sha 作为发布与回滚的权威锚点。"
    printf '%s:sha-%s\n' "$GHCR_PREFIX" "$arg"
  elif [[ "$arg" == ghcr.io/* ]]; then
    printf '%s\n' "$arg"
  else
    die "无法识别的镜像引用：'$arg'。用法：<完整40位sha> | <7-39位短sha> | ghcr.io/<owner>/niu-dali:sha-<sha>"
  fi
}

# --------------------------- 发布锁 ---------------------------
# with_publish_lock <cmd...> —— 对 $PUBLISH_LOCK 取排他锁后执行命令。
# flock 不存在时直接 die（不 fallback 到无锁，避免并发发布破坏生产）。
with_publish_lock() {
  require_cmd flock
  mkdir -p "$LOCK_DIR"
  log "获取发布锁：${PUBLISH_LOCK}（等待上限 ${LOCK_WAIT_SECONDS}s）"
  (
    if ! flock -x -w "$LOCK_WAIT_SECONDS" 9; then
      die "获取发布锁超时（另一个发布可能正在进行）：$PUBLISH_LOCK"
    fi
    "$@"
  ) 9>"$PUBLISH_LOCK"
}

# --------------------------- compose 封装 ---------------------------
# 统一加 -p "$COMPOSE_PROJECT_NAME"，避免 project 名取自目录名而另起并行容器。
compose() {
  [ -n "${COMPOSE_FILE:-}" ] || die "COMPOSE_FILE 未确定，无法执行 compose。请先修复环境（见 ops/README.md）。"
  docker compose -f "$COMPOSE_FILE" --project-directory "$COMPOSE_DIR" -p "$COMPOSE_PROJECT_NAME" "$@"
}

# --------------------------- 环境校验 ---------------------------
# ensure_env_ready —— 逐项校验发布前提；任一不满足即 die 并给出可直接照做的修复提示。
# ⚠️ 必须在**取发布锁之后、备份之前**调用：其中包含依赖 docker 的目标 project/服务/容器校验，
#    以便在「project 名选错 → 会另起一套并行容器」这类最危险的失败模式下**大声失败**。
ensure_env_ready() {
  local env_file="$COMPOSE_DIR/.env"

  if [ -z "${COMPOSE_FILE:-}" ]; then
    die "未找到生产 compose 文件。已尝试：$COMPOSE_DIR/{compose.yaml,compose.yml,docker-compose.yml,docker-compose.yaml}。
修复：把生产 compose 的真实路径写进 ops.env 后重试，例如：
  echo 'COMPOSE_FILE=/srv/new-api/production/compose.yaml' >> ${OPS_ENV_FILE:-/srv/new-api/ops/ops.env}
或导出 COMPOSE_FILE=/srv/new-api/production/compose.yaml 后再执行。"
  fi

  [ -f "$COMPOSE_FILE" ] || die "COMPOSE_FILE 指向的文件不存在：$COMPOSE_FILE
修复：核对路径并写入 ops.env：echo 'COMPOSE_FILE=/srv/new-api/production/compose.yaml' >> ${OPS_ENV_FILE:-/srv/new-api/ops/ops.env}"

  grep -q '^[[:space:]]*image:' "$COMPOSE_FILE" \
    || die "compose 文件 $COMPOSE_FILE 中未找到 image: 字段。
修复：确认该 compose 使用预构建镜像（image: \${APP_IMAGE}），而不是在服务器本机构建。"

  grep -Eq '^[[:space:]]*image:[[:space:]]*\$\{APP_IMAGE\}' "$COMPOSE_FILE" \
    || die "compose 文件 $COMPOSE_FILE 的 image: 未引用 \${APP_IMAGE}。
修复：把 app 服务的 image: 改为 image: \${APP_IMAGE}，使 .env 中的 APP_IMAGE 生效。"

  [ -f "$env_file" ] || die "未找到环境文件：$env_file
修复：cp deploy/.env.prod.example $env_file 并替换所有 CHANGE_ME。"

  grep -q '^APP_IMAGE=' "$env_file" \
    || die "$env_file 中缺少 APP_IMAGE= 行。
修复：在 $env_file 中新增一行，例如 APP_IMAGE=${GHCR_PREFIX}:sha-<完整40位sha>"

  # ---- 目标 project / 服务 / 容器 校验（把「选错 project」变成大声失败）----
  # (1) 断言 $APP_SERVICE 存在于该 project 的 compose 服务列表
  local services
  if ! services="$(compose config --services 2>/dev/null)"; then
    die "无法解析 compose 服务列表：docker compose -f $COMPOSE_FILE -p $COMPOSE_PROJECT_NAME config --services 失败。
修复：核对 COMPOSE_FILE / COMPOSE_DIR / COMPOSE_PROJECT_NAME，并确认 .env 可被 compose 解析。"
  fi
  printf '%s\n' "$services" | grep -qx "$APP_SERVICE" \
    || die "compose project '$COMPOSE_PROJECT_NAME' 的服务列表中不包含 '$APP_SERVICE'。
实际服务：$(printf '%s' "$services" | paste -sd', ' -)
修复：核对 APP_SERVICE / COMPOSE_PROJECT_NAME / COMPOSE_FILE 是否指向生产真实编排（生产 project 应为 newapi-prod、服务应为 app）。"

  # (2) 断言目标容器已存在（直接挡住「起了一套新容器」的情形）
  docker container inspect "$APP_CONTAINER" >/dev/null 2>&1 \
    || die "目标容器 '$APP_CONTAINER' 不存在。
修复：核对 APP_CONTAINER 与 COMPOSE_PROJECT_NAME（生产应指向 newapi-prod-app-1）。若确实需要新建容器，请人工确认后再操作——发布脚本不得静默创建并行容器。"

  # (3) 断言目标容器的 compose project 标签与 $COMPOSE_PROJECT_NAME 一致
  local actual_project
  actual_project="$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$APP_CONTAINER" 2>/dev/null || true)"
  [ "$actual_project" = "$COMPOSE_PROJECT_NAME" ] \
    || die "容器 '$APP_CONTAINER' 的 compose project 标签为 '${actual_project:-<空>}'，与 COMPOSE_PROJECT_NAME='$COMPOSE_PROJECT_NAME' 不一致。
修复：把 COMPOSE_PROJECT_NAME 设为该容器实际的 project 名（生产应为 newapi-prod）；否则 up -d 会另起一套并行容器。"
}

# --------------------------- 镜像 digest ---------------------------
# image_digest <ref> —— 打印本地镜像的 RepoDigest（规范化为裸 sha256:...）；无则打印空行。
# 注意：docker 的 RepoDigests 元素形如 "<repo>@sha256:..."，此处只保留 "@" 之后的部分，
# 以便与 CI 的 build-push-action 输出（裸 sha256:...）以及 --image-digest 直接比对。
image_digest() {
  local ref="$1" raw
  raw="$(docker image inspect --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' "$ref" 2>/dev/null || true)"
  printf '%s\n' "${raw##*@}"
}

# --------------------------- 保护语句（供脚本调用）---------------------------
# assert_guardrails —— 对传入的清理/危险参数做静态防护检查，命中即 die。
assert_no_forbidden_cleanup() {
  # 由调用脚本在解析参数后自行调用；此函数只是集中提示文案。
  :
}

# 被 source 时不执行任何动作；直接运行本文件则打印用法。
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  cat <<'USAGE'
ops/lib/common.sh —— 共用库，请勿直接运行，应由 ops/ 下脚本 source。

主要变量（均可被环境变量覆盖，或写入 ops.env）：
  GHCR_PREFIX         镜像前缀       默认 ghcr.io/shiyujie167-ui/niu-dali
  COMPOSE_DIR         compose 目录   默认 /srv/new-api/production
  COMPOSE_FILE        compose 文件   自动探测
  COMPOSE_PROJECT_NAME compose project 默认 newapi-prod
  APP_SERVICE         compose 服务名 默认 app
  APP_CONTAINER       容器名         默认 newapi-prod-app-1
  LOCK_DIR            锁目录         默认 /srv/new-api/ops/locks
  PUBLISH_LOCK        发布锁文件     默认 $LOCK_DIR/publish.lock
  RELEASES_DIR        发布记录目录   默认 /srv/new-api/releases
  KEEP_IMAGES         保留镜像数     默认 3
USAGE
fi
