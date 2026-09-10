# MiniMax H3 视频 API 接入文档

文档版本：`1.0`  
更新时间：`2026-08-27`  
适用对象：需要在服务端接入 MiniMax H3 视频生成能力的开发者

> 本文只描述对客户开放的公共 API 契约。示例中的密钥、任务 ID 和素材 URL 均为占位值。

## 1. 能力结论

MiniMax H3 支持以下生成方式：

- 文生视频；
- 单张首帧图生视频；
- 首帧 + 尾帧视频生成；
- 多张参考图生成；
- 参考视频生成，最多 3 条；
- 图片、视频、音频混合参考生成。

需要特别注意：

1. **首尾帧模式**与**普通参考素材模式**不能混用。
2. 参考图片最多 9 张、参考视频最多 3 条、参考音频最多 3 条，三类普通参考素材合计最多 12 个。
3. 参考音频不能单独使用，必须同时提供参考图片或参考视频。
4. H3 只接受 JSON 请求；素材通过公网 HTTPS URL 提供，不接受本地文件路径。
5. 视频任务为异步任务：创建任务 → 保存任务 ID → 查询状态 → 下载成片。

## 2. 基础信息

| 项目 | 值 |
|---|---|
| API Base URL | `https://YOUR_BASE_URL` |
| 鉴权方式 | `Authorization: Bearer YOUR_API_KEY` |
| 创建请求格式 | `application/json; charset=utf-8` |
| 创建任务 | `POST /v1/videos` |
| 查询任务 | `GET /v1/videos/{task_id}` |
| 下载成片 | `GET /v1/videos/{task_id}/content` |
| 查询模型 | `GET /v1/models` |

所有 API Key 都必须仅保存在接入方服务端。不得把 API Key 写入网页源码、App 安装包、公开仓库、截图或业务日志。
规则：

- 画质档位由 `model` 决定。新接入不要再发送 `resolution` 或 `size` 来切换画质。
- `MiniMax-H3-720P` 是兼容模型名，实际输出为 768P 档；精确宽高由画面比例决定。
- 2K 也表示画质档位，不保证所有比例都固定为 `2560×1440`；客户端应读取下载后视频的真实媒体信息。
- 两档均按任务固定计费。在 5～15 秒范围内，不会再按秒重复乘价。
- 查询任务和下载成片不重复收费。
- 模型可见性、权限和最新价格以当前账户的模型列表及控制台展示为准。

## 4. 规范用语

本文中的关键词含义如下：

- **必须（MUST）**：不满足时请求会被拒绝，或可能造成重复付费任务。
- **应该（SHOULD）**：强烈建议遵守，以提高兼容性和可靠性。
- **可以（MAY）**：可选能力。

## 5. 鉴权与模型检查

### 5.1 鉴权头

```http
Authorization: Bearer YOUR_API_KEY
```

### 5.2 查询当前密钥可用模型

```bash
curl -sS "https://YOUR_BASE_URL/v1/models" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

只有返回结果中包含目标模型 ID，才表示当前 API Key 已获得相应权限。模型 ID 区分产品档位，建议严格使用本文列出的大小写形式。

## 6. 创建视频任务

### 6.1 请求

```http
POST /v1/videos HTTP/1.1
Host: YOUR_BASE_URL
Authorization: Bearer YOUR_API_KEY
Content-Type: application/json
Idempotency-Key: YOUR_UNIQUE_BUSINESS_REQUEST_ID
```

### 6.2 请求头

| 请求头 | 必填 | 约束 | 说明 |
|---|---:|---|---|
| `Authorization` | 是 | `Bearer <API_KEY>` | API 鉴权 |
| `Content-Type` | 是 | `application/json` | H3 不接受 multipart 请求 |
| `Idempotency-Key` | 强烈建议视为必填 | 1～200 字符，不得含控制字符 | 防止超时重试产生重复任务 |

### 6.3 顶层请求字段

| 字段 | 类型 | 必填 | 约束与行为 |
|---|---|---:|---|
| `model` | string | 是 | `MiniMax-H3-720P` 或 `MiniMax-H3-2K` |
| `prompt` | string | 是 | 非空，最长 60,000 字符 |
| `duration` | integer | 是 | `5`～`15`，必须是整数 |
| `ratio` | string | 否 | 见“画面比例”；文生视频默认 `16:9` |
| `content` | array | 否 | 结构化参考素材；新接入推荐使用 |
| `callback_url` | string | 否 | 终态回调地址，必须是公网 HTTPS 443 地址 |
| `callback_secret` | string | 否 | 回调验签共享密钥，最长 512 字符 |

未列出的字段不属于本 H3 公共契约。接入方不应依赖 `seed`、`negative_prompt`、`generate_audio`、`resolution`、`size` 等字段改变结果。

### 6.4 `content[]` 结构

新接入应把参考素材写入 `content[]`，每个元素为一个对象。

| `type` | `role` | URL 字段 | 用途 |
|---|---|---|---|
| `image_url` | `first_frame` | `image_url.url` | 首帧图生视频 |
| `image_url` | `last_frame` | `image_url.url` | 尾帧；与首帧配合使用 |
| `image_url` | `reference_image` | `image_url.url` | 普通参考图片 |
| `video_url` | `reference_video` | `video_url.url` | 参考动作、运镜或场景视频 |
| `audio_url` | `reference_audio` | `audio_url.url` | 参考音频；必须配合视觉素材 |

图片元素示例：

```json
{
  "type": "image_url",
  "role": "reference_image",
  "image_url": {
    "url": "https://assets.example.com/reference.jpg"
  }
}
```

视频元素示例：

```json
{
  "type": "video_url",
  "role": "reference_video",
  "video_url": {
    "url": "https://assets.example.com/motion.mp4"
  }
}
```

音频元素示例：

```json
{
  "type": "audio_url",
  "role": "reference_audio",
  "audio_url": {
    "url": "https://assets.example.com/music.mp3"
  }
}
```

严格规则：

- `type`、`role` 和对应的 URL 字段必须匹配。
- `content[]` 中只放参考素材；提示词统一放在顶层 `prompt`，避免重复文本。
- 同一素材不要同时出现在 `content[]` 与旧版简写字段中。
- 一个请求只能选择“首尾帧模式”或“普通参考模式”之一。

## 7. 画面比例

| `ratio` | 说明 |
|---|---|
| `16:9` | 横屏，文生视频默认值 |
| `4:3` | 横向标准比例 |
| `1:1` | 方形 |
| `3:4` | 竖向标准比例 |
| `9:16` | 竖屏 |
| `adaptive` | 根据参考素材自适应 |
| `auto` | `adaptive` 的兼容写法 |

`21:9` 当前不受支持，请勿提交。需要横屏时应使用 `16:9` 或 `4:3`；提交 `21:9` 会导致任务失败，并可能返回 `unsupported_ratio`。

行为说明：

- 文生视频未提供 `ratio` 时，默认使用 `16:9`。
- 普通参考模式未提供 `ratio`，或提供 `auto` / `adaptive` 时，模型按参考素材自适应。
- 首帧或首尾帧模式固定按素材自适应；即使请求中填写其他比例，也不应依赖该比例覆盖帧图片比例。

## 8. 参考素材限制

### 8.1 数量限制

| 素材 | 最大数量 |
|---|---:|
| 普通参考图片 | 9 张 |
| 参考视频 | 3 条 |
| 参考音频 | 3 条 |
| 普通参考素材总数 | 12 个 |
| 首帧 | 1 张 |
| 尾帧 | 1 张 |

首帧和尾帧属于帧控制模式，不能与普通参考图片、参考视频或参考音频混合。

### 8.2 URL 硬性要求

每个素材 URL 必须：

- 使用 `https://`；
- 指向公网可访问资源；
- 不使用 `localhost`、环回地址、局域网地址或私有网络域名；
- 不包含 URL 用户名或密码；
- 无需 Cookie、登录页面或额外请求头即可读取；
- 在任务提交和模型读取期间保持有效。

推荐使用稳定的对象存储直链。临时签名 URL 的有效期应该覆盖排队、提交和素材读取时间。

### 8.3 推荐媒体格式

为减少解码失败，建议：

- 图片：JPEG、PNG 或 WebP；
- 视频：MP4 容器，H.264 视频编码；
- 音频：MP3、WAV 或 M4A。

本站在创建时只校验 URL 与数量，不会在创建阶段完整解码全部素材。格式、编码或文件损坏问题可能在异步生成阶段返回失败。

## 9. 各模式完整请求示例

以下示例均可直接替换 API Key、幂等键和素材 URL 后使用。

### 9.1 文生视频：720P 兼容型号（实际 768P）

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-text-001" \
  -d '{
    "model": "MiniMax-H3-720P",
    "prompt": "一艘白色帆船驶过平静海面，日落金色光线，镜头缓慢向前推进，写实电影质感，无文字，无水印",
    "duration": 7,
    "ratio": "16:9"
  }'
```

### 9.2 单图首帧图生视频：2K 竖屏

单张图片的新接入应显式使用 `first_frame`，不要把单图写成含糊的普通参考图。

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-first-frame-001" \
  -d '{
    "model": "MiniMax-H3-2K",
    "prompt": "保持首帧人物身份、五官、发型和服装一致，人物自然向前走两步，镜头平稳后退，动作连续，画面清晰，无文字，无水印",
    "duration": 7,
    "ratio": "adaptive",
    "content": [
      {
        "type": "image_url",
        "role": "first_frame",
        "image_url": {
          "url": "https://assets.example.com/first-frame.jpg"
        }
      }
    ]
  }'
```

### 9.3 首帧 + 尾帧：2K

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-frames-001" \
  -d '{
    "model": "MiniMax-H3-2K",
    "prompt": "从首帧自然过渡到尾帧，人物身份、服装和场景保持一致，动作连贯，镜头缓慢推进，不闪烁，不变形，无文字，无水印",
    "duration": 10,
    "content": [
      {
        "type": "image_url",
        "role": "first_frame",
        "image_url": {
          "url": "https://assets.example.com/first.jpg"
        }
      },
      {
        "type": "image_url",
        "role": "last_frame",
        "image_url": {
          "url": "https://assets.example.com/last.jpg"
        }
      }
    ]
  }'
```

本模式不能再加入 `reference_image`、`reference_video` 或 `reference_audio`。

### 9.4 多张参考图：2K

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-images-001" \
  -d '{
    "model": "MiniMax-H3-2K",
    "prompt": "综合参考图片保持同一人物的脸部、发型、服装和配色一致。人物在摄影棚中缓慢转身，镜头平稳环绕，动作自然，无文字，无水印",
    "duration": 7,
    "ratio": "9:16",
    "content": [
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://assets.example.com/character-front.jpg"
        }
      },
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://assets.example.com/character-side.jpg"
        }
      }
    ]
  }'
```

当前兼容行为会把“仅一张普通参考图、且没有其他参考视频或音频”的请求按单图首帧模式处理。因此只有一张图片时，应直接使用 `role: first_frame`；需要多参考图语义时，应提供至少两张 `reference_image`，或与参考视频一起使用。

### 9.5 参考视频：720P 兼容型号

H3 可以参考视频中的动作、节奏、镜头或场景。提示词应明确哪些元素来自参考视频、哪些元素需要保留或修改。

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-video-001" \
  -d '{
    "model": "MiniMax-H3-720P",
    "prompt": "参考视频中的舞蹈动作、节奏和镜头运动，生成同样连贯的竖屏舞蹈视频。保持人体结构稳定，动作自然，不复制多余人物，无文字，无水印",
    "duration": 7,
    "ratio": "adaptive",
    "content": [
      {
        "type": "video_url",
        "role": "reference_video",
        "video_url": {
          "url": "https://assets.example.com/dance-reference.mp4"
        }
      }
    ]
  }'
```

### 9.6 参考角色图片 + 参考动作视频：2K

这是人物替换、角色迁移等场景的推荐写法。

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-image-video-001" \
  -d '{
    "model": "MiniMax-H3-2K",
    "prompt": "用参考图片中的人物替换参考视频中的主要人物。严格保持参考图片人物的五官、发型、服装和体型；完整参考视频中的舞蹈动作、节奏、运镜和构图。只保留一个主要人物，动作连贯，无文字，无水印",
    "duration": 7,
    "ratio": "9:16",
    "content": [
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://assets.example.com/character.jpg"
        }
      },
      {
        "type": "video_url",
        "role": "reference_video",
        "video_url": {
          "url": "https://assets.example.com/dance.mp4"
        }
      }
    ]
  }'
```

### 9.7 图片 + 视频 + 音频混合参考

```bash
curl -sS -X POST "https://YOUR_BASE_URL/v1/videos" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260827-multimodal-001" \
  -d '{
    "model": "MiniMax-H3-2K",
    "prompt": "保持参考图片中的人物身份，参考视频中的动作和运镜，并使动作节奏与参考音频协调。人物稳定、动作自然、镜头连贯，无文字，无水印",
    "duration": 10,
    "ratio": "adaptive",
    "content": [
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://assets.example.com/character.jpg"
        }
      },
      {
        "type": "video_url",
        "role": "reference_video",
        "video_url": {
          "url": "https://assets.example.com/motion.mp4"
        }
      },
      {
        "type": "audio_url",
        "role": "reference_audio",
        "audio_url": {
          "url": "https://assets.example.com/rhythm.mp3"
        }
      }
    ]
  }'
```

## 10. 创建响应

创建接口成功受理时返回 HTTP `200`，示例：

```json
{
  "id": "vjob_0123456789abcdef0123456789abcdef",
  "status": "queued",
  "model": "MiniMax-H3-2K",
  "progress": 0,
  "created_at": 1787800000
}
```

客户端必须：

1. 立即持久化 `id`；兼容旧客户端时也可以回退读取 `task_id`。
2. 不把“HTTP 200”直接等同于“任务创建成功”。
3. 同时检查响应中存在非空任务 ID，且任务状态不是失败态。
4. 后续只使用该公共任务 ID 查询和下载，不使用其他来源的任务号。

异步校验失败可能表现为 HTTP `200` 加失败状态，例如：

```json
{
  "status": "failed",
  "model": "MiniMax-H3-2K",
  "progress": 100,
  "created_at": 1787800000,
  "error": {
    "code": "unsupported_ratio",
    "message": "当前模型不支持请求的画幅比例。"
  }
}
```

这种响应属于失败，不能进入正常轮询，也不能换一个新的幂等键盲目重提。

## 11. 查询任务状态

### 11.1 请求

```bash
curl -sS "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

建议每 5～10 秒查询一次。2K 任务可能运行数分钟，不能因为单次查询超时或长时间处理中就创建新任务。

### 11.2 客户端状态归一化

创建和查询阶段可能使用不同的状态名称，接入方应按下表归一化：

| 原始状态 | 业务状态 | 处理方式 |
|---|---|---|
| `queued`、`pending`、`submitted`、`created` | 排队中 | 继续查询 |
| `in_progress`、`processing`、`running`、`generating` | 生成中 | 继续查询 |
| `completed`、`succeeded`、`success`、`done` | 成功 | 下载成片 |
| `failed`、`failure`、`error` | 失败 | 停止查询，记录错误 |
| `cancelled`、`canceled` | 失败终态 | 停止查询 |
| 其他或空值 | 未知 | 保留任务 ID，降低频率后重查或联系支持 |

部分兼容响应会把任务字段放在 `task` 或 `data` 对象内。建议使用以下解析规则：

```javascript
function normalizeVideoTask(response) {
  const node = response?.task ?? response?.data ?? response ?? {};
  const id = response?.id ?? response?.task_id ?? node?.id ?? node?.task_id ?? "";
  const rawStatus = String(node?.status ?? response?.status ?? "").toLowerCase();

  const queued = new Set(["queued", "pending", "submitted", "created"]);
  const running = new Set(["in_progress", "processing", "running", "generating"]);
  const succeeded = new Set(["completed", "succeeded", "success", "done"]);
  const failed = new Set(["failed", "failure", "error", "cancelled", "canceled"]);

  let state = "unknown";
  if (queued.has(rawStatus)) state = "queued";
  if (running.has(rawStatus)) state = "running";
  if (succeeded.has(rawStatus)) state = "succeeded";
  if (failed.has(rawStatus)) state = "failed";

  return {
    id,
    state,
    rawStatus,
    progress: node?.progress ?? response?.progress,
    error: node?.error ?? response?.error,
    raw: response
  };
}
```

### 11.3 成功响应示例

```json
{
  "id": "vjob_0123456789abcdef0123456789abcdef",
  "model": "MiniMax-H3-2K",
  "status": "completed",
  "progress": 100,
  "content": {
    "video_url": "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef/content",
    "requires_auth": true
  }
}
```

不要依赖响应中的临时下载 URL 长期有效。最稳定的下载方式始终是使用保存的任务 ID 请求公共内容接口。

## 12. 下载成片

### 12.1 完整下载

```bash
curl -fL "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef/content" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -o "video.mp4"
```

### 12.2 Range 与断点续传

内容接口支持标准 HTTP Range：

```bash
curl -fL \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Range: bytes=0-1023" \
  "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef/content" \
  -o "video-head.bin"
```

使用 curl 断点续传：

```bash
curl -fL -C - \
  -H "Authorization: Bearer YOUR_API_KEY" \
  "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef/content" \
  -o "video.mp4"
```

### 12.3 网页端交付

平台 API Key 不应暴露在浏览器中。网页端应请求接入方自己的后端，由后端携带平台 API Key 下载或代理视频，再向浏览器返回受控地址。

任务状态已成功但下载暂时失败时，应重试同一任务的内容接口，不能重新创建视频。

## 13. 幂等与安全重试

### 13.1 幂等键规则

每个业务视频订单必须生成一个稳定且唯一的 `Idempotency-Key`：

```http
Idempotency-Key: 7f420431-2825-4df7-b53b-13a762e6e6ac
```

硬性规则：

1. 同一业务订单的首次请求和所有创建重试必须使用相同 Key。
2. 重试时请求体必须保持一致，包括模型、提示词、素材、回调地址和回调密钥。
3. 新业务订单必须使用新 Key。
4. Key 最长 200 字符，不得包含控制字符。
5. 同一个 Key 配合不同请求体会返回 HTTP `409`，错误码 `idempotency_conflict`。
6. 显式幂等记录当前保留 24 小时。

### 13.2 重试决策

| 情况 | 正确处理 |
|---|---|
| 已获得任务 ID | 只查询该任务，不再调用创建接口 |
| 创建连接中断或超时，未获得任务 ID | 使用相同 Key 和完全相同请求体重试 |
| HTTP 429 | 遵循 `Retry-After`，使用相同 Key 退避重试 |
| HTTP 5xx | 使用相同 Key 指数退避重试 |
| HTTP 400 | 修正参数后作为新业务请求使用新 Key |
| HTTP 401 / 403 | 修复鉴权、权限或余额问题，不盲目重试 |
| HTTP 409 | 不更换 Key 规避冲突；先核对原请求体与业务订单 |
| 查询或下载超时 | 使用原任务 ID 重试查询或下载，不重新创建 |

推荐退避间隔：`1s → 5s → 15s → 60s`，并增加少量随机抖动。

## 14. 终态回调（可选）

### 14.1 创建时登记回调

```json
{
  "model": "MiniMax-H3-2K",
  "prompt": "镜头缓慢推进，夕阳下的城市天际线，无文字，无水印",
  "duration": 7,
  "ratio": "16:9",
  "callback_url": "https://api.example.com/hooks/video",
  "callback_secret": "YOUR_CALLBACK_SHARED_SECRET"
}
```

`callback_url` 必须满足：

- 公网 HTTPS；
- 只使用 443 端口；
- 不使用内网、环回、链路本地或保留地址；
- 不带 URL 用户名、密码或片段；
- 接收端在 10 秒内返回 HTTP 2xx。

回调仅用于终态通知。接入方仍应保留轮询作为兜底和状态来源。

### 14.2 回调状态

回调体中的终态为：

- `succeeded`：生成成功；
- `failed`：生成失败；
- `cancelled`：取消或等价失败终态。

成功回调示例：

```json
{
  "id": "vjob_0123456789abcdef0123456789abcdef",
  "status": "succeeded",
  "model": "MiniMax-H3-2K",
  "progress": 100,
  "created_at": 1787800000,
  "updated_at": 1787800600,
  "content": {
    "video_url": "https://YOUR_BASE_URL/v1/videos/vjob_0123456789abcdef0123456789abcdef/content",
    "requires_auth": true
  }
}
```

### 14.3 回调请求头

```text
X-Callback-Request-Id: <稳定投递 ID>
X-Callback-Timestamp: <Unix 秒>
X-Callback-Event: video.task.<status>
X-Callback-Signature-Version: v1
X-Callback-Signature: <hex HMAC-SHA256>
```

只有提供 `callback_secret` 时，平台才发送签名和签名版本请求头。

签名原文：

```text
request_id + "." + timestamp + "." + raw_body
```

签名算法：

```text
hex(HMAC-SHA256(callback_secret, signing_text))
```

Node.js 验签示例：

```javascript
import crypto from "node:crypto";

export function verifyVideoCallback({ headers, rawBody, secret }) {
  const requestId = String(headers["x-callback-request-id"] ?? "");
  const timestamp = String(headers["x-callback-timestamp"] ?? "");
  const signature = String(headers["x-callback-signature"] ?? "");

  if (!requestId || !timestamp || !signature) return false;

  const expected = crypto
    .createHmac("sha256", secret)
    .update(`${requestId}.${timestamp}.${rawBody}`)
    .digest("hex");

  const left = Buffer.from(expected, "hex");
  const right = Buffer.from(signature, "hex");
  return left.length === right.length && crypto.timingSafeEqual(left, right);
}
```

验签时必须使用未经 JSON 解析和重新序列化的原始请求体。接收端还应：

- 校验时间戳，建议只接受与本机时间相差 5 分钟以内的请求；
- 按 `X-Callback-Request-Id` 幂等处理，避免重复执行业务动作；
- 验签成功并完成持久化后尽快返回 HTTP 2xx；
- 生成成功后仍使用平台 API Key 请求 `/content` 下载成片。

回调失败会退避重试。接收端不得依赖“只投递一次”。

## 15. 错误响应

### 15.1 HTTP 错误信封

```json
{
  "error": {
    "code": "invalid_duration",
    "message": "当前 H3 仅支持 5–15 秒的整数时长。",
    "type": "invalid_request_error",
    "request_id": "gw_0123456789abcdef01234567"
  },
  "code": "invalid_duration",
  "message": "当前 H3 仅支持 5–15 秒的整数时长。",
  "request_id": "gw_0123456789abcdef01234567"
}
```

接入方应记录脱敏后的 `request_id`、HTTP 状态码、错误码、业务订单号和任务 ID，便于排查；不得记录完整 API Key 或带敏感签名的素材 URL。

### 15.2 常见错误码

| HTTP | 错误码 | 含义与处理 |
|---:|---|---|
| 400 | `invalid_json` | JSON 无法解析；修正请求体 |
| 400 | `invalid_prompt` | 提示词为空或超过 60,000 字符 |
| 400 | `invalid_duration` | 时长不是 5～15 的整数 |
| 400 | `unsupported_ratio` | 比例不受支持 |
| 400 | `invalid_reference_url` | 素材不是有效公网 HTTPS URL |
| 400 | `too_many_reference_images` | 普通参考图片超过 9 张 |
| 400 | `too_many_reference_videos` | 参考视频超过 3 条 |
| 400 | `too_many_reference_audios` | 参考音频超过 3 条 |
| 400 | `too_many_reference_media` | 普通参考素材合计超过 12 个 |
| 400 | `incompatible_reference_mode` | 首尾帧与普通参考素材混用 |
| 400 | `audio_reference_requires_visual` | 只提供音频，没有图片或视频 |
| 400 | `invalid_idempotency_key` | 幂等键格式不正确或过长 |
| 400 / 403 | `content_rejected` | 提示词或素材未通过内容安全检查 |
| 401 | `authentication_error` | 缺少或使用了无效 API Key |
| 404 | `task_not_found` | 任务不存在、无权访问或已失效 |
| 409 | `idempotency_conflict` | 相同幂等键对应不同请求体 |
| 429 | `video_task_user_limit_exceeded` | 当前用户已有 30 个未完成视频任务 |
| 429 | `service_busy` | 请求过快或服务繁忙；按 `Retry-After` 退避 |
| 503 | `model_unavailable` | 当前模型暂不可用，或当前密钥未开通该模型 |
| 503 | `queue_unavailable` | 异步排队服务暂不可用；同 Key 重试 |
| 503 | `gateway_unavailable` | 视频服务暂不可用；同 Key 退避重试 |
| 200 | `generation_failed` 或具体错误码 | 异步任务失败；检查响应 `status` 和 `error` |

一个用户最多同时保留 30 个未完成视频任务。超过限制时应等待已有任务进入终态，而不是高频重试。

## 16. Node.js 完整接入示例

以下代码适用于 Node.js 18 及以上版本，演示创建、轮询和下载。

```javascript
import crypto from "node:crypto";
import fs from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const BASE_URL = "https://YOUR_BASE_URL";
const API_KEY = process.env.API_KEY;

if (!API_KEY) throw new Error("缺少环境变量 API_KEY");

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function normalizeVideoTask(response) {
  const node = response?.task ?? response?.data ?? response ?? {};
  const id = response?.id ?? response?.task_id ?? node?.id ?? node?.task_id ?? "";
  const rawStatus = String(node?.status ?? response?.status ?? "").toLowerCase();

  if (["queued", "pending", "submitted", "created"].includes(rawStatus)) {
    return { id, state: "queued", rawStatus, error: node?.error ?? response?.error };
  }
  if (["in_progress", "processing", "running", "generating"].includes(rawStatus)) {
    return { id, state: "running", rawStatus, error: node?.error ?? response?.error };
  }
  if (["completed", "succeeded", "success", "done"].includes(rawStatus)) {
    return { id, state: "succeeded", rawStatus, error: node?.error ?? response?.error };
  }
  if (["failed", "failure", "error", "cancelled", "canceled"].includes(rawStatus)) {
    return { id, state: "failed", rawStatus, error: node?.error ?? response?.error };
  }
  return { id, state: "unknown", rawStatus, error: node?.error ?? response?.error };
}

async function requestJson(url, options = {}) {
  const response = await fetch(url, options);
  const text = await response.text();
  let body;
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    throw new Error(`非 JSON 响应：HTTP ${response.status}`);
  }

  if (!response.ok) {
    const code = body?.error?.code ?? body?.code ?? "http_error";
    const message = body?.error?.message ?? body?.message ?? `HTTP ${response.status}`;
    const error = new Error(`${code}: ${message}`);
    error.status = response.status;
    error.body = body;
    throw error;
  }
  return body;
}

async function createVideo({ idempotencyKey, request }) {
  const body = await requestJson(`${BASE_URL}/v1/videos`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey
    },
    body: JSON.stringify(request)
  });

  const task = normalizeVideoTask(body);
  if (task.state === "failed") {
    throw new Error(`创建失败：${JSON.stringify(task.error ?? body)}`);
  }
  if (!task.id) {
    throw new Error(`创建响应缺少任务 ID：${JSON.stringify(body)}`);
  }
  return task.id;
}

async function waitForVideo(taskId, maxWaitMs = 30 * 60 * 1000) {
  const deadline = Date.now() + maxWaitMs;

  while (Date.now() < deadline) {
    const body = await requestJson(`${BASE_URL}/v1/videos/${encodeURIComponent(taskId)}`, {
      headers: { Authorization: `Bearer ${API_KEY}` }
    });
    const task = normalizeVideoTask(body);

    if (task.state === "succeeded") return;
    if (task.state === "failed") {
      throw new Error(`视频生成失败：${JSON.stringify(task.error ?? body)}`);
    }

    await sleep(task.state === "unknown" ? 15000 : 8000);
  }

  throw new Error(`轮询超时，但任务可能仍在运行，请保留任务 ID：${taskId}`);
}

async function downloadVideo(taskId, outputPath) {
  const response = await fetch(
    `${BASE_URL}/v1/videos/${encodeURIComponent(taskId)}/content`,
    { headers: { Authorization: `Bearer ${API_KEY}` } }
  );

  if (!response.ok || !response.body) {
    throw new Error(`下载失败：HTTP ${response.status}`);
  }

  await pipeline(Readable.fromWeb(response.body), fs.createWriteStream(outputPath));
}

const idempotencyKey = crypto.randomUUID();
const taskId = await createVideo({
  idempotencyKey,
  request: {
    model: "MiniMax-H3-2K",
    prompt: "用参考图片中的人物替换参考视频中的主要人物，保持人物身份和服装一致，参考原视频动作与运镜，无文字，无水印",
    duration: 7,
    ratio: "9:16",
    content: [
      {
        type: "image_url",
        role: "reference_image",
        image_url: { url: "https://assets.example.com/character.jpg" }
      },
      {
        type: "video_url",
        role: "reference_video",
        video_url: { url: "https://assets.example.com/motion.mp4" }
      }
    ]
  }
});

console.log("taskId:", taskId);
await waitForVideo(taskId);
await downloadVideo(taskId, `./${taskId}.mp4`);
console.log("downloaded");
```

生产环境应把 `idempotencyKey`、请求体指纹、任务 ID 和业务订单号持久化到数据库。进程重启后应继续查询原任务，而不是重新创建。

## 17. 旧版简写字段兼容

平台仍兼容以下简写字段，但新项目建议使用 `content[]`：

```json
{
  "model": "MiniMax-H3-2K",
  "prompt": "保持参考素材一致，镜头平稳推进",
  "duration": 7,
  "ratio": "9:16",
  "image_urls": [
    "https://assets.example.com/character-1.jpg",
    "https://assets.example.com/character-2.jpg"
  ],
  "video_urls": [
    "https://assets.example.com/motion.mp4"
  ],
  "audio_urls": [
    "https://assets.example.com/rhythm.mp3"
  ]
}
```

首尾帧简写：

```json
{
  "model": "MiniMax-H3-2K",
  "prompt": "从首帧自然过渡到尾帧",
  "duration": 7,
  "first_frame_url": "https://assets.example.com/first.jpg",
  "last_frame_url": "https://assets.example.com/last.jpg"
}
```

不要把 `first_frame_url` / `last_frame_url` 与 `image_urls`、`video_urls`、`audio_urls` 混用。

## 18. 上线前检查清单

- API Key 仅保存在服务端，并已开通目标模型。
- 已使用 `/v1/models` 验证模型对当前密钥可见。
- 画质只通过 `model` 选择，没有依赖 `resolution` 改档。
- `duration` 是 5～15 的整数。
- 参考素材为可直接读取的公网 HTTPS URL。
- 图片≤9、视频≤3、音频≤3、普通参考素材合计≤12。
- 首尾帧模式没有混入普通参考素材。
- 音频参考同时带有图片或视频。
- 每个业务订单都有稳定且唯一的 `Idempotency-Key`。
- 创建重试使用同一个 Key 和完全相同的请求体。
- 已持久化任务 ID，并正确处理 200 响应中的失败状态。
- 轮询间隔不少于 5 秒，状态解析兼容顶层、`task` 和 `data`。
- 下载使用 `/v1/videos/{task_id}/content`，失败时重试下载而非重新生成。
- 回调接收端使用原始请求体验签，并按投递 ID 幂等。
- 已在业务侧校验下载文件非空、可解码、时长和像素符合预期。

## 19. 常见问题

### H3 能参考视频吗？

可以。每个任务最多 3 条参考视频，使用 `type: video_url`、`role: reference_video`。人物替换场景通常同时提供一张或多张角色参考图。

### H3 支持首尾帧吗？

支持。首帧和尾帧各最多 1 张，分别使用 `role: first_frame` 和 `role: last_frame`。首尾帧不能与普通参考图片、参考视频或参考音频混用。

### 7 秒能生成吗？

可以。当前公共契约支持 5～15 秒的任意整数，包括 7 秒、10 秒和 15 秒。

### 720P 为什么实际是 768P？

`MiniMax-H3-720P` 是为既有客户保留的兼容模型 ID，实际使用 768P 输出档位。无需改请求模型名。

### 可以在请求里把 720P 改成 2K 吗？

不可以。画质由模型 ID 固定决定。需要 2K 时必须使用 `MiniMax-H3-2K`。

### 本地图片或本地视频能直接传吗？

不能。H3 JSON API 要求公网 HTTPS URL。请先上传到接入方可控的对象存储，再提交直链。

### 收到 504 或创建超时怎么办？

如果没有任务 ID，使用原 `Idempotency-Key` 和完全相同的请求体重试；如果已经拿到任务 ID，只查询原任务。不要生成新 Key 直接重提。

### 状态成功但暂时下载不到怎么办？

继续重试原任务的 `/content` 接口。下载故障不等于生成失败，也不应触发重新生成。
