# 生产运维脚本（ops/）

本目录是「GitHub Actions 构建镜像 → 推 GHCR → 生产服务器只拉取」方案的服务器侧脚本。
镜像由 CI 构建并推送到 GHCR（**public**），生产服务器**零 GitHub 凭据**（无需 `docker login`），
只做 `docker pull` + 重建 `app` 容器（compose project `newapi-prod`）。

> 所有脚本：`#!/usr/bin/env bash` + `set -euo pipefail`；均支持 `--help`；
> 环境不符时**大声失败并给出可执行的修复指引**，绝不静默继续。

---

## 1. 脚本签名清单

| 脚本 | 签名 | 作用 |
| --- | --- | --- |
| `lib/common.sh` | 被 source | 共用库：日志、环境载入、镜像引用解析、发布锁、compose 封装、环境校验 |
| `pull-image.sh` | `pull-image.sh <sha\|image-ref> [--record]` | 从 GHCR 拉取镜像（linux/amd64），可选记录到 `pulled.log` |
| `deploy.sh` | `deploy.sh <sha\|image-ref> [--image-digest <sha256:..>] [--build-local] [--no-backup] [--skip-health]` | **发布核心**：备份 → 拉取 → 原子改 `.env` → 重建 app → 健康检查 → 记录 |
| `rollback.sh` | `rollback.sh <prev-sha\|image-ref> [--no-backup]` | 回滚到旧镜像（**不回滚数据库**） |
| `prune-images.sh` | `prune-images.sh [preview\|cleanup] [--apply] [--keep N]` | 安全清理旧镜像（**仅** `$GHCR_PREFIX`，按显式镜像 ID） |
| `healthcheck.sh` | `healthcheck.sh [local\|public]` | **只读**健康巡检 |

---

## 2. 环境变量表

所有变量都可用环境变量覆盖，或固化到 `ops.env`（`OPS_ENV_FILE`，默认 `/srv/new-api/ops/ops.env`）。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `OPS_ENV_FILE` | `/srv/new-api/ops/ops.env` | 操作人固化服务器实际路径的配置文件；存在则被 source |
| `GHCR_PREFIX` | `ghcr.io/shiyujie167-ui/niu-dali` | 镜像前缀（GHCR public） |
| `COMPOSE_DIR` | `/srv/new-api/production` | compose 项目目录（含 `.env`） |
| `COMPOSE_FILE` | 自动探测 | 探测顺序见下（生产为 `/srv/new-api/production/compose.yaml`） |
| `COMPOSE_PROJECT_NAME` | `newapi-prod` | compose project 名（**必须显式绑定**，否则会另起并行容器） |
| `APP_SERVICE` | `app` | compose 服务名 |
| `APP_CONTAINER` | `newapi-prod-app-1` | 容器名 |
| `LOCK_DIR` | `/srv/new-api/ops/locks` | 锁目录 |
| `PUBLISH_LOCK` | `$LOCK_DIR/publish.lock` | 发布锁文件 |
| `LOCK_WAIT_SECONDS` | `600` | 获取发布锁的最长等待秒数 |
| `RELEASES_DIR` | `/srv/new-api/releases` | 发布台账目录 |
| `BACKUP_SCRIPT` | `/srv/new-api/ops/backup.sh` | 备份脚本（发布/回滚前置） |
| `LEGACY_BUILD_SCRIPT` | `/srv/new-api/ops/build-image.sh` | 遗留本机构建入口（仅 `--build-local` 用） |
| `GC_MAINTENANCE_SCRIPT` | `/srv/new-api/ops/build-cache-maintenance.sh` | 只做 `docker buildx prune` 的维护脚本 |
| `KEEP_IMAGES` | `3` | `prune-images.sh` 保留最新的 N 个 `sha-*` |
| `APP_PORT` | `3000` | 应用端口 |
| `HEALTH_URL_LOCAL` | `http://127.0.0.1:3000/api/status` | 本机健康地址 |
| `HEALTH_TIMEOUT_SECONDS` | `180` | 健康等待上限 |
| `PUBLIC_HEALTH_URL` | 空 | `healthcheck.sh public` 时的对外地址（未设则跳过） |
| `POSTGRES_CONTAINER` / `REDIS_CONTAINER` / `CADDY_CONTAINER` | `newapi-prod-postgres-1` / `newapi-prod-redis-1` / `newapi-prod-caddy-1` | 巡检用的容器名 |

**`COMPOSE_FILE` 自动探测顺序**（`COMPOSE_DIR` 之下）：
`compose.yaml` → `compose.yml` → `docker-compose.yml` → `docker-compose.yaml`。
生产真实文件为 `/srv/new-api/production/compose.yaml`（`COMPOSE_DIR=/srv/new-api/production` 时第一个即命中）。

**`ops.env` 用法示例**（把服务器实际路径固化，脚本每次运行自动载入）：
```sh
# /srv/new-api/ops/ops.env
COMPOSE_DIR=/srv/new-api/production
COMPOSE_FILE=/srv/new-api/production/compose.yaml
COMPOSE_PROJECT_NAME=newapi-prod
LOCK_DIR=/srv/new-api/ops/locks
RELEASES_DIR=/srv/new-api/releases
KEEP_IMAGES=3
```

---

## 3. 三把锁的语义

生产已存在三把锁（语义如下，**锁文件真实路径需人工核对后写入 `ops.env`**）：

| 锁 | 语义 | 谁持有 |
| --- | --- | --- |
| 发布锁 | 序列化「备份→拉取→改 `.env`→重建 app→健康检查」，防止并发发布互相踩踏 | `deploy.sh` / `rollback.sh` 通过 `with_publish_lock`（`flock -x`） |
| 构建缓存锁 | 保护 BuildKit 缓存维护（`docker buildx prune`）不与构建/发布竞争 | `build-cache-maintenance.sh`（迁移后本机不再构建，仅保留） |
| 容量锁 | 保护磁盘清理动作，避免清理与发布窗口重叠 | 容量维护脚本 |

> `with_publish_lock` 在 `flock` 不存在时**直接 die**，不 fallback 到无锁运行。

---

## 4. 发布 / 回滚步骤

### 发布（唯一发布操作人执行）
```sh
# 1) CI 在 main 上构建并推送 GHCR；从 Actions 的 Job Summary 拿到完整 40 位 sha
# 2) 服务器执行发布：
/srv/new-api/ops/deploy.sh <full-40-sha>
#    可选：--image-digest sha256:...   （比对 digest，防拉错）
# 3) 查看可回收镜像（先预览，再确认执行）：
/srv/new-api/ops/prune-images.sh preview
/srv/new-api/ops/prune-images.sh cleanup --apply --keep 3
# 4) 巡检：
/srv/new-api/ops/healthcheck.sh local
```

### 回滚
```sh
# ⚠️ 先人工核对：旧应用与当前数据库 schema 的兼容性（本脚本不回滚数据库）
/srv/new-api/ops/rollback.sh <prev-40-sha>
```

### 关键行为
- 只重建 `app` 一个服务：`docker compose -p newapi-prod up -d --no-deps app`。
- 改 `.env` 前先备份 `.env.bak.<UTC ts>`，再用**临时文件 + `mv` 原子替换**只改 `APP_IMAGE=` 一行。`.env` 位于 `/srv/new-api/production/.env`。
- 取锁后、备份前完成 `ensure_env_ready`（含 compose project/服务/容器校验，见下）。
- 失败**不自动回滚**（生产规则：维护失败只告警），打印带回滚命令的明确提示后非零退出。
- 成功后把 `<UTC> sha=.. ref=.. digest=.. backup=.. prev=..` 追加到 `$RELEASES_DIR/releases.log`。

### 发布前置校验（`ensure_env_ready`，取锁后、备份前）
1. `COMPOSE_FILE` 存在且 app 服务的 `image:` 引用 `${APP_IMAGE}`；`/srv/new-api/production/.env` 含 `APP_IMAGE=`。
2. `docker compose -f "$COMPOSE_FILE" -p "$COMPOSE_PROJECT_NAME" config --services` 的服务列表**包含** `$APP_SERVICE`。
3. 目标容器 `$APP_CONTAINER` **已存在**（`docker container inspect` 成功）。
4. 该容器的 `com.docker.compose.project` 标签**等于** `$COMPOSE_PROJECT_NAME`。

> 这三条（2/3/4）专门把「project 名选错 → 另起一套并行容器」变成**大声失败**：任一项不满足即 `die` 并给出修复指引，**绝不允许改完 `.env` 才发现**。

---

## 5. 一次性服务器迁移步骤

> 目标：把服务器从「本机构建」切换为「从 GHCR 拉取」。**只是把既有 `ops/` 换成仓库版本并补齐路径配置，不重建数据。**

```sh
# ① 备份既有 ops 目录
cp -a /srv/new-api/ops /srv/new-api/ops.bak.$(date -u +%Y%m%dT%H%M%SZ)

# ② 同步本仓库的 ops/ 到服务器（保持 backup.sh / build-cache-maintenance.sh 等既有脚本不被覆盖）
#    推荐逐一 diff 后再放置，避免误覆盖生产专用脚本：
#      rsync -av --exclude='ops.env' --exclude='locks/' ops/ /srv/new-api/ops/

# ③ 核对并写入 ops.env（服务器真实值）：
#      COMPOSE_DIR=/srv/new-api/production
#      COMPOSE_FILE=/srv/new-api/production/compose.yaml
#      COMPOSE_PROJECT_NAME=newapi-prod
#      以及 锁路径 / RELEASES_DIR 等
$EDITOR /srv/new-api/ops/ops.env

# ④ 只读校验：能只读地确认 compose 结构与环境符合发布预期
/srv/new-api/ops/deploy.sh --help
/srv/new-api/ops/healthcheck.sh local

# ⑤ 【关键】确认 project 名正确：
docker compose ls        # 必须能看到 project = newapi-prod 的编排
#    若显示的 project 不是 newapi-prod，请**立即停止**并核对 ops.env 的 COMPOSE_PROJECT_NAME。

# ⑥ 验证可拉取（不改变运行态）：拉一个已存在或已知的 tag
/srv/new-api/ops/pull-image.sh <full-40-sha> --record

# ⑦ 首次以新方式发布（内部会在取锁后校验 project/服务/容器）
/srv/new-api/ops/deploy.sh <full-40-sha>
```

**生产真实坐标（务必对齐）**：编排文件 `/srv/new-api/production/compose.yaml`、compose project `newapi-prod`、
服务名 `app`、`.env` 位于 `/srv/new-api/production/.env`。

> ⚠️ **project 名检查**：若 `docker compose ls` 显示的相关 project 不是 `newapi-prod`（例如落到 `production`），
> **立即停止**，把 `ops.env` 的 `COMPOSE_PROJECT_NAME` 改回 `newapi-prod`。project 名选错会让 `up -d` 另起一套并行容器。
> （`deploy.sh` / `rollback.sh` 的 `ensure_env_ready` 会在取锁后、备份前再次硬校验并 `die`，作为最后一道闸。）

若 ④ 报「未找到生产 compose 文件」或「image: 未引用 `${APP_IMAGE}`」：
说明 `ops.env` 路径不对，或现网 compose 仍在用 `build:`。按提示修正后再继续。**不要直接覆盖生产 compose**——
请先与 `deploy/docker-compose.prod.yml`（参考模板）做 `diff`。

### 5.1 迁移后的一次性回收（构建缓存）

构建搬到 CI 之后，**服务器不再产生 BuildKit 构建缓存**；但历史累计的构建缓存需要**人工一次性**回收。
请通过既有的 `build-cache-maintenance.sh`（`GC_MAINTENANCE_SCRIPT`，默认 `/srv/new-api/ops/build-cache-maintenance.sh`）执行：

```sh
# ① 只读预览：确认将要回收的 buildx 缓存规模，不做任何删除
/srv/new-api/ops/build-cache-maintenance.sh preview

# ② 确认此刻无活跃构建、且未处于发布窗口（必要时先取发布锁，避免与发布重叠）
# ③ 在持锁前提下执行清理
/srv/new-api/ops/build-cache-maintenance.sh cleanup
```

**红线**：只做 buildx 缓存回收（`docker buildx prune`）。**严禁** `docker system prune` / `docker image prune` / `docker volume prune`。
实际释放量**以执行前后 `df -h /` 与 `docker system df` 的实测为准**（镜像与 BuildKit 缓存存在重叠引用统计，**不可相加**）。
镜像本身的历史回收走 `prune-images.sh cleanup --apply`（见第 4 节）。

---

## 6. 严禁事项清单

**脚本中严禁出现**（任何人不得加入）：
- `docker system prune`
- `docker image prune`
- `docker volume prune`
- `docker compose down -v`
- `docker rmi $(...)`（宽泛/命令替换式批量删除）

**发布/回滚/清理的共同红线**：
- 不删数据卷；不动 `postgres` / `redis` / `caddy` 容器；不改它们的数据或配置。
- 发布只允许重建 `app` 一个服务（project 固定 `newapi-prod`）。
- 禁止 `docker compose down`（尤其 `-v`）。
- **不修改** `SHUTDOWN_TIMEOUT_SECONDS=120` 与容器 stop timeout 60（历史未决项 OPS-02）。
- 脚本/配置中**不得写入任何真实密钥、真实 token、真实 IP**；`.env` 内容不得进仓库。

清理**只**由 `prune-images.sh` 用**显式镜像 ID** 执行，且候选集**仅**来自 `$GHCR_PREFIX` 前缀。

---

## 7. 常见故障排查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `未找到生产 compose 文件` | `COMPOSE_FILE` 未探测到 | 写真实路径：`echo 'COMPOSE_FILE=/srv/new-api/production/compose.yaml' >> $OPS_ENV_FILE` |
| `image: 未引用 ${APP_IMAGE}` | 现网 compose 仍使用 `build:` | 改为 `image: ${APP_IMAGE}`；先 `diff` 参考模板 `deploy/docker-compose.prod.yml` 再改 |
| `缺少 APP_IMAGE= 行` | `.env` 未含该项 | 在 `/srv/new-api/production/.env` 增加 `APP_IMAGE=...` |
| 服务列表中不包含 `app` | `COMPOSE_PROJECT_NAME`/`COMPOSE_FILE` 指向错误 | 核对并写回 `ops.env`（project `newapi-prod`、compose `/srv/new-api/production/compose.yaml`） |
| 目标容器不存在 `newapi-prod-app-1` | project 名选错，或容器名不对 | 核对 `APP_CONTAINER` / `COMPOSE_PROJECT_NAME`；确认现网真实容器名 |
| compose project 标签不一致 | 容器实际 project ≠ `COMPOSE_PROJECT_NAME` | 把 `COMPOSE_PROJECT_NAME` 设为实际值（生产应为 `newapi-prod`） |
| `docker compose ls` 的 project 不是 `newapi-prod` | project 名选错 | **立即停止**，核对 `COMPOSE_PROJECT_NAME`（见第 5 节） |
| 获取发布锁超时 | 另一个发布正在进行 | 等待其结束；确认无残留进程后重试 |
| healthcheck 未通过 | 应用启动失败/依赖未就绪 | 查 `docker logs newapi-prod-app-1`；核对 `curl $HEALTH_URL_LOCAL` |
| 清理无候选 | 保留集覆盖了所有镜像 | 正常；`--keep N` 可调，或检查 `rollback-keep.txt` |
| `docker pull` 403/权限 | 网络或 GHCR 未 public | 确认包可见性为 public；本方案服务器无需登录 |

---

## 8. 磁盘收益说明（严谨口径）

迁移后服务器**不再产生构建期内容**（Go 编译缓存 / node_modules / Go 模块缓存 / Bun 缓存 / BuildKit 缓存都不再增长）。
一次性可回收量以**执行前后 `df -h /` 与 `docker system df` 实测为准**，预期量级约 **10–13 GiB 分阶段回收**，
**不承诺单一数字**。注意：镜像与 BuildKit 缓存存在**重叠引用统计，不可相加**。详见
[`docs/operations/cicd-ghcr.md`](../docs/operations/cicd-ghcr.md)。
