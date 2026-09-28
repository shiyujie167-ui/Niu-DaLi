# 团队 Git 协作与 Ubuntu 生产发布

团队项目：Niu-DaLi（大力牛-Niu DaLi）。

适用场景：在 new-api 基础上维护团队自己的 API 服务。多人可能同时开发，也可能轮流接手；每个人在 Mac mini 或自己的电脑上开发，共用一个团队 Git 仓库，最终由 Ubuntu 服务器运行生产服务。保留原项目名称、QuantumNous 归属及许可证信息，继续遵守 [AGENTS.md](AGENTS.md)。

## 1. 固定分工

| 位置 / 角色 | 职责 |
| --- | --- |
| 开发者电脑 | 独立克隆仓库，修改、测试、提交；使用测试配置和测试数据 |
| 团队仓库 `origin` | 保存团队代码、任务分支、PR 和检查结果，是团队协作的唯一代码来源 |
| 官方仓库 `upstream` | 获取上游更新；通过独立任务分支评估和合并 |
| `main` | 保存已审核的集成结果；合并不代表已上线 |
| Ubuntu 生产服务器 | 运行负责人选定的版本，保存独立的生产配置与业务数据 |
| 发布负责人 | 决定发布版本、核实备份、执行发布并记录结果；一次只允许一人操作生产发布 |

先采用 `main` 加短期任务分支，不设置长期 `develop`、个人永久分支或服务器专用开发分支。多人不需要同时在线，通过已推送的分支和 PR 交接；不要共享同一个工作目录或正在编辑的分支。

## 2. 团队仓库首次设置

团队仓库由负责人指定为 `shiyujie167-ui/Niu-DaLi`。2026-09-28 在 GitHub 页面核实该仓库为公开模板仓库，已有一个 README 初始提交，并非空的私有仓库。负责人已在获知公开可见性后要求上传本机代码；仓库可见性未由代理更改。当前本机使用同一仓库的 SSH 地址：

```text
origin    git@github.com:shiyujie167-ui/Niu-DaLi.git
upstream  https://github.com/QuantumNous/new-api.git
```

本机已设置 `remote.pushDefault=origin` 和 `pull.ff=only`，取消了本地 `main` 对官方分支的旧跟踪关系。团队仓库的 README 初始历史与本地上游历史已完成整合。首次上传使用临时协作分支；随后负责人明确要求直接更新到 `main`，本次初始化按该指示将完整代码纳入 `main`，保留双方历史。后续日常开发仍遵循任务分支和 PR 规则。上述是本机 Git 配置，不会随提交传给其他成员。

本机原先没有 SSH 身份密钥，22 端口连接也被中断。现已在 `~/.ssh/` 生成专用 Ed25519 密钥，并通过本仓库的 `core.sshCommand` 使用 SSH 443 端口、固定身份文件和已核实的 GitHub 主机公钥；这不会修改其他项目的 SSH 配置。私钥权限为 `0600`，不进入项目目录或 Git。GitHub 已保存本机的仓库专用读写 Deploy key，`git ls-remote origin` 和 `git fetch --no-tags origin` 均已通过。初次获取的远程 `main` 为 `c409330663feb20cad4d0bc2582f8e9f1a364d3c`。首次代码导入保留该提交及上游历史，不使用强推；分支保护仍需另行设置。每位协作者应使用自己的凭据，不能复制这台 Mac 的私钥。

先检查：

```sh
git status --short --branch
git remote -v
```

其他已有官方克隆的电脑，仅当 `origin` 确认仍是官方仓库、尚无 `upstream` 时，才执行下面的配置；当前本机已完成，不要重复执行：

```sh
git remote rename origin upstream
git remote add origin git@github.com:shiyujie167-ui/Niu-DaLi.git
```

团队仓库应保留项目现有 Git 历史。初次导入由维护者核实仓库目标、分支和待提交文件后执行，不使用 `git push --mirror` 或强制推送覆盖已有团队历史。初次将 `main` 导入空仓库是维护者执行的一次性初始化；日常开发仍全部通过任务分支和 PR。若远程已有 README 或其他提交，先比较双方历史并决定整合方式，不盲目推送或覆盖。

确认团队 `main` 存在后，运行 `git fetch origin`，再用 `git branch --set-upstream-to=origin/main main` 绑定本地分支，并检查 `git remote -v` 和 `git branch -vv`。

新成员在团队历史导入后直接克隆，不需要重命名远程：

```sh
git clone git@github.com:shiyujie167-ui/Niu-DaLi.git new-api-team
cd new-api-team
git remote add upstream https://github.com/QuantumNous/new-api.git
git config --local remote.pushDefault origin
git config --local pull.ff only
```

项目仓库名称可以作为团队地址使用，源码中的 new-api 名称、QuantumNous 归属和许可证保持原样。

团队 GitHub 仓库由维护者设置：

- 成员使用各自账号和凭据，按职责授予权限，不共享账号或 SSH 私钥。
- 默认分支为 `main`；要求 PR 合并、另一名协作者批准、讨论已解决，禁止强制推送和删除 `main`。
- 要求实际运行并成功的 CI 检查；更新分支后重新检查，代码变更后重新评审。首次 PR 跑完后，从平台实际显示的检查名称中选择必需检查。
- 当前 [CI](.github/workflows/ci.yml) 包括 `Backend vet, build, and test` 和 `Frontend typecheck and test`。前端构建及领域专项验证仍按 `AGENTS.md` / `web/AGENTS.md` 执行，不能仅凭这两个检查推断所有要求均已覆盖。
- GitHub 私有仓库的分支保护等功能取决于账号方案；确认规则实际可用并已启用。此 Markdown 文件本身不会开启服务端限制。

现有 Docker、Release、Electron 等发布工作流沿用上游发布配置，有些会被标签推送触发。团队启用发布工作流前，先检查触发条件、凭据和目标仓库。现阶段使用完整提交 SHA 记录生产版本，不通过随意推送标签来触发发布，也不把原有工作流视为 Ubuntu 自动部署。需要团队自动发布时另行配置，保留上游项目归属信息。

## 3. 每次开始开发

先检查工作区。如果存在未提交修改，确认归属并保存到原任务分支；不要自动丢弃、覆盖或混入新任务，也不要在他人的工作目录中切换分支。

下面的命令仅用于工作区干净、团队 `origin` 已配置的情况；分支名用本次任务名替换：

```sh
git status --short --branch
git remote -v
git fetch origin
git switch main
git pull --ff-only origin main
git switch -c feat/channel-setup
```

命名约定：功能用 `feat/<task>`，修复用 `fix/<task>`，维护用 `chore/<task>`；AI 代理默认用 `codex/<task>`。一个分支只处理一项聚焦任务。`pull --ff-only` 失败时先检查本地提交与远程差异，不用强制重置解决。

提交前检查差异、运行适用验证，只暂存本次修改的具体文件：

```sh
git diff --check
git diff
git add path/to/changed-file
git diff --cached
git commit -m "feat: describe the change"
git push -u origin HEAD
```

`path/to/changed-file` 和提交说明都是示例，需要替换。推送前核实远程地址和暂存内容，尤其不要将本地配置、凭据或其他人的未完成工作一并提交。创建目标为团队 `main` 的 PR；模板和 AI 辅助披露遵守 `AGENTS.md` 的现有规定。

## 4. 轮流开发和交接

工作尚未完成时，可在任务分支提交清晰的阶段成果，推送后用 Draft PR 交接。仅保存在本机或 stash 中的修改，其他人无法接手。按照现有 PR 模板填写，并在适当段落包含：

- 分支、完整提交 SHA、当前负责人及下一位接手人。
- 已完成、剩余工作和已知问题。
- 实际运行过的验证命令及结果；未运行的检查明确注明。
- 配置变更的键名与用途，不写真实凭据。

接手人先确认前一位已停止修改该分支，再在自己的干净工作区操作。首次获取该分支：

```sh
git fetch origin
git switch --track origin/feat/channel-setup
```

本地已经有该分支时，使用 `git switch feat/channel-setup` 后执行 `git pull --ff-only origin feat/channel-setup`，不要重复创建。核对 `git rev-parse HEAD` 与交接记录，再继续工作。

共同使用过的分支不重写历史、不强推。需要同步团队 `main` 时，在任务分支上执行 `git fetch origin` 和 `git merge origin/main`，解决冲突后重跑相关验证。遇到其他人已更新远程导致推送失败，先获取和整合提交，不用强推覆盖。

两个人同时做不同需求时，各建分支；需要修改同一功能时先明确负责人和改动边界。原任务合并后，后续工作从最新 `main` 创建新分支，不继续向已合并分支累积修改。

## 5. 合并与上游同步

维护者异步检查 PR 的范围、行为、验证结果及配置影响，满足评审和检查要求后合并。日常任务使用 squash merge；首次代码导入和上游同步使用 merge commit，保留远程初始提交与上游的完整祖先关系。服务器发布记录使用合并到 `main` 后的提交 SHA，而不是功能分支合并前的 SHA。

上游更新也走独立 PR：从最新团队 `main` 创建 `chore/sync-upstream-<date>`，获取 `upstream` 后选择明确的上游版本或提交合并。解决冲突、验证团队定制功能，再评审合入。不要将官方 `main` 强制覆盖团队 `main`，不要让生产服务器直接拉上游更新。

## 6. Ubuntu 生产发布

初期采用负责人手动发布，不要求所有协作者拥有生产 SSH 权限。流程为：

```text
任务分支 → PR 和验证 → 团队 main → 负责人选定提交
                                      ↓
                       备份与升级检查 → 部署 → 验收与记录
```

每次发布依次执行：

1. 记录目标完整提交 SHA、当前线上 SHA 和当前应用镜像标识。确认目标属于团队 `main`，所需检查已通过、变更已审核。
2. 确认没有另一人正在发布，检查 Ubuntu 发布目录没有未解释的修改。配置、持久化数据和备份独立于源码发布目录管理。
3. 在升级前完成可恢复的数据库及必要文件备份，记录位置和恢复办法。涉及数据库升级时先完成项目要求的数据库验证，评估旧版程序是否仍能使用升级后的数据库。
4. 获取选定提交，构建适配 Ubuntu CPU 架构的应用镜像，或拉取由该提交构建的团队镜像。记录完整 SHA 与镜像 ID / digest 的对应关系，保留上一版镜像；不要用可变 `latest` 作为唯一发布记录。
5. 用已配置的生产 Compose 更新应用服务。例行应用发布不顺带升级数据库或 Redis，不删除数据卷，不运行 `docker compose down -v`。
6. 检查服务健康、错误日志，并用专用测试账号验证实际 API 请求、流式返回和消费记录。确认结果后记录发布时间、操作人、目标 SHA、镜像、备份与验收结果。
7. 失败时先判断旧版与当前数据库是否兼容。兼容时切回保留的旧应用镜像；不兼容时按评审过的修复/恢复方案处理。不要直接覆盖生产数据库，恢复备份可能丢失备份后的客户数据，必须明确影响和处理决定。

**当前部署边界：** [docker-compose.yml](docker-compose.yml) 使用官方 `calciumion/new-api:latest` 镜像，没有配置团队源码构建。因此只 `git pull` 或重启该容器，都不会运行团队源码修改。生产 Compose 的团队镜像引用、域名、配置路径和发布命令需要在确认实际 Ubuntu 环境后落地；本次协作规范不代表服务器已经配置、GitHub 保护已开启或自动部署已经启用。

发布记录保存在团队 PR / 发布记录或受控运维记录中；只记录备份标识和配置键名，不附密钥或数据库内容。需要自动部署时，再配置独立的生产发布入口、权限和部署互斥，避免多人触发并发上线。

## 7. 环境与敏感文件

| 内容 | 是否进入 Git | 管理方式 |
| --- | --- | --- |
| 源码、锁文件、Dockerfile、无密钥的共享配置与脚本 | 是 | PR 审核，统一版本 |
| `.env.example` / `.env.*.example` | 是 | 只含配置名和示例值，不含真实凭据 |
| `.env`、`.env.*` 实际配置、`local.env*` | 否 | 每台机器独立维护 |
| `服务器/` 下的登录信息和私钥 | 否 | 仅本地保存，也不进入 Docker 构建上下文 |
| 客户数据、上游 Key、余额、日志、备份 | 否 | 存储于独立环境及受控备份位置，不用 Git 同步 |

已有 `.gitignore` / `.dockerignore` 排除相关本地文件。忽略规则不会移除已经被 Git 跟踪或进入历史的秘密；发现已提交凭据时先停止继续传播，安排撤销/轮换与历史清理，不以新增忽略规则代替处理。

生产目前按应用、PostgreSQL、Redis 三个容器规划；人数增加不需要复制生产容器。每位开发者使用自己的测试数据库与缓存。若后续增加公共测试环境，数据库、Redis、端口、卷、凭据及测试渠道必须独立，严禁本机测试库覆盖正式库，也不以开发配置连接客户生产数据。

## 8. 落地清单

- [x] 确认团队仓库地址，并配置当前本机 `origin` / `upstream`。
- [x] 完成本机 SSH 认证并获取远程 `main`，核实远程初始历史。
- [x] 按负责人指示将首次导入代码纳入 `main`。
- [ ] 配置团队成员权限。
- [ ] 创建首个团队 PR，核实 CI 实际结果并启用 `main` 保护。
- [ ] 指定 PR 维护者、发布负责人和交接方式。
- [ ] 确认 Ubuntu 当前运行版本、配置与数据位置，建立备份和恢复方案。
- [ ] 配置团队应用镜像及生产 Compose，用测试账号完成一次发布验收。

这些事项需要实际配置和验证后再勾选，不能仅因文档已写入就视为完成。
