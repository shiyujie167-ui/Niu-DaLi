# 88API Seedance 经 new-api 网关接入无限画布：生产页面操作手册

> 更新说明：本地源码现已扩展为 `88api-seedance` 1.1.0，新增素材上传凭证接口和画布内置多素材适配。新功能及迁移方式见 [画布集成说明](../integrations/infinite-canvas/README.md#88api-seedance-多素材内置接入)。下文保留 1.0.0 单图方案的历史实测记录；第 7 节旧脚本仍会限制单图，使用新版内置接入时应清空该脚本。历史成功记录不代表新功能均已完成真实上游验收。

整理日期：2026-10-11。本文覆盖本会话从本地启动、88API Key 接入、插件配置、main 预览，到无限画布单图视频生成与播放的完整流程。面向生产管理员和使用画布的用户；示例域名均为占位地址，不含真实 Key、用户提示词、图片或视频地址。

**状态：本地单图生成和播放已成功；本会话未执行生产配置，生产版本兼容性、站点货币售价和实际账务尚未验收。** 本文是可按步骤执行的手册，不代表这些配置已在生产生效。

## 1. 是否需要发布生产代码

不能把本会话全部内容概括为“都不需要发布”。最终成功的无限画布链路运行在干净的团队 main `34872e8b3a340fcce41b7cb614c14b602ee5c6ce` 上，通过管理页面动态上传插件、渠道绑定和画布专用脚本实现；这条路径没有依赖原开发目录的未提交 Go 改动。

| 内容 | 本会话实际状态 | 生产需要做什么 |
| --- | --- | --- |
| 动态上传 `88api-seedance` 插件 | 已在本地 main 上传、启用并激活 | 生产支持相同插件 API 时，管理页面上传即可，无需重新构建主站应用镜像 |
| 专用渠道、模型权限、用户 Key、HTTP/1.1、strict 规则和价格 | 本地配置；价格和账务仍待核对 | 在生产独立配置和验收，不会随本地保存自动同步 |
| 画布渠道专用脚本、10 秒轮询／15 分钟等待 | 已在当前本地浏览器保存 | 在生产画布的对应浏览器配置；已有兼容画布时无需发布源码 |
| 上传足够像素的图片、重新连接或替换节点 | 用户素材与画布操作 | 按第 8 节使用合格素材，无需应用发布 |
| 将 Seedance 插件作为源码内置插件发行 | 本地验证时为新增插件；插件源码随本手册交付，尚未部署生产 | 如要内置到发行镜像，需要评审、合并及发布；动态安装可以不采用这条路径 |
| 原生视频工作台 Seedance 适配、后端防重复提交代码 | 原目录有未提交修改，未进入上述运行 main | 如要采用这些能力，需要整理变更、完成适用测试、评审合并及发布 |
| 独立无限画布站点 | 本地另起 3001，使用固定画布版本和 main 既有补丁 | 生产若没有兼容画布站点，需要单独构建部署画布；发布主站不会自动部署画布 |
| 图片尺寸预检、提交后立即持久化任务 ID、超时后恢复原任务 | 改进建议，尚未实现 | 如开发这些功能，需要发布对应画布前端 |

生产操作前先检查当前页面是否具备任务插件 API v1、New API 渠道插件绑定、精确路径 strict 规则、HTTP/1.1 选择和画布模型脚本编辑能力。缺少其中必要能力时，不能照搬本地成功结论，需要先升级对应应用。本次文档整理没有重新登录或检查生产。

本次源码交付与早期未发布候选的定位如下。插件、插件测试和两份文档纳入本次聚焦 PR；其它 Go 候选保留在原目录，不表示已完成发布验证：

| 源码位置 | 候选行为 |
| --- | --- |
| `plugins/tasks/88api-seedance/plugin.js` | 本次交付的上游契约实现；可作为经过审核的动态插件上传，或随审核后的镜像内置发行 |
| `constant/context_key.go`、`controller/relay.go` | 标记 Seedance 已尝试提交，遇到结果不明时停止宿主重试循环 |
| `relay/channel/task/jsplugin/adaptor.go`、`relay/channel/api_request.go` | 标记对应插件 POST，禁用该请求体的传输回放机制 |
| `controller/video_workspace.go` | 原生视频工作台识别官方 Seedance 能力、模型映射和相应参数范围 |
| `plugins/builtin_plugins_test.go` | 本次交付的插件请求、用量边界和任务生命周期契约测试；不代替生产验收 |
| `controller/relay_task_plugin_test.go` | 早期工作台／防重投回归候选，不属于本次 PR |

这次实际发生的真实生成请求虽从本地发起，仍会调用真实 88API 上游，可能产生费用；“本地开发”不等于“免费模拟”。

## 2. 最终范围、模型名称和 Key 的关系

最终只开放 **`seedance-2.5官方版`／`720p`**，首次验收使用 **4 秒、16:9、单张参考图加文字提示**。不要把模型名缩写成 `Sd-2.5`；该名称属于画布既有另一套网关配置，与本手册模型不是同一个路由标识。

| 项目 | 本手册最终配置 |
| --- | --- |
| 上游服务 | `https://api.88api.ai` |
| 网关任务插件 | key：`88api-seedance`；名称：`88API Seedance`；版本：`1.0.0`；API：v1 |
| 协议 | `openai_video`，创建／查询入口为 `/v1/videos` |
| 对外唯一模型 | `seedance-2.5官方版` |
| 分辨率 | 固定 `720p` |
| 时长 | 4～30 秒整数；首次使用 4 秒 |
| 比例 | `16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`；首次使用 `16:9` |
| 画布输入 | 非空文字；可选一张本地 PNG／JPEG／WebP 参考图 |
| 本地文件限制 | 大于 0 且不超过 10 MiB，是本集成的工程限制 |
| 参考视频／音频 | 此画布脚本不支持 |
| 首尾帧、多图 | 上游／插件存在相应能力，未通过本文画布脚本启用或验收 |
| 当前目标售价 | `720p`：1.2 站点货币单位／秒，须完成生产币种换算和账务验收 |

用户同时给过 480p 为 0.95 站点币／秒、1080p 为 3.2 站点币／秒。这两项仅作为当时的售价意向记录，**不在本轮启用范围内**；当前 2.5 官方版插件契约只允许 720p，不能因有价格便开放其它分辨率。2.0、fast、mini 也保持关闭。

所谓“将 88vip 的 Key 转成我们渠道的 Key”，是由网关建立上游渠道并向用户签发独立的网关令牌，不是把同一串 Key 改名：

```text
用户浏览器里的无限画布
  └─ 我们的公开 HTTPS 网关地址 + 该用户的网关 Key
       └─ new-api：鉴权、模型／分组路由、价格、任务持久化与结算
            └─ 88api-seedance 插件 + 后台渠道里的 88API 上游 Key
                 └─ https://api.88api.ai/v1/videos
```

88API 上游 Key 只填生产网关的后台渠道凭据。给用户的是我们网关签发的个人 Key 和网关地址；不要把上游 Key 填进画布脚本、默认配置或客户端。用户之前在会话中给过的真实 Key 不在本文复制，也不能据此认定该本地 Key 可直接用于另一套生产数据库。

## 3. 本会话完整处理记录

下表记录已发生事实。时间为 2026-10-11 UTC，上海时间加 8 小时；未独立采集时间的阶段只描述顺序。

| 阶段 | 问题／执行 | 结果和限制 |
| --- | --- | --- |
| 当前项目启动，10:29:53 验证 | 原后端挂载另一工作树；改用当前项目开发容器，复用本地 PostgreSQL／Redis 和 5173 前端 | 本地 3000／5173 健康，非生产；启动本身未验证生成能力 |
| 88API 规范与 Key 检查 | 阅读官方 Seedance／统一视频接口文档，`GET /v1/models` 返回 200，四系列模型可见 | 模型可见只证明鉴权和目录，不证明实际生成或计费 |
| 插件与售价要求 | 新增 88API Seedance 插件及工作台／防重投候选代码；用户明确站点币定价，并最后限定只启用 2.5／720p | 插件支持范围与渠道开放范围分开；售价未按美元盲目写入 |
| main 和画布预览，11:49:27 记录 | 原目录是 `codex/channel-weighted-selection`，有未提交改动；另建干净 main 预览工作树，启动独立画布 | 未覆盖原工作；运行 main 为 `34872e8b3…`，画布固定版本见第 4 节 |
| 本地入口配置 | 设置 `general_setting.canvas_link=http://localhost:3001`；本地相对徽标地址因 URL 校验改为等价本机绝对地址 | 仅本地入口修复，不能复制 localhost 地址到生产 |
| 点击生成报错 | `invalid api platform: 60`；main 尚未动态安装匹配插件，回落普通任务适配器 | 此前返回 400 的请求不是成功提交；需插件启用和渠道绑定 |
| 配置修复，12:32 记录 | 动态上传插件；保存画布专用脚本；增加精确 strict 规则；专用渠道固定 HTTP/1.1 | 10 个画布契约用例、7 个 strict 规则用例通过，无上游付费请求；当时任务数为 0 |
| 前两次图片任务 | 实际图片为 244×240，共 58,560 像素 | 两任务失败：分别提示至少 90,000 像素、宽度须在 300～6000px；画布显示尺寸不等于图片真实像素 |
| 用户要求 300px 图片并试到成功 | 保留原图，等比放大并补白到 300×300，共 90,000 像素；替换原图片节点，保留连接 | 成功创建新的真实生成任务，采用 2.5／720p／4 秒 |
| 第三个任务 | 13:00:55 提交，13:06:45 完成 | 上游成功，耗时 350 秒；画布原 5 分钟等待先超时 |
| 超时后的一次人工重试 | 13:07:05 再提交，13:11:44 完成 | 新建第四个任务，279 秒成功；不是恢复第三个任务，存在两次成功生成的潜在费用 |
| 画布播放与收尾，13:12:45 记录 | 当前视频 1280×720、实际媒体时长 4.041667 秒；加载无错误，点击播放并播放到末尾 | 本地单图生成、显示及完整播放已验收；共 4 任务、2 失败、2 成功、0 在途 |
| 轮询修正 | 画布脚本保存 10 秒查询、最长 15 分钟等待 | 保存成功，但未为了验证超 5 分钟路径再发新的收费任务 |
| 发布范围复核和初版手册 | 区分动态配置、未提交源码、独立画布和生产状态 | 在本地验证和初版整理阶段，未部署生产、未提交／推送；后续源码交付不等于生产已配置或账务已验收 |

12:32 的定价页面曾显示“当前计费 未配置”，且当时编辑入口只显示美元。后续生成成功之后没有重新读取价格，所以不能把该历史观察写成当前价格状态，也不能把生成成功当作售价／扣费已经正确。

## 4. 生产页面前置核对

先记录生产主站版本、画布站点版本、管理员权限和当前配置；在变更前保存可恢复的插件版本及配置备份。下面是本地已验证基线，不是已确认的生产版本：

| 层 | 本地基线 |
| --- | --- |
| 网关 | 干净团队 main：`34872e8b3a340fcce41b7cb614c14b602ee5c6ce` |
| 无限画布 | `basketikun/infinite-canvas` v0.19.0，提交 `dab19adc0847e32e39b7fc8ff90cb392561fb826` |
| 画布默认集成 | 应用该 main 的 `integrations/infinite-canvas/defaults.patch`，并为本模型另外配置专用脚本 |
| 插件源码 | [plugin.js](../plugins/tasks/88api-seedance/plugin.js)；SHA-256：`f4426caebd98d4857d40ed0ed82df098aa53b72c21ebbd43b7e4b910dde3fdab` |
| 脚本功能副本 | 第 7 节代码；SHA-256：`c747d7ea318fea3074c76c6a2434076d9ababe3e3e03a8525eb615b4969806a2` |

插件文件随本手册交付。跨电脑交接时，从选定的团队提交读取经过审核的插件文件并核对哈希；PR 合并不会自动配置生产渠道、价格或画布。以上哈希只标识本次版本，不代表之后修改的版本继续相同。

页面入口按核对的源码路由列出；不同生产版本的中文标签可能有差异，缺少字段时先确认版本，不用旧页面的相似设置代替：

| 操作 | 主站路径／页面 |
| --- | --- |
| 任务插件上传、启用、激活 | `/task-plugins`，需要超级管理员 |
| 上游渠道、插件绑定、模型／分组、HTTP 协议 | `/channels` |
| 模型价格编辑 | `/models/metadata` → 对应模型的“定价” →“表达式／可视化编辑器” |
| 分组基础倍率及特殊覆盖 | `/system-settings/billing/group-pricing`，最终有效倍率以消费记录核对 |
| 用户价格展示 | `/pricing` |
| 会话和请求重试规则 | `/system-settings/request-policies/routing` |
| 用户令牌 | `/keys` |
| 无限画布入口 | 系统设置中的“无限画布入口地址”，键 `general_setting.canvas_link` |
| 画布渠道脚本 | 独立画布 → 配置 → 渠道 → 编辑 → 视频模型 → 脚本 |

生产主站和画布应使用真实可访问的 HTTPS 地址，例如 `https://gateway.example.com`、`https://canvas.example.com`。画布入口应可直达画布，浏览器跨域预检和视频创建／查询／内容请求应实际可用。HTTPS 画布不能依赖本机 `localhost:5173`、`localhost:3001` 或访客电脑的环回代理。

画布是独立站点。若需要新部署，按目标 main 中的 `integrations/infinite-canvas/README.md` 和 `apply-defaults.sh` 固定版本、应用补丁、安装依赖、运行适用检查，在画布 `web/` 目录执行 `bun run build`，将 `dist/` 部署到独立 HTTPS 站点。不要把该集成补丁原有的其它网关模型／域名当成 88API 的专用默认配置。已有浏览器配置不会自动被新默认值覆盖。

## 5. 管理员配置：插件、渠道、路由和用户令牌

### 5.1 上传并激活插件

1. 打开生产 `/task-plugins`，确认 “Enable task plugins／启用任务插件” 开关开启。
2. 点击 “Upload plugin／上传插件”，选择经过审核的 [plugin.js](../plugins/tasks/88api-seedance/plugin.js)。上传前核对第 4 节哈希和源码，插件文件不需要写入任何 Key 或价格。
3. 确认插件 key 为 `88api-seedance`，版本为 `1.0.0`，协议为 `openai_video`。若上传后未激活，使用页面 “Activate now／立即激活”。
4. 刷新列表，确认 enabled／active 均已生效、运行状态已注册且无编译错误。若同模型存在路由冲突，先处理冲突，不强制覆盖未知插件。
5. 如生产有多个网关进程／副本，确认各实例运行时均已同步该插件版本；只在一个页面看到上传成功不等于所有实例已加载。

插件声明了四个系列模型，因为它记录上游契约；**安装插件不等于允许用户调用全部模型**，实际曝光由下一节渠道模型及用户权限控制。单图文件会转换为上游 multipart `input_reference`；不传图片时使用 JSON。插件负责验证 prompt、整数时长、720p、比例和字段，主机负责公共任务 ID、后台查询、退款与结算。

### 5.2 创建专用 Seedance 渠道

在生产 `/channels` 新建专用渠道，或复制已有可信的 88API 渠道后仅修改专用副本。建议先保持停用，在价格／规则核对后对测试账号开放。

| 字段 | 填写值 |
| --- | --- |
| 名称 | 自定，例如 `New API_Seedance_720p` |
| 类型 | `New API`，类型 60，与本地成功路径一致 |
| Base URL | `https://api.88api.ai`；不要重复附加 `/v1` 或 `/v1/videos` |
| Key／凭据 | 88API 上游 Key，仅在后台填写 |
| Models／模型 | 仅 `seedance-2.5官方版`，先不设名称映射 |
| Groups／分组 | 选择实际允许使用的生产测试组；本地 `dp` 只是示例，不要求生产照搬 |
| Upstream task plugins／上游任务插件 | 绑定 `88api-seedance` |
| HTTP Protocol／HTTP 协议 | `HTTP/1.1`，不是 Auto 或 HTTP/2 |
| HTTP/2 Connection Shards | 选择 HTTP/1.1 后页面可能自动置 1；不以调整分片替代协议设置 |

本次渠道类型是 New API/type60，但地址直接指向 88API。此插件对声明的 `vendor`／`new_api` 上游均采用相同 Bearer 和 `/v1/videos` 请求契约，因此本地成功路径不需要去 88API 供应商服务器安装插件。如果渠道指向的是**另一台 New API 网关**，则还需要确认那台网关安装和配置同一插件；不要把通用“上游网关安装插件”提示误解成必须修改供应商服务器。

如果原通用渠道也包含该 Seedance 模型，先规划唯一有效路线，再仅移除该模型的重复暴露，保留它的其它模型和原传输协议。本地由原渠道 9 复制为专用渠道 10，原渠道保留其它 11 个模型；这些是本地 ID，生产不能按 ID 9／10 直接修改。

保存后重新打开确认模型、分组、插件绑定和 HTTP/1.1。不以普通 `/v1/models` 可见或通用渠道测试成功替代异步视频验收。

### 5.3 为创建请求配置 strict 和禁止失败重试

打开 `/system-settings/request-policies/routing`，核对会话保持全局开关已启用；**在既有规则数组中追加／按优先级放置精确规则，保留 Codex、Claude 等现有规则**。若已有宽泛规则会先命中此模型／路径，需要把本规则置于它之前并验证匹配结果。不要为了单个视频模型把全站重试次数设成 0。

下面是一个规则对象，不是整站配置，也不是完整 rules 数组：

```json
{
  "name": "seedance 2.5 video submit single attempt",
  "model_regex": ["^seedance-2\\.5官方版$"],
  "path_regex": ["^/v1/videos$"],
  "key_sources": [{ "type": "context_int", "key": "token_id" }],
  "session_mode": "strict",
  "skip_retry_on_failure": true,
  "ttl_seconds": 3600,
  "include_using_group": true,
  "include_model_name": true,
  "include_rule_name": true
}
```

规则保存到 `channel_affinity_setting.rules` 数组对应项；若使用规则编辑窗口，保存规则后还需保存外层设置页。重新加载页面，确认模型和路径精确匹配、模式 strict、失败跳过重试开启。核验首次没有绑定缓存的创建请求也受限制；`GET /v1/videos/<id>`、`/content` 和其它模型不应被该精确路径规则命中。本地真实函数的 7 个离线用例验证了上述边界，生产仍需按所运行版本确认。

这条规则限制网关失败后自动重新提交；HTTP/1.1 用于本地验证的传输路径。两者**不能阻止用户再次点击、客户端自行重发，也不构成上游幂等保证**。若插件以后增加幂等头、换协议／请求机制，需重新验证传输重投行为。规则名中的 single attempt 也不能据此理解为同一用户一小时只允许生成一次。

### 5.4 向用户提供我们网关的 Key

管理员先准备测试账号、可用分组和权限，再由测试用户登录本人生产账号，在 `/keys` 创建自己的令牌，确认启用、有效期、额度、有效分组和模型权限。此页面创建的令牌归属当前登录账号；管理员在自己的账号下建出的 Key 归管理员，不能作为测试用户个人 Key 交付。固定分组应与专用渠道可用分组匹配；在“高级设置 → 模型限制”仅选择 `seedance-2.5官方版`，保存后重新打开确认。模型限制空选表示全部允许，不表示全部禁用；无模型限制时，也需要确认该用户其它渠道权限符合预期。

用户配置画布时拿到的是：

```text
Base URL：https://gateway.example.com
API Key：该用户自己的网关令牌（此处不提供真实值）
模型：seedance-2.5官方版
```

通过此 Key 查询网关 `/v1/models` 应看得到目标模型；实际生成还要求该分组可路由专用渠道、价格有效、额度足够。不能把上游 Key 或管理员浏览器登录会话交给最终用户。

## 6. 管理员配置：1.2 站点币／秒的价格

用户要求的是 **1.2 站点货币单位／秒**。当前系统底层价格和原始计费表达式使用 USD；“站点货币”输入模式用于换算，并非把底层表达式改成另一种货币。生产 `/api/status` 和定价面板用于核对实际显示币种及换算率；有效分组倍率需在 `/system-settings/billing/group-pricing` 核对 `GroupRatio` 和 `GroupGroupRatio` 特殊覆盖，特殊覆盖优先于渠道组基础倍率，最后通过该测试用户的实际消费记录确认。不能将本地历史 `7.3` 或自定义倍率 `1` 当成生产当前值。

设生产页面确认 **1 USD = R 站点币**、该测试 Key 的有效分组倍率为 **G**。下列直接输入 1.2 的步骤以 **G=1，且没有额外请求倍率或有效定价覆盖** 为前提：

```text
720p 单价 = 1.2 站点币／秒 = (1.2 / R) USD／秒
4 秒基础售价 = 4 × 1.2 = 4.8 站点币
按当前分组计价后的预期售价 = 4.8 × G
```

如果基础单价填写 1.2 而 G 不等于 1，用户最终单价也会变成 `1.2×G`。若业务要求该组最终售价恰为 1.2，需按已确认的业务政策把基础单价配置为 `1.2/G` 站点币／秒，内部 USD 单价为 `1.2/(R×G)`；4 秒计算器基础价为 `4.8/G`，再乘 G 后才是目标 4.8。此换算要求 G 有效且大于 0，不得为了单个模型擅自修改全站分组倍率。一个共享基础价也不能同时保证不同 G 的组都付相同终价，需先明确对哪些组提供这个售价。

还需核对该模型／插件是否已有生效的插件专属表达式、模型表达式或请求条件规则；`requestRuleExpr` 会合并到最终表达式，请求倍率可能进一步改变费用。存在这些因素时不能只套上面的简化公式，应核对实际命中的计价配置和消费记录。

当且仅当站点币与 USD 真实配置为 1:1 时，二者数字相同。不要为了此模型直接改变全站币种或全站汇率。

1. 打开 `/models/metadata`，找到完整模型名 `seedance-2.5官方版`，进入模型“定价”。
2. 选择“Expression／表达式 → Visual editor／可视化编辑器”，插件 schema 应呈现 `resolution=720p` 的价格矩阵行；`seconds` 的价格列为“视频生成单价”，单位 second，额外费用保持 0。不要把 1.2 填入旧版“按次价格”、每次调用总价或 token 价格。
3. 若该版本提供 “Site currency／站点货币” 选择器，选择生产实际站点币，G=1 时输入 1.2；若已确认要补偿其它有效 G，则按上述政策输入基础价 `1.2/G`。核对页面提示的换算率及保存后的 USD 对应值。
4. 若页面只能输入 USD，先明确站点币定义、R 和 G；G=1 时填 `1.2/R` 的 USD 秒单价，按上述政策补偿 G 时填 `1.2/(R×G)`。原始表达式同样必须使用 USD。不明确换算或有效价格时保持对公众关闭，先完成价格核对。
5. 保存后重新打开，并在 Cost calculator／费用试算的“用量 · 视频生成单价”输入 4 秒，`resolution=720p`。计算器显示的是未含分组／请求倍率的基础价格：直接基础价 1.2 时应为 4.8 站点币，补偿 G 时应为 `4.8/G`；最终组价和请求倍率需另核对，不能只看计算器便认定实际扣费。
6. 在 `/pricing` 的用户视角确认价格和货币展示，实际一次验收后核对消费、预扣与结算；失败任务的退还也需独立核对。

此插件记录的计费用量是**提交时校验通过的请求时长**和分辨率；完成时不使用未经约定的媒体时长覆盖它。因此本轮请求 4 秒、成品媒体 4.041667 秒，不能自行按 4.041667 秒收取费用。也不要用上游钱包扣款反推替代用户侧请求计价。

所核对源码只在站点货币配置为 CNY／CUSTOM 且汇率有效时提供站点货币选择器；若生产当前显示 USD，单个定价窗口不会自动定义另一种站点币。先确认既有全站业务口径，不能因选项不存在便擅自修改全站货币设置。

本轮没有核对两成功任务各自消费、两失败任务退款或价格换算实际生效情况。即使价格未配置时能够生成，也不应视为商业账务验收通过。

## 7. 画布配置和完整可粘贴脚本

### 7.1 配置我们的网关渠道

1. 打开生产独立无限画布，进入“配置 → 渠道”。新建或编辑指向我们网关的 OpenAI 协议渠道。
2. Base URL 填 `https://gateway.example.com` 这样的网关根地址；不要填 88API 地址，不要填 `/v1/models`。本脚本 `http` 会接入 `/v1` 和渠道 Bearer Key，不要再在脚本加 `/v1` 或重复写认证头。
3. API Key 填当前用户自己的生产网关令牌。获取模型目录，选择／添加完整模型名 `seedance-2.5官方版`，明确能力为“视频”。
4. 为这个视频模型打开脚本编辑器，粘贴下一节完整代码。不是给整个渠道所有模型设置同一个脚本。
5. 先保存脚本编辑器，再保存外层渠道编辑窗口，最后完成配置。重新打开模型确认显示“脚本已设”。只保存内层窗口可能无法持久化外层修改。
6. 设置视频参数为 720p、16:9、4 秒。其它分辨率即使出现在通用 UI 中，也不能在本脚本使用；脚本会拒绝。

画布默认 OpenAI 视频请求携带的字段不一定适合 88API，原有网关补丁的 `reference_images`、`with_audio` 等契约也不适合直接复用到此模型。专用脚本只发送 `model`、`prompt`、字符串 `seconds`、`resolution`、`ratio` 和可选单图 `input_reference`，避免默认 `generate_audio`、`callback_url` 等不支持字段。

配置与脚本保存在当前浏览器画布配置中，**不是自动写入所有用户的全站默认值**。更换浏览器、清空站点数据或换电脑后需要重新配置／安全导入。导出整份画布配置可能包含 API Key、WebDAV 等凭据，只能作为受控备份；分发模板必须逐项去除个人密钥。若希望所有新用户自动具备该模型及脚本，需要另行实现和发布无密钥默认配置。

### 7.2 专用脚本

以下为当前已保存版本的功能副本，10 秒轮询一次，最多等待 15 分钟。代码不自动重试生成 POST。浏览器停止等待／取消本地执行不等于上游任务取消，也不等于退款。

```javascript
/**
 * 88API Seedance official 2.5 through the local New API gateway.
 * @param {string} prompt - video description
 * @param {string[]} images - zero or one PNG/JPEG/WebP data URL
 * @param {File[]} videos - not supported by this integration
 * @param {File[]} audios - not supported by this integration
 * @param {object} params
 * @param {string|number} params.seconds - integer duration, 4–30 seconds
 * @param {string} params.resolution - must be 720p
 * @param {string} params.ratio - supported aspect ratio
 * @param {string} model - seedance-2.5官方版
 * @param {object} http - joins /v1 and adds the channel bearer key
 * @param {function} poll - waits up to 15 minutes for an upstream task
 * @param {AbortSignal} signal - cancellation from the canvas
 * @returns {Promise<{url:string}|Blob>}
 */
async function generateSeedanceVideo({ prompt, images, videos, audios, params: { seconds, resolution, ratio }, model, http, poll, signal }) {
  if (model !== "seedance-2.5官方版") throw new Error("此脚本仅支持 seedance-2.5官方版");
  if (!prompt.trim()) throw new Error("请填写视频提示词");
  const duration = Number(seconds ?? 4);
  if (!Number.isInteger(duration) || duration < 4 || duration > 30) throw new Error("视频时长必须是 4–30 秒的整数");
  if (resolution && resolution !== "720p") throw new Error("当前仅启用 720p，请先选择 720p");
  const aspectRatio = ratio || "16:9";
  if (!["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"].includes(aspectRatio)) throw new Error("不支持此视频比例");
  if (images.length > 1) throw new Error("当前仅支持一张本地参考图");
  if (videos.length || audios.length) throw new Error("当前不支持参考视频或参考音频");

  const form = new FormData();
  form.set("model", model);
  form.set("prompt", prompt.trim());
  form.set("seconds", String(duration));
  form.set("resolution", "720p");
  form.set("ratio", aspectRatio);
  if (images[0]) {
    if (!/^data:image\/(png|jpeg|webp);base64,/i.test(images[0])) throw new Error("参考图必须是 PNG、JPEG 或 WebP");
    const blob = await (await fetch(images[0], { signal })).blob();
    if (!blob.size || blob.size > 10 * 1024 * 1024) throw new Error("参考图必须大于 0 且不超过 10 MiB");
    const extension = blob.type === "image/jpeg" ? "jpg" : blob.type === "image/webp" ? "webp" : "png";
    form.append("input_reference", blob, `reference.${extension}`);
  }

  const task = await http.post("/videos", form);
  if (typeof task.id !== "string" || !task.id.trim()) throw new Error("网关未返回视频任务 ID");
  const path = `/videos/${encodeURIComponent(task.id)}`;
  return await poll(
    async () => {
      const state = await http.get(path);
      if (state.status === "failed" || state.status === "cancelled") throw new Error(state.error?.message || "视频生成失败");
      if (state.status !== "completed") return null;
      const url = [state.url, state.video_url, state.result_url].find(value => typeof value === "string" && /^https?:\/\//i.test(value));
      if (url) return { url };
      const blob = await http.get(`${path}/content`, { responseType: "blob" });
      if (!(blob instanceof Blob) || !blob.size) throw new Error("网关未返回视频内容");
      if (blob.type.includes("json")) {
        const body = JSON.parse(await blob.text());
        throw new Error(body.error?.message || body.message || "视频内容下载失败");
      }
      return blob;
    },
    result => result,
    { intervalMs: 10000, timeoutMs: 900000 },
  );
}

return await generateSeedanceVideo({ prompt, images, videos, audios, params, model, http, poll, signal });
```

脚本注释中的 local 表示它来源于本地验证，不包含本机地址；实际服务地址由画布渠道配置提供。它不在客户端校验图片像素，这一项需要用户按第 8 节准备，并由上游做最终校验。

## 8. 用户操作：图片、节点和生成

### 8.1 准备真实像素合格的图片

本轮上游明确拒绝了 244×240 的图：总像素必须至少 90,000，另一错误要求参考图宽度 300～6000px。本地 300×300 PNG 成功；这是本次复现的最小方形素材，不代表完整上游尺寸规范已覆盖全部比例和格式。建议使用清晰、两边至少 300px 的合格素材，文件仍需不超过 10 MiB，并按上游最新规范检查更大图片的边长限制。

在画布上拖大图片节点只改变显示框，不改变原图像素。需要在图片编辑器中调整导出文件的真实分辨率，再替换节点图片。放大小图不能补回细节，只能满足像素门槛；生产优先使用足够分辨率的原始图片。

本次 macOS 实际操作相当于下面两步，分别保存新文件，保留原图；示例文件名使用尚不存在的名称：

```bash
# 先等比缩放最长边到 300，再补白边成为 300×300
sips -Z 300 "/path/to/reference-original.png" --out "/path/to/reference-resized-v1.png"
sips -p 300 300 --padColor FFFFFF "/path/to/reference-resized-v1.png" --out "/path/to/reference-300-v1.png"
sips -g pixelWidth -g pixelHeight "/path/to/reference-300-v1.png"
```

Windows／Linux 可用图片编辑器做等比缩放后补画布；最终确认文件真实宽高均为 300，而不是仅查看画布节点大小。不必复制用户本轮图片，手册也不包含其素材。

### 8.2 连接和生成

1. 在画布上传准备好的单张参考图；若已有节点，用“替换图片”替换其素材，可保留节点和连接。
2. 添加文字节点，写非空视频描述，例如“保持主体外观，镜头缓慢环绕，动作自然”。文字加图片均连接到目标视频节点。只有图片而没有 prompt 时，本脚本会拒绝，不会暗中补提示词。
3. 视频节点选择配置了专用脚本的网关渠道和 `seedance-2.5官方版`，参数选择 720p、16:9、4 秒；不连接第二张图片、参考音频或参考视频。
4. 点击生成一次；提交后不要反复点击。即使显示长时间排队也等待查询结果，首次可能需要数分钟。
5. 生成完成后确认视频可加载，点击播放并确认能播放到末尾。必要时将成品下载／归档到自己的存储，上游结果链接并非永久存储。

本轮仅单图＋文字实际成功，文字无图路径有离线契约验证但未独立做真实上游验收。多图、首尾帧、其它比例／时长及其它模型均不能写成已经验收。

## 9. 超时、任务恢复和重复收费

### 9.1 等待超时不代表任务失败

原画布插件默认轮询预算为 300,000ms，即 5 分钟。第三个任务用了 350 秒，在画布显示超时后后台仍成功；随后人工“重试”产生第四个任务，279 秒也成功。两个成功任务互相独立，不能把第二次描述为只查询第一次。

脚本现在覆盖为 900,000ms，即 15 分钟。该预算用于减少早超时，已保存但未用新的长任务实测，也不能承诺 15 分钟内必定完成。浏览器网络中断、刷新或关闭页面也可能让本地等待结束，上游任务继续。

### 9.2 当前画布恢复能力的限制

核对的画布版本里，脚本的创建与轮询全部完成后才把结果交回画布；插件路径没有立即将上游公共任务 ID 持久化到节点 `videoTaskId`。因此脚本等待超时后，普通“重试”可能重新执行 POST，无法自动恢复原任务。

“获取任务状态”只在节点已有可用 task ID 的情况下出现，不是可任意输入 task ID 的恢复入口。不要让用户寻找当前版本没有的手动 ID 输入框。保存 15 分钟脚本也没有实现任务 ID 持久化。

### 9.3 正确处理已经提交的任务

1. 从创建响应或生产任务记录找到**网关公共任务 ID**。管理记录可能同时显示内部数值 ID 和上游 ID，不要直接把它们当公共 URL 的 ID；管理员应核对映射。
2. 先通过同一个网关、同一个有权限的用户 Key 查询原任务，使用 `GET /v1/videos/<公共任务ID>`；不要向生产重新发送 POST 来“查询”。
3. `queued`／`in_progress` 时继续查；`completed` 时取得结果并播放／下载；`failed` 时保留错误、确认是否已退款，再决定是否需要新生成。
4. 如果 POST 本身断线、根本未拿到 ID，先由管理员在受控任务／渠道记录中检查是否已接收。不明确时避免立即重发；供应商未承诺可用的客户端幂等键。

下面只执行 GET，`GATEWAY_API_KEY` 需由操作者在受控环境预先提供，不将真实值写入文档或脚本：

```bash
GATEWAY_BASE_URL='https://gateway.example.com'
PUBLIC_VIDEO_TASK_ID='REPLACE_WITH_PUBLIC_TASK_ID'
curl --fail-with-body "$GATEWAY_BASE_URL/v1/videos/$PUBLIC_VIDEO_TASK_ID" \
  -H "Authorization: Bearer $GATEWAY_API_KEY"
```

查询地址需要公共 ID；如 ID 不是普通安全路径片段，客户端应先按 URL 路径编码，画布脚本已使用 `encodeURIComponent`。完成后优先使用返回的有效 `url`／`video_url`／`result_url`；或通过同网关 `/v1/videos/<id>/content` 获取。向结果 CDN 地址下载时不要携带任何网关或 88API Key。

受控开发排障可以临时使用“仅 GET 已知任务”的脚本找回结果，结束后恢复正常脚本；本轮成功验收没有采用这种恢复方案。给所有用户提供可靠恢复入口仍需后续前端开发。

## 10. 常见问题对应操作

| 现象 | 本次原因／检查项 | 处理 |
| --- | --- | --- |
| `invalid api platform: 60` | 视频 POST 没有进入匹配插件，回落普通 type60 适配器 | 检查插件开关、enabled／active／运行注册状态、协议／模型及专用渠道绑定 |
| 模型目录能看到但无法生成 | 目录可见不能证明任务协议、分组、额度或价格正确 | 检查实际 POST 返回、有效分组、渠道模型与插件；不反复提交 |
| unsupported parameter／字段不支持 | 通用画布字段与 88API 契约不符 | 保存本模型专用脚本，检查两层保存，去除 generate_audio／callback_url 等字段 |
| prompt required | 只连接了图片，没有非空文字 | 添加文字并连接到视频节点 |
| 要求至少 90,000 像素 | 原图真实尺寸过小 | 导出合格文件再替换；节点拖大无效 |
| 宽度须在 300～6000px | 素材边长未达要求 | 检查真实图片宽高，本轮用 300×300 成功；对其他尺寸查最新上游规范 |
| 400 不支持分辨率／时长／比例 | 不是 720p，时长不是 4～30 整数，或使用 auto 等比例 | 固定首次参数 720p／4 秒／16:9；duration 与 seconds 不同时传 |
| 超过 5 分钟前端报超时 | 未保存 15 分钟脚本，或其它路径仍用默认预算 | 重新核对脚本；先 GET 原任务，禁止把重试当恢复 |
| 15 分钟后仍未完成 | 本地等待预算结束或上游持续排队 | 保存任务 ID，继续由受控查询检查，等待不等于退款 |
| `strict_session_binding_unavailable` | 已绑定渠道被停用／改变，或缓存与规则不一致 | 管理员核对精确规则和绑定；按对应规则清理失效绑定，不批量删除其它会话缓存 |
| 401／403 | Key 属于错误网关、失效或权限不足 | 核对用户 Key、有效分组／模型权限，不用上游 Key 替换用户令牌 |
| CORS／混合内容错误 | 网关跨域策略、HTTPS 或地址填写不正确 | 核对真实 HTTPS 网关和跨域预检，不依赖访客 localhost 代理 |
| 完成但视频加载失败 | 结果链接失效、内容错误或浏览器访问限制 | 查询原任务与 content，检查响应 MIME／状态；不重新生成来替代下载排障 |
| 4 秒费用不是预期 4.8 站点币 | 秒计价、汇率、有效分组倍率或定价模式不符 | 用相同组试算并核对消费，按第 6 节处理 |

任务查询以 `status=completed` 判成功，不只看 progress=100。429／5xx 查询可以按响应退避，但生成 POST 的失败／断线不能照抄 GET 重试策略。

## 11. 生产验收、关闭入口和后续发布

### 11.1 配置完成后的验收表

由指定操作者使用专用生产测试账号执行，并记录时间、目标版本、插件哈希、渠道名称、公共任务 ID 的脱敏标识和结果；不要保存可用令牌、认证头、客户提示词／媒体地址。

| 验收项 | 合格标准 | 本会话已验证范围 |
| --- | --- | --- |
| 插件运行 | 生产所有网关实例加载匹配版本，无编译／路由冲突 | 仅本地 main 上传启用成功 |
| 路由 | 只开放 2.5／720p，用户有效组命中专用渠道／插件 | 本地渠道配置确认；生产待验 |
| 请求契约 | 单图 multipart input_reference、无不支持字段 | 本地离线契约及真实单图成功 |
| 创建／查询重投 | exact strict + HTTP/1.1 生效，失败不会被网关自动重复 POST | 本地规则 7 例；不覆盖用户再次点击和上游幂等 |
| 图片输入 | 合格真实尺寸；小图有明确错误 | 本地 244×240 失败、300×300 成功；未实现前端像素预检 |
| 等待／结果 | 单次创建，查询完成，画布播放到末尾 | 本地媒体播放已验；15 分钟保存后未新增长任务验证 |
| 售价 | seconds=4、720p；指定测试组在约定条件下最终 4.8 站点币，基础价、汇率、有效组倍率和请求规则均可解释 | 本地和生产实际价格／费用未独立验收 |
| 结算／退款 | 成功结算可追踪，失败退款核对，重复任务分别对账 | 未验收，不能从播放成功推导 |
| 用户配置 | 新用户可按手册设置，不泄露上游 Key | 当前浏览器配置不自动覆盖其他用户 |
| 文字生视频 | 无参考图也能完成真实生成 | 本地仅离线契约通过，真实路径待验 |

真实上游验收会收费，应在授权范围内明确次数和预算；本手册没有自动执行生产测试或授权无限次重试。无需仅为证明 15 分钟数值已保存再重复付费生成。

### 11.2 出现问题时关闭新提交

先停止该模型的新提交／对公众曝光，保留在途任务查询和结算能力，查清现有任务状态。不要先删除插件、任务或数据库数据，以免破坏查询／结算证据。需要回到旧版本时使用受控保存的插件版本和配置，并确认无不兼容在途任务。

调整专用渠道或 strict 规则后按规则检查已有绑定；不要自动把 Seedance 恢复到原通用渠道的 Auto 传输路径，也不要清空全站其它规则或用户画布。配置回退不等于应用镜像回滚。

早期 Go 防重投和原生视频工作台适配需要另外整理聚焦 PR，并完成所涉及计费、认证／数据库等适用验证。本次 PR 只交付插件、插件测试和两份文档；将插件内置到生产镜像仍须评审、合并及发布。按 [TEAM_WORKFLOW.md](../TEAM_WORKFLOW.md)，由指定发布负责人选定团队 main 的审核提交，记录目标提交、旧镜像、备份和数据库兼容性评估，再发布及验收；原目录其它未提交修改不属于本次生产候选。

### 11.3 已知待办

- 核对生产所运行版本和画布站点兼容性，再决定采用仅页面配置还是升级站点。
- 完成 1.2 站点币／秒实际价格、有效分组倍率、预扣／结算和失败退款验收。
- 对本地两个成功任务分别核对费用，保留两次生成的事实。
- 画布后续可增加真实图片像素预检，并在提交成功后立即保存公共任务 ID，让刷新／超时／重试先 GET 原任务；当前没有实现。
- 如需所有新用户默认拥有此模型和脚本，另行提供不含 Key 的默认配置并发布画布；当前仅记录手动配置流程。

## 12. 资料和证据边界

共享资料：

- [88API Seedance 视频接入简表](88api-seedance-video-guide.md)：上游广义参数／多图／首尾帧规范，与本轮限定画布范围区分。
- [Task Plugin API v1](plugin-api/v1.md)：插件契约、协议、用量字段、描述与单位要求。
- [本轮插件源文件](../plugins/tasks/88api-seedance/plugin.js)：随本手册交付；安装前从选定团队提交读取、审核并核对哈希。
- [计费规则](../.agents/rules/billing.md)、[团队发布流程](../TEAM_WORKFLOW.md)。
- 上游官方来源：[Seedance 官方版规范](https://88api.ai/zh/docs/api/video/seedance-official/)、[统一视频接口规范](https://88api.ai/zh/docs/api/video/video-api-standard/)。初次核对依据 2026-10-10 文档版本，后续使用前核对更新。

本机受控证据位于 `.codex/server/`，该目录排除 Git 和 Docker 构建，不随仓库克隆同步。它是管理员审计线索，生产用户按本文操作不依赖此目录：

| 本地记录 | 内容 |
| --- | --- |
| `.codex/server/records/2026-10-11T102953Z-local-development-startup.md` | 当前源码启动、服务健康、环境隔离 |
| `.codex/server/records/2026-10-11T114927Z-main-canvas-preview.md` | 干净 main 及独立画布启动、本地入口 |
| `.codex/server/records/2026-10-11T123200Z-local-seedance-canvas.md` | 动态插件、专用渠道、strict／HTTP1、17 个离线用例、当时未配置价格 |
| `.codex/server/records/2026-10-11T131245Z-local-seedance-300px-validation.md` | 小图失败、300×300、两个成功任务、实际播放和轮询保存 |

300px 结果摘要 SHA-256：`ee3e20bad60896216aaaeb53784fca6c9187e14c675fb3bc55e4cfbc52295bd1`。摘要来源为本地白名单 SQL／HTTP／容器检查及会话内图片／UI／DOM 核验派生报告，未保存媒体、凭据或完整浏览器配置。哈希只证明所存文本一致性，不证明未保存的浏览器状态、账务正确或生产已上线。
