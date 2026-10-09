# Grok 视频生成接口调用文档

更新日期：2026-08-31

本文档基于本项目当前的 Grok 视频生成前端、`POST /v1/videos` 任务入口、GrokAI 任务适配器和视频内容代理实现编写，说明如何通过 New API 调用 `grok-imagine-video-1.5` 生成视频。

配套 Node.js 示例代码：[`grok-video-example.mjs`](./grok-video-example.mjs)

## 1. 功能范围

当前支持以下请求形态：

1. 纯文生视频，不上传任何媒体。
2. 首帧模式，上传 0 或 1 张首帧图。
3. 参考图模式，上传 0 至 6 张参考图、0 至 3 个参考音频。

前端中的“首帧模式”和“参考图模式”是客户端状态，不需要向接口发送 `mode` 字段。客户端只需要根据模式构造不同的媒体字段：

| 前端模式 | 请求媒体字段 | 时长 |
| --- | --- | --- |
| 首帧模式 | 可选 `image` | `6 / 12 / 18 / 24 / 30` 秒，默认 `6` 秒 |
| 参考图模式 | 可选 `reference_images`、`reference_audios` | 固定 `6` 秒 |
| 任一模式无素材 | 不发送任何媒体字段 | 按所选模式的时长规则 |

所有请求固定使用：

```json
{
  "model": "grok-imagine-video-1.5",
  "resolution": "720p"
}
```

支持的画幅为：

- `16:9`
- `9:16`

## 2. 调用流程

Grok 视频生成是异步任务，完整流程如下：

1. 使用 API Token 调用 `POST /v1/videos` 创建任务。
2. 从响应的 `id` 或 `task_id` 取得公开任务 ID。
3. 调用 `GET /v1/videos/{task_id}` 轮询任务状态。
4. 状态变为 `completed` 后，调用 `GET /v1/videos/{task_id}/content` 下载视频。

```text
本地文件
  -> Data URL Base64
  -> POST /v1/videos
  -> task_id
  -> GET /v1/videos/{task_id}
  -> completed
  -> GET /v1/videos/{task_id}/content
  -> 视频文件
```

前端当前使用 10 秒轮询间隔。自行接入时建议也使用 10 秒左右的间隔，不要高频轮询。

## 3. 前置条件

### 3.1 New API 服务地址

本文档和示例代码统一调用 [https://geekapis.com](https://geekapis.com)：

```bash
export NEW_API_BASE_URL='https://geekapis.com'
```

`NEW_API_BASE_URL` 应为站点根地址，不要在末尾附加 `/v1`。

### 3.2 API Token

在 New API 控制台创建普通 API Token，并确保该 Token 所属分组能够访问 `grok-imagine-video-1.5`。

```bash
export NEW_API_KEY='sk-your-new-api-token'
```

所有任务接口都使用 Bearer Token：

```http
Authorization: Bearer sk-your-new-api-token
```

不要把 New API Token 写入前端持久化、日志、示例仓库或发送给非 New API 域名。

### 3.3 渠道和模型

管理员需要在 New API 中配置一个能够处理 `/v1/videos` 的上游渠道，并让下游模型名 `grok-imagine-video-1.5` 能够路由到该渠道。

如果上游使用不同模型名，请在渠道模型映射中完成转换。客户端仍应提交：

```json
{
  "model": "grok-imagine-video-1.5"
}
```

可通过模型列表接口检查当前 Token 是否能看到该模型：

```bash
curl "$NEW_API_BASE_URL/v1/models" \
  -H "Authorization: Bearer $NEW_API_KEY"
```

## 4. 认证和公共请求头

创建任务：

```http
POST /v1/videos
Authorization: Bearer <NEW_API_KEY>
Content-Type: application/json
```

查询任务：

```http
GET /v1/videos/{task_id}
Authorization: Bearer <NEW_API_KEY>
```

下载视频：

```http
GET /v1/videos/{task_id}/content
Authorization: Bearer <NEW_API_KEY>
```

## 5. 公共请求字段

| 字段 | 类型 | 必需 | 约束 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | 固定 `grok-imagine-video-1.5` | 下游模型名 |
| `prompt` | string | 是 | 去除空白后不可为空 | 视频生成提示词 |
| `duration` | integer | 是 | 见模式规则 | 视频时长，单位秒 |
| `resolution` | string | 是 | 固定 `720p` | 当前不支持其他分辨率 |
| `aspect_ratio` | string | 是 | `16:9` 或 `9:16` | 视频画幅 |
| `image` | object | 否 | 仅首帧模式 | `{ "b64": "data:image/...;base64,..." }` |
| `reference_images` | array | 否 | 仅参考图模式，最多 6 个 | 每项为 `{ "b64": "..." }` |
| `reference_audios` | array | 否 | 仅参考图模式，最多 3 个 | 每项为 `{ "b64": "..." }` |

不要同时发送 `image` 和 `reference_images`。

## 6. Base64 媒体格式

前端和示例代码统一使用完整 Data URL，不使用裸 Base64：

图片：

```text
data:image/png;base64,iVBORw0KGgoAAA...
```

音频：

```text
data:audio/mpeg;base64,SUQzBAAAAAA...
```

结构必须为：

```text
data:<MIME 类型>;base64,<Base64 数据>
```

GrokAI 任务适配器会校验：

- `image.b64` 和 `reference_images[].b64` 必须以 `data:image/` 开头。
- `reference_audios[].b64` 必须以 `data:audio/` 开头。
- Data URL 必须包含 `;base64,` 且后面存在数据。
- `reference_images` 最多 6 项。
- `reference_audios` 最多 3 项。

当前 Grok 前端会压缩超限图片，并按 Data URL 的实际序列化大小计算 100 MB 总素材上限。配套 JS 示例不执行图片压缩，只负责将原文件编码成 Data URL；调用方应自行控制文件大小和 New API 部署的请求体上限。

## 7. 纯文生视频

两种前端模式都允许不上传素材。此时不要发送 `image`、`reference_images` 或 `reference_audios`。

首帧模式下的纯文生视频可以使用 `6 / 12 / 18 / 24 / 30` 秒：

```bash
curl -X POST "$NEW_API_BASE_URL/v1/videos" \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "grok-imagine-video-1.5",
    "prompt": "A cinematic sunrise over a quiet futuristic city",
    "duration": 12,
    "resolution": "720p",
    "aspect_ratio": "16:9"
  }'
```

参考图模式下不上传素材时，仍固定发送 `duration: 6`：

```json
{
  "model": "grok-imagine-video-1.5",
  "prompt": "A paper boat crossing a glowing underground river",
  "duration": 6,
  "resolution": "720p",
  "aspect_ratio": "16:9"
}
```

服务端不会接收前端模式，因此两个纯文本请求只通过 `duration` 和客户端自身状态区分。

## 8. 首帧模式

### 8.1 规则

- `image` 可省略；省略后即为纯文生视频。
- 最多发送一个 `image`。
- 不发送 `reference_images` 或 `reference_audios`。
- `duration` 支持 `6 / 12 / 18 / 24 / 30`，默认 `6`。
- `resolution` 固定为 `720p`。

### 8.2 请求示例

```json
{
  "model": "grok-imagine-video-1.5",
  "prompt": "Bring the still frame to life with subtle natural motion",
  "image": {
    "b64": "data:image/png;base64,<IMAGE_BASE64>"
  },
  "duration": 12,
  "resolution": "720p",
  "aspect_ratio": "16:9"
}
```

### 8.3 curl 示例

先把本地文件转换为单行 Data URL。以下命令依赖系统 `base64`，不同系统的参数可能略有区别：

```bash
IMAGE_B64="$(base64 < ./first-frame.png | tr -d '\n')"

curl -X POST "$NEW_API_BASE_URL/v1/videos" \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"grok-imagine-video-1.5\",
    \"prompt\": \"Bring the still frame to life with subtle natural motion\",
    \"image\": {
      \"b64\": \"data:image/png;base64,$IMAGE_B64\"
    },
    \"duration\": 12,
    \"resolution\": \"720p\",
    \"aspect_ratio\": \"16:9\"
  }"
```

生产代码应使用 JSON 序列化 API 构造请求，不建议手工拼接包含 Base64 的 JSON 字符串。配套 JS 示例使用 `JSON.stringify()`。

## 9. 参考图模式

### 9.1 规则

- `reference_images` 可省略，最多 6 张图片。
- `reference_audios` 可省略，最多 3 个音频。
- 支持只有图片、只有音频、图片和音频同时存在，或者完全不传素材。
- 不发送 `image`。
- `duration` 固定为 `6`，不能使用其他值。
- `resolution` 固定为 `720p`。

### 9.2 请求示例

```json
{
  "model": "grok-imagine-video-1.5",
  "prompt": "Use the subject and scene references, synchronized with the reference voice",
  "reference_images": [
    { "b64": "data:image/png;base64,<IMAGE_1_BASE64>" },
    { "b64": "data:image/jpeg;base64,<IMAGE_2_BASE64>" }
  ],
  "reference_audios": [
    { "b64": "data:audio/mpeg;base64,<AUDIO_1_BASE64>" }
  ],
  "duration": 6,
  "resolution": "720p",
  "aspect_ratio": "16:9"
}
```

### 9.3 curl 示例

```bash
IMAGE_1_B64="$(base64 < ./subject.png | tr -d '\n')"
IMAGE_2_B64="$(base64 < ./scene.jpg | tr -d '\n')"
AUDIO_1_B64="$(base64 < ./voice.mp3 | tr -d '\n')"

curl -X POST "$NEW_API_BASE_URL/v1/videos" \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"grok-imagine-video-1.5\",
    \"prompt\": \"Use the subject and scene references, synchronized with the reference voice\",
    \"reference_images\": [
      { \"b64\": \"data:image/png;base64,$IMAGE_1_B64\" },
      { \"b64\": \"data:image/jpeg;base64,$IMAGE_2_B64\" }
    ],
    \"reference_audios\": [
      { \"b64\": \"data:audio/mpeg;base64,$AUDIO_1_B64\" }
    ],
    \"duration\": 6,
    \"resolution\": \"720p\",
    \"aspect_ratio\": \"16:9\"
  }"
```

## 10. 创建任务响应

创建成功后，客户端应兼容 `id` 和 `task_id`。优先读取 `id`：

```json
{
  "id": "task_01abc123",
  "task_id": "task_01abc123",
  "object": "video",
  "model": "grok-imagine-video-1.5",
  "status": "queued",
  "progress": 0,
  "created_at": 1788159000
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | New API 公开任务 ID，优先使用 |
| `task_id` | string | 兼容任务 ID |
| `object` | string | 通常为 `video` |
| `model` | string | 下游请求模型名 |
| `status` | string | 当前任务状态 |
| `progress` | number | 0 至 100；部分上游可能返回 0 至 1，客户端可兼容归一化 |
| `created_at` | number | Unix 秒级时间戳 |

不要使用或记录上游真实任务 ID。New API 返回的公开任务 ID用于后续查询和下载。

## 11. 查询任务状态

### 11.1 请求

```bash
TASK_ID='task_01abc123'

curl "$NEW_API_BASE_URL/v1/videos/$TASK_ID" \
  -H "Authorization: Bearer $NEW_API_KEY"
```

### 11.2 状态值

New API 的 OpenAI 兼容视频响应主要使用：

| 状态 | 说明 | 是否终态 |
| --- | --- | --- |
| `queued` | 已提交或排队中 | 否 |
| `in_progress` | 正在生成 | 否 |
| `completed` | 已完成 | 是 |
| `failed` | 已失败 | 是 |

为了兼容不同上游，客户端也可以识别：

- 成功：`completed`、`succeeded`、`success`
- 处理中：`queued`、`pending`、`in_progress`、`processing`、`running`
- 失败/取消：`failed`、`canceled`、`cancelled`

### 11.3 处理中响应

```json
{
  "id": "task_01abc123",
  "task_id": "task_01abc123",
  "object": "video",
  "model": "grok-imagine-video-1.5",
  "status": "in_progress",
  "progress": 45,
  "created_at": 1788159000
}
```

### 11.4 完成响应

```json
{
  "id": "task_01abc123",
  "task_id": "task_01abc123",
  "object": "video",
  "model": "grok-imagine-video-1.5",
  "status": "completed",
  "progress": 100,
  "created_at": 1788159000,
  "completed_at": 1788159300,
  "metadata": {
    "url": "https://upstream.example/video.mp4",
    "remote_url": "https://upstream.example/video.mp4"
  }
}
```

不同上游也可能返回顶层 `output_url`。当前 Grok 前端按以下顺序解析结果地址：

1. `output_url`
2. `metadata.url`
3. `metadata.remote_url`

API 客户端不需要直接访问这些地址，优先使用 New API 的 `/content` 代理下载可以避免把 New API Token 发送到外部域名。

### 11.5 失败响应

```json
{
  "id": "task_01abc123",
  "object": "video",
  "model": "grok-imagine-video-1.5",
  "status": "failed",
  "progress": 100,
  "error": {
    "message": "Generation failed",
    "code": "generation_failed"
  }
}
```

## 12. 下载视频

任务完成后调用：

```bash
curl "$NEW_API_BASE_URL/v1/videos/$TASK_ID/content" \
  -H "Authorization: Bearer $NEW_API_KEY" \
  --output grok-result.mp4
```

成功响应通常为：

```http
HTTP/1.1 200 OK
Content-Type: video/mp4
Cache-Control: public, max-age=86400
```

`/content` 会根据任务所属渠道获取最终视频。任务未完成、任务不属于当前 Token 用户、上游结果地址不可用或被 SSRF 策略阻止时，会返回 OpenAI 风格错误。

## 13. 错误格式与排查

常见错误响应：

```json
{
  "error": {
    "message": "duration must be one of 6, 12, 18, 24, or 30 seconds",
    "type": "invalid_request_error",
    "code": "invalid_request"
  }
}
```

实际错误结构可能包含 `error.message`、顶层 `message` 或字符串 `error`。客户端应优先显示 `error.message`。

常见问题：

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `prompt is required` | 提示词为空 | 传入非空 `prompt` |
| `image and reference_images cannot be used together` | 混用了两种模式字段 | 只保留当前模式的字段 |
| `image.b64 must be an image Data URL` | 图片不是完整 Data URL | 添加正确 MIME 前缀和 `;base64,` |
| `reference_images supports at most 6 items` | 参考图超过 6 张 | 减少图片数量 |
| `reference_audios supports at most 3 items` | 音频超过 3 个 | 减少音频数量 |
| `duration must be one of ...` | 首帧时长不受支持 | 使用 `6/12/18/24/30` |
| `reference mode duration must be 6 seconds` | 参考模式时长不是 6 | 固定使用 `6` |
| `Task not found` | 任务 ID 错误或不属于当前用户 | 使用创建接口返回的公开任务 ID 和同一用户 Token |
| `Task is not completed yet` | 任务还未完成就下载 | 继续轮询至 `completed` |
| `request blocked` | 结果 URL 被 SSRF 配置阻止 | 检查渠道 Base URL 和系统 Fetch/SSRF 设置 |

## 14. JavaScript 示例

### 14.1 环境要求

- Node.js 18 或更高版本。
- 不需要安装 npm 依赖。
- 本地文件由脚本读取并转换成 Data URL Base64。

### 14.2 查看帮助

```bash
node docs/Grok/grok-video-example.mjs --help
```

### 14.3 首帧模式

```bash
NEW_API_BASE_URL='https://geekapis.com' \
NEW_API_KEY='sk-your-new-api-token' \
node docs/Grok/grok-video-example.mjs first-frame \
  --prompt 'Bring the still frame to life with subtle natural motion' \
  --image './first-frame.png' \
  --duration 12 \
  --aspect-ratio '16:9' \
  --output './grok-first-frame.mp4'
```

首帧纯文生视频：

```bash
NEW_API_BASE_URL='https://geekapis.com' \
NEW_API_KEY='sk-your-new-api-token' \
node docs/Grok/grok-video-example.mjs first-frame \
  --prompt 'Clouds flowing rapidly above a mountain valley' \
  --duration 24 \
  --aspect-ratio '16:9'
```

### 14.4 参考图模式

`--image` 和 `--audio` 可以重复：

```bash
NEW_API_BASE_URL='https://geekapis.com' \
NEW_API_KEY='sk-your-new-api-token' \
node docs/Grok/grok-video-example.mjs reference \
  --prompt 'Use the same person and room, synchronized with the voice' \
  --image './subject.png' \
  --image './room.jpg' \
  --audio './voice.mp3' \
  --aspect-ratio '9:16' \
  --output './grok-reference.mp4'
```

参考图模式的 `duration` 固定为 `6`。可以显式传 `--duration 6`，但传入其他值会在发送前报错。

### 14.5 Dry run

使用 `--dry-run` 只构造并检查请求，不调用接口，也不会产生生成费用：

```bash
NEW_API_BASE_URL='https://geekapis.com' \
NEW_API_KEY='sk-your-new-api-token' \
node docs/Grok/grok-video-example.mjs reference \
  --prompt 'Preview request only' \
  --image './subject.png' \
  --audio './voice.mp3' \
  --dry-run
```

脚本日志会隐藏 API Token 和 Base64 正文，只显示媒体类型、文件大小和 Base64 字符数。

## 15. 接入检查清单

- [ ] Token 能从 `GET /v1/models` 看到或调用 `grok-imagine-video-1.5`。
- [ ] 所选渠道支持 `/v1/videos` 创建、查询和内容下载。
- [ ] 请求固定使用 `resolution: "720p"`。
- [ ] `aspect_ratio` 只使用 `16:9` 或 `9:16`。
- [ ] 首帧模式只发送 0 或 1 个 `image`。
- [ ] 首帧模式时长只使用 `6/12/18/24/30`。
- [ ] 参考图模式不发送 `image`。
- [ ] 参考图不超过 6 张，参考音频不超过 3 个。
- [ ] 参考图模式固定发送 `duration: 6`。
- [ ] 所有媒体使用带 MIME 的 Data URL Base64。
- [ ] 轮询间隔不低于约 10 秒。
- [ ] 使用创建响应中的公开 `id` 或 `task_id` 查询任务。
- [ ] 任务完成后通过 `/v1/videos/{task_id}/content` 下载。
- [ ] 不在日志中打印完整 Token 或 Base64 数据。
