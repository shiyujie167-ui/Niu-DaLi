# 88API Seedance 官方版视频接入简表

核对日期：2026-10-11；依据官方文档 2026-10-10 版本。
初次已用提供的 Key 请求 `GET /v1/models`，返回 HTTP 200，下表四个系列模型均可见；当时只验证鉴权和模型列表。后续在本地干净 main 网关与独立无限画布，通过动态插件、专用渠道和单张 300×300 参考图，完成 `seedance-2.5官方版`／720p／4 秒真实生成及完整播放：两个成功任务耗时分别为 350 秒和 279 秒，成品为 1280×720、约 4.04 秒。第一次成功前页面等待超时，人工重试另建任务，不能记为仅生成一次；价格、扣费／退款未独立核对，本会话未进行生产配置或发布。本文不保存真实 Key。

生产管理员和画布用户按 [生产页面操作手册](88api-seedance-production-setup.md) 配置。该手册含完整脚本、Key 分层、站点币定价、图片要求、超时恢复及发布边界。本简表描述上游广义能力；本轮最终只开放 2.5／720p 的文字或单张本地参考图路径，其它模型、多图和首尾帧不代表已在画布启用或验收。

## 1. 三种功能怎么区分

三种功能统一调用 `POST https://api.88api.ai/v1/videos`，不需要额外的模式字段，**全部必须填写 `prompt`**。

| 产品功能 | 请求内容 | 实现说明 |
| --- | --- | --- |
| 文字生视频 | `prompt` | 不传图片 |
| 图片生视频 | `prompt` + `images` | 用户只选图时，应用补充默认提示词，例如“保持图片主体和风格，生成自然运动的镜头” |
| 文字和图片生视频 | 用户填写的 `prompt` + `images` | 图片提供外观、人物或场景参考，文字描述动作、运镜与情节 |

图生视频与文字加图片生视频属于同一种接口用法，区别在产品交互。普通参考图不等同于指定视频第一帧；需要首帧约束时使用第 4 节的字段。

## 2. 模型与参数

以下名称已在当前 Key 的模型列表中确认；以后接入仍应以 `data[].id` 为准。

| `model` | `resolution` | `duration` | 参考图片上限 | 首尾帧 |
| --- | --- | --- | --- | --- |
| `seedance-2.5官方版` | `720p` | 4–30 秒，默认 4 | 30 张 | 支持 |
| `seedance-2.0官方版` | `720p` | 4–15 秒，默认 4 | 9 张 | 支持 |
| `seedance-2.0-fast官方版` | `720p` | 4–15 秒，默认 4 | 9 张 | 支持 |
| `seedance-2.0-mini官方版` | `480p` / `720p`，默认 `480p` | 4–15 秒，默认 5 | 9 张 | 文档未明确，不建议使用 |

- `duration` 为整数秒；也可用字符串 `seconds`，二选一，建议统一用 `duration`。
- `ratio`：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`，默认 `16:9`，不支持 `auto`。
- 可选：`negative_prompt`（负面提示词）、`seed`（整数）。
- 本组仅支持轮询，**不要发送 `callback_url`**；不提供音频生成开关，**不要发送 `generate_audio`**。
- 不支持的分辨率返回 400，不会自动降档。旧版带分辨率的模型名仍可能可用，但固定档位并忽略 `resolution`，新接入使用上表名称。

以下通用示例保留 `seedance-2.0官方版`／720p／8 秒，用于说明上游接口；本轮生产计划与已成功的画布配置以 `seedance-2.5官方版`／720p／4 秒为准，不开放 2.0、fast 或 mini。其它模型没有本轮速度和费用实测，不能仅凭 fast／mini 名称判断性价比。

## 3. 三种上游通用请求示例（不属于本轮开放范围）

鉴权头为 `Authorization: Bearer <API_KEY>`，JSON 请求使用 `Content-Type: application/json`。下面是服务端直连 88API 的示例，上游 Key 仅保存在网关后台；画布使用我们生产网关签发的用户 Key 和网关地址，按生产操作手册配置，不向浏览器分发 88API 上游 Key。

**文字生视频**

```json
{
  "model": "seedance-2.0官方版",
  "prompt": "雨后城市街道，一辆银色汽车缓慢驶过，低机位跟拍，电影感",
  "duration": 8,
  "resolution": "720p",
  "ratio": "16:9"
}
```

**图片生视频：应用补充默认提示词**

```json
{
  "model": "seedance-2.0官方版",
  "prompt": "保持图片主体和风格，生成自然运动的镜头",
  "images": ["https://cdn.example.com/subject.jpg"],
  "duration": 8,
  "resolution": "720p",
  "ratio": "16:9"
}
```

**文字和图片生视频：用户描述具体变化**

```json
{
  "model": "seedance-2.0官方版",
  "prompt": "保持参考图中产品的外观，镜头缓慢环绕，灯光由冷色渐变为暖色",
  "images": ["https://cdn.example.com/product.jpg"],
  "duration": 8,
  "resolution": "720p",
  "ratio": "16:9"
}
```

图片地址为占位示例，须换成实际可下载的素材。把所选请求体保存为 `request.json`，服务端配置 `API_KEY` 后提交：

```bash
curl --fail-with-body 'https://api.88api.ai/v1/videos' \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  --data-binary @request.json
```

## 4. 图片上传与首尾帧

**参考图**使用顶层 `images` 数组；多图直接增加数组元素，不要同时填写 `image`、`images` 等别名。

**首尾帧**使用以下 `metadata` 替换普通参考图片字段：

```json
{
  "metadata": {
    "firstFrame": "https://cdn.example.com/start.jpg",
    "lastFrame": "https://cdn.example.com/end.jpg"
  }
}
```

这是合并到完整请求中的字段片段，仍须保留 `model`、`prompt` 等字段。可以只提供 `firstFrame`；`lastFrame` 必须搭配 `firstFrame`。首尾帧不能与普通参考图片、参考视频或参考音频混用。

图片建议 JPG / PNG / WebP，使用公网可匿名下载的 HTTPS 地址，生成期间保持有效，建议至少 60 分钟；不能依赖登录、Cookie、防盗链请求头或本机路径。

本地图片两种提交方式：

- **单图简便方式**：向同一生成接口提交 `multipart/form-data`，文件字段为 `input_reference`，同时填写模型、提示词等表单字段；只能传 1 个图片文件，不能再传 `images` / `image`。由客户端生成 multipart boundary。
- **多图统一方式**：服务端调用 `POST /v1/media/uploads`，提供文件 `size`、`mime_type`、`sha256`；摘要是 SHA-256 原始摘要的 Base64URL 编码，不带 `=`，不是十六进制。按响应的 `upload_url`、`method` 和 `headers` 上传原始二进制，HTTP 201 后将返回的 `url` 加入 `images`。上传仅使用返回的专用凭证，不发送 API Key；文件大小、MIME 和内容必须与申请时一致。

## 5. 查询、结果与异常

提交 HTTP 200 只表示任务接收成功。保存响应中的 **`id`**，不要依赖 `task_id`。

```bash
curl --fail-with-body "https://api.88api.ai/v1/videos/$TASK_ID" \
  -H "Authorization: Bearer $API_KEY"
```

首次等待 3–5 秒，之后每 10–15 秒查询一次：

| `status` | 处理 |
| --- | --- |
| `queued` / `in_progress` | 继续等待 |
| `completed` | 读取 `url`，兼容回退到 `video_url` / `result_url`，用于播放或下载 |
| `failed` | 展示 `error.message`，保留 `error.code` 排查 |
| `unknown` | 短暂重试；持续出现时联系平台 |

以 `status` 判断成功，不能只看 `progress=100`。页面可设置 15 分钟等待预算并保留任务 ID，之后继续查；停止等待不表示任务取消或退款。

生成 POST 超时不要盲目重发：平台未承诺客户端幂等键，可能重复生成并计费。查询遇到 429 / 5xx 可按 `Retry-After` 或退避策略重试；400 检查参数，401 / 403 检查 Key 和权限。

结果 URL 原样使用，保留必要参数、允许重定向，不自行拼接下载路径，也不向结果地址发送 API Key。平台上传素材和归档视频保存 30 天，重复访问不续期；上游官方直链按自身有效期处理。需要长期保留的视频应另存到自己的存储。

来源：[Seedance 官方版规范](https://88api.ai/zh/docs/api/video/seedance-official/) · [统一视频接口规范](https://88api.ai/zh/docs/api/video/video-api-standard/)
