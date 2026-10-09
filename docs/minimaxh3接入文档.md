# H3 视频生成 API 调用文档

本文档面向接入 H3 ComfyUI Admin 的业务调用方，说明如何创建视频任务、查询任务状态、获取生成结果和取消任务。管理后台使用的 `/api/admin/*` 接口不在本文档范围内。

## 1. 接入信息

生产环境示例地址：

```text
https://geekapis.com
```

后续示例使用以下变量：

```bash
BASE_URL="https://geekapis.com"
API_KEY="<API_KEY>"
```

### 1.1 接口总览

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| `POST` | `/v1/videos` | Bearer | 创建异步视频生成任务 |
| `GET` | `/v1/videos/{id}` | Bearer | 查询任务状态和生成结果 |

### 1.2 Bearer 鉴权

除结果文件、健康检查和就绪检查外，请求必须携带：

```http
Authorization: Bearer <API_KEY>
```

API Key 缺失或无效时返回 HTTP `401`：

```json
{
  "error": {
    "code": "unauthorized",
    "message": "A valid Bearer API key is required",
    "retryable": false
  }
}
```

## 2. 创建视频任务

```http
POST /v1/videos
Authorization: Bearer <API_KEY>
Content-Type: application/json
Idempotency-Key: <可选的幂等键>
```

任务创建成功后进入异步队列并返回 HTTP `200 OK`。使用相同 `Idempotency-Key` 再次提交时同样返回 HTTP `200 OK` 和原任务，不会创建新任务。

### 2.1 请求字段

| 字段 | 类型 | 必填  | 说明 |
| --- | --- |-----| --- |
| `model` | string | 是   | 默认 `minimax-h3` |
| `content` | array | 是   | 必须且只能包含一个非空 `text`，可以追加参考素材 |
| `resolution` | string | 是   | `720p` 或 `2k` |
| `ratio` | string | 是   | `16:9` 或 `9:16` |
| `duration` | integer | 是   | `5` 至 `15` 秒，包含边界值 |
| `metadata` | object | 否   | 调用方自定义信息，键和值都必须是字符串 |

请求 JSON 不接受未定义字段。

### 2.2 `content` 类型

| `type` | 数据字段 | `role` | 数量限制 | 数据要求 |
| --- | --- | --- | --- | --- |
| `text` | `text` | 不传 | 必须且只能 1 个 | 非空提示词 |
| `image_url` | `image_url.url` | `reference_image` | 图片合计最多 9 个 | 公网 HTTP/HTTPS URL |
| `image_b64` | `image_b64` | `reference_image` | 图片合计最多 9 个 | 纯 Base64 或 `image/*` Data URL |
| `video_url` | `video_url.url` | `reference_video` | 视频合计最多 3 个 | 公网 HTTP/HTTPS URL |
| `video_b64` | `video_b64` | `reference_video` | 视频合计最多 3 个 | 纯 Base64 或 `video/*` Data URL |
| `audio_url` | `audio_url.url` | `reference_audio` | 音频合计最多 3 个 | 公网 HTTP/HTTPS URL |
| `audio_b64` | `audio_b64` | `reference_audio` | 音频合计最多 3 个 | 纯 Base64 或 `audio/*` Data URL |

URL 和 Base64 可以混用。图片、视频和音频分别按照各自在 `content` 数组中的出现顺序映射到对应参考槽位。

素材 URL 必须能从 Worker 所在服务器公开访问。Base64 会增加约 33% 的请求体积；请合理优化压缩图片，保证请求体大小不超过10MB，大型视频和音频建议使用 URL！！！！

### 2.3 提示词引用

提示词可以使用以下引用标记：

| 标记 | 说明 |
| --- | --- |
| `@图像N` | 引用第 N 张图片 |
| `@图片N` | `@图像N` 的别名 |
| `@视频N` | 引用第 N 个视频 |
| `@音频N` | 引用第 N 个音频 |

例如，`@图像1` 指 `content` 中出现的第一张图片，与视频或音频在数组中的位置无关。
重要!!!!!!:MiniMax-H3的提示词强烈使用官方skill优化，有相应的标准。

官方提示词优化skill示例:
你是 MiniMax H3 全参考模式（full-reference）的提示词改写专家。用户会用自己的话描述想要的视频，你要把它改写成规范的六段式输出。

官方模板主要规定格式（六段顺序、标签、任务类型、保留关系枚举、分镜语法）。你的任务是在不破坏这些格式的前提下，把参考图里真实可见的身份特征与每镜画面写清楚。

请严格只输出一个 JSON 对象（不要 markdown 围栏、不要解释、不要多余字段），字段如下：
{
"subject_definitions": ["每行一条参考内容的定义句"],
"summary": "一段英文，以方括号任务类型前缀开头",
"retention_analysis": ["每行一条参考内容的保留关系"],
"detailed_description": "正文，含风格开场句与 [Shot N] 分镜，换行分隔",
"overall_soundscape": "全片环境音与物理音",
"non_diegetic_music": "只有观众能听到的配乐，没有则写 N/A"
}

写作规范：
- 六段全部用英文书写。只有 <d> 里的台词/歌词，以及画面中可见的文字，保留原语言。
- 主体开口的完整格式是 <Subject N> (Sx) + 动作/语气 + <d>[Language] 台词</d>，三者缺一不可。Language 必须用英文语言全称，与 ref-en.txt 及页面 @ 对白一致，常用：Chinese / English / Korean / Japanese。正确：<Subject 1> (S1) 开口说，<d>[Chinese] 师姐我终于找到你了。</d>；<Subject 2> (S1) turns toward the woman and says, <d>[English] Last summer, I went to my grandfather's house.</d>。禁止只写 <d> 不带 <Subject N> (Sx)；禁止只写主体名不带 (S1)；禁止 zh、en、ja、ko、zh-CN。
- subject_definitions 是身份锁定段：标签含义 + 参考角色 + 需要跟随的主要特征。官方短句（with long dark hair, a blue cardigan, and a thin silver necklace）只是句式，不是质量上限。
- 每一行写清三件事：这个标签是什么、来自哪张图/哪段视频/哪段音频、在成片里起什么作用、要跟随哪些可见特征。
- 人物/生物对照参考图写真实可见、能把该主体认出来的特征，图中有多少写多少，通常 5-8 项：年龄感、发型发色与盘法、五官与妆容（仅写图中可见）、肤色、服装款式/颜色/面料或纹样、配饰、体态与标志性姿态。图里没有的不要编。
- 场景/环境写空间结构、关键陈设与材质、主色、标志物、光线与天气，不要只写「雪景」「房间」。官方完整示例的粒度是：exposed brick wall, orange tufted sofa with patterned pillows, a neon sign, and a wooden coffee table。
- 服装、道具、风格若被单独跟踪，各自成行；仅用于定义人物的图不要单独立 <Picture N>。
- 多素材定义同一主体时合并成一句，并说明各素材分别提供外观、动作还是音色。
- 定义句保持一行一句。动作、运镜、对白、光影变化留给 detailed_description，不要在定义段写成小传或分镜。
- 人物定义句目标粒度（仍保持一行）：<Subject 1> is the young woman in <Picture 1>, with long dark hair falling over one shoulder, a pale complexion, a blue knit cardigan over a white blouse, and a thin silver necklace.
- 场景定义句目标粒度：<Subject 1> is the coffee-shop environment in <Picture 1>, featuring an exposed brick wall, an orange tufted sofa with patterned pillows, a neon sign, and a wooden coffee table.
- summary 只写成片目标：类型、核心事件、时长意向、成片要达成的效果。不要复述素材清单。
- 任务类型按素材实际角色选，不要因为用户附了视频/音频就改成 video editing 或 audio reuse。
- 仅当用户明确要求剪辑、转场、特效、字幕、画中画、变速、混音、配乐替换时，才用 video editing / audio reuse。
- 任务类型只用官方枚举：keyframe completion / reference generation / video editing / video continuation / audio reuse / audio reference。纯文生视频额外允许 [text to video]。不要自造 first-frame-to-video 这类前缀。
- 本段不得引入新的参考标签。
- retention_analysis 只评已定义角色是否保留，不要把新动作、新剧情、新运镜当成保真损失。
- 每个已定义标签写一行：哪些定义特征被保留、哪些被改。特征未改时关系词用 fully_preserved，并点名被保留的特征。
- 可见内容用 fully_preserved / partially_preserved / attribute_transfer / weak_reference；音频用 fully_copy / partially_copy / reference / weak_reference。
- <Subject N> 写成 <Subject 1> (appears in [Shot 1], [Shot 3]): fully_preserved - ...
- <Picture N> 写它担任的角色而不是 appears in，例如 <Picture 2> ([Shot 1] first frame): fully_preserved - ...
- 本段不出现 (Sx) 说话人编号。没有参考素材时按当前模式写一致性要求，不要编造参考标签。
- detailed_description 才是成片正文。官方要求写细，不要压成剧情梗概或参考关系清单。
- 先用一到两句确立整体风格（画质、色调、镜头语言），再按播放顺序分镜。风格开场必须写在 [Shot 1] 之前。
- 每个镜头至少写清七件事：构图/景别、主体外观与在画面中的位置、环境与光线、动作与状态、运镜（类型、幅度、速度）、当前声音、参考内容具体在哪里生效。
- 重要 <Subject N> 第一次清楚出场时，把定义段锁定的外观落到这个镜头的可见画面里（位置 + 当前动作）；之后镜头沿用同一标签，不要重新定义标签含义。
- [Shot 1] 不带时间戳；之后 [Shot N] At MM:SS.mmm, ...，切点必须小于目标时长。
- 有人开口时必须同时写出：主体标签 + 说话人编号 + 对白语言和文字。官方格式：<Subject 1> (S1) … <d>[Chinese] 台词。</d>
- (Sx) 按成片里实际发声顺序从 (S1) 起编，同一发声源全程复用；不要在 retention_analysis 里写 (Sx)。
- 画外音写成 <Subject N> (Sx) off-screen。对不上已定义主体时，用稳定音色描述 + (Sx)，例如 a young male narrator (S2)。
- 只有直接复用的 BGM/完整音轨里的唱词才用 <Audio N> 当声源，不要再编一个 (Sx)。
- 主体开口的完整格式是 <Subject N> (Sx) + 动作/语气 + <d>[Language] 台词</d>，三者缺一不可。Language 必须用英文语言全称，与 ref-en.txt 及页面 @ 对白一致，常用：Chinese / English / Korean / Japanese。正确：<Subject 1> (S1) 开口说，<d>[Chinese] 师姐我终于找到你了。</d>；<Subject 2> (S1) turns toward the woman and says, <d>[English] Last summer, I went to my grandfather's house.</d>。禁止只写 <d> 不带 <Subject N> (Sx)；禁止只写主体名不带 (S1)；禁止 zh、en、ja、ko、zh-CN。
- 生成类任务按 350-500 英文词的信息量来写（中文输出时保持同等细节，不要因为换语言而变短）。对白密集时优先把台词时间线写完整。
- overall_soundscape 只写全片环境音与现场物理声，不要写观众听到的配乐。
- non_diegetic_music 只写观众听到、角色听不到的配乐；有配乐时写乐器、速度、动态，不要只写「轻快」或「悲伤」。没有配乐写 N/A。
- 完整台词只写在 detailed_description 的 <d> 里，不要在声音两段重复整句台词。

### 2.4 分辨率映射

| `resolution` | `ratio` | 基础画布 | 输出尺寸 | 超清 |
| --- | --- | --- | --- | --- |
| `720p` | `16:9` | `1344x768` | `1344x768` | 关闭 |
| `720p` | `9:16` | `768x1344` | `768x1344` | 关闭 |
| `2k` | `16:9` | `1344x768` | `2688x1536` | RTX 2x |
| `2k` | `9:16` | `768x1344` | `1536x2688` | RTX 2x |

### 2.5 完整请求示例

```bash
curl -X POST "$BASE_URL/v1/videos" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-20260817-0001" \
  -d '{
    "model": "minimax-h3",
    "content": [
      {
        "type": "text",
        "text": "参考@图像1的主体和@视频1的镜头节奏，生成一支产品广告片"
      },
      {
        "type": "image_url",
        "image_url": {
          "url": "https://cdn.example.com/reference.png"
        },
        "role": "reference_image"
      },
      {
        "type": "video_url",
        "video_url": {
          "url": "https://cdn.example.com/reference.mp4"
        },
        "role": "reference_video"
      },
      {
        "type": "audio_b64",
        "audio_b64": "data:audio/mpeg;base64,<BASE64_DATA>",
        "role": "reference_audio"
      }
    ],
    "resolution": "2k",
    "ratio": "16:9",
    "duration": 10,
    "metadata": {
      "order_id": "order-20260817-0001",
      "user_id": "user-10001"
    }
  }'
```

### 2.6 创建成功响应

HTTP `200 Accepted`：

```json
{
  "id": "video_abc123",
  "object": "video",
  "model": "minimax-h3",
  "status": "queued",
  "progress": 0,
  "stage": "queued",
  "created_at": 1786924800,
  "created_at_iso": "2026-08-17T00:00:00Z"
}
```

调用方应保存 `id`，用于后续查询和取消任务。

### 2.7 幂等请求

建议每个业务请求生成一个稳定且唯一的 `Idempotency-Key`。网络超时后可以使用同一个 Key 安全重试：

```http
Idempotency-Key: order-20260817-0001
```

相同 Key 会直接返回第一次创建的任务。服务端不会用新请求体替换原任务，因此修改了生成参数后必须使用新的 Key。

## 3. 查询任务状态与结果

```http
GET /v1/videos/{id}
Authorization: Bearer <API_KEY>
```

请求示例：

```bash
VIDEO_ID="video_abc123"

curl "$BASE_URL/v1/videos/$VIDEO_ID" \
  -H "Authorization: Bearer $API_KEY"
```

### 3.1 响应字段

| 字段 | 类型 | 出现条件 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 始终 | 视频任务 ID |
| `object` | string | 始终 | 固定为 `video` |
| `model` | string | 始终 | 使用的模型 |
| `status` | string | 始终 | 任务状态 |
| `progress` | integer | 始终 | `0` 至 `100` 的进度 |
| `stage` | string | 始终 | 当前内部执行阶段 |
| `created_at` | integer | 始终 | Unix 秒级创建时间戳 |
| `created_at_iso` | string | 始终 | ISO 8601 创建时间 |
| `completed_at` | integer | 进入终态后 | Unix 秒级完成时间戳 |
| `output_url` | string | 结果已生成后 | 最终视频访问地址 |
| `error` | object | 失败、取消或等待重试时 | 错误详情 |

`stage` 用于展示执行细节，不是稳定枚举。业务逻辑应以 `status` 判断任务是否完成。

### 3.2 任务状态

| 状态 | 是否终态 | 说明 |
| --- | --- | --- |
| `queued` | 否 | 已进入队列，等待可用实例 |
| `dispatching` | 否 | 正在选择并占用执行实例 |
| `running` | 否 | ComfyUI 正在执行工作流 |
| `retrying` | 否 | 当前尝试失败，等待自动重试 |
| `succeeded` | 是 | 生成成功，可以读取 `output_url` |
| `failed` | 是 | 生成失败，可以读取 `error` |
| `canceled` | 是 | 被调用方取消或排队等待超时 |

### 3.3 处理中响应

```json
{
  "id": "video_abc123",
  "object": "video",
  "model": "minimax-h3",
  "status": "running",
  "progress": 55,
  "stage": "rendering",
  "created_at": 1786924800,
  "created_at_iso": "2026-08-17T00:00:00Z"
}
```

### 3.4 成功响应

```json
{
  "id": "video_abc123",
  "object": "video",
  "model": "minimax-h3",
  "status": "succeeded",
  "progress": 100,
  "stage": "completed",
  "created_at": 1786924800,
  "created_at_iso": "2026-08-17T00:00:00Z",
  "completed_at": 1786925100,
  "output_url": "https://xxx/v1/videos/video_abc123/content"
}
```

### 3.5 失败响应

```json
{
  "id": "video_abc123",
  "object": "video",
  "model": "minimax-h3",
  "status": "failed",
  "progress": 55,
  "stage": "failed",
  "created_at": 1786924800,
  "created_at_iso": "2026-08-17T00:00:00Z",
  "completed_at": 1786925100,
  "error": {
    "task_id": "video_abc123",
    "code": "execution_failed",
    "message": "execution failed",
    "retryable": false
  }
}
```

### 3.6 建议轮询方式

1. 创建任务并保存返回的 `id`。
2. 每 2 至 5 秒查询一次 `/v1/videos/{id}`。
3. `queued`、`dispatching`、`running` 或 `retrying` 时继续轮询。
4. `succeeded` 时使用服务端返回的 `output_url` 获取视频。
5. `failed` 或 `canceled` 时停止轮询并记录 `error`。

不要根据预计生成时长固定等待后直接拼接结果地址，也不要根据 `stage` 判断终态。

## 4. 获取视频结果

优先使用状态查询响应中的 `output_url`，不要自行拼接地址：

```bash
OUTPUT_URL="https://xxxx/v1/videos/video_abc123/content"

curl -L "$OUTPUT_URL" -o result.mp4
```

`-L` 可以兼容 HTTP `302` 跳转。

### 4.1 结果交付方式

| 系统设置 | `output_url` | 访问行为 |
| --- | --- | --- |
| 关闭“使用外部结果存储” | `{H3_PUBLIC_BASE_URL}/v1/videos/{id}/content` | API 从本地共享卷返回视频，支持 Range 请求 |
| 开启“使用外部结果存储” | ComfyUI、S3 或阿里云 OSS 的公开地址 | 配置对象存储时由 Worker 流式上传成品，调用方直接访问外部地址；手动访问 `/content` 时可能返回 `302` |

本地存储模式下，文件会保存为 `/data/outputs/{任务ID}/{UUID}.扩展名`，不会沿用 ComfyUI 的固定文件名。

### 4.2 Range 请求

本地结果支持 Range，适合浏览器播放和断点下载：

```bash
curl -L \
  -H "Range: bytes=0-1048575" \
  "$OUTPUT_URL" \
  -o result.part
```

### 4.3 访问控制和有效期

`GET /v1/videos/{id}/content` 不要求 Bearer API Key，得到地址的任何人都可以访问。需要更严格的权限控制时，应在业务网关、Nginx 或 CDN 层增加鉴权。

任务和结果文件会按照系统保留天数清理，默认保留 3 天。任务存在但本地结果文件已经不存在时返回 HTTP `410 Gone`。


## 6. 错误响应

业务接口错误统一使用以下结构：

```json
{
  "error": {
    "task_id": "video_abc123",
    "code": "not_found",
    "message": "video task not found",
    "retryable": false
  }
}
```

没有对应任务 ID 的错误不会返回 `task_id`。

### 6.1 常见 HTTP 状态码

| HTTP 状态码 | 常见 `code` | 是否建议重试 | 说明 |
| --- | --- | --- | --- |
| `400` | `invalid_json` | 否 | JSON 格式错误、存在未知字段或字段类型错误 |
| `400` | `invalid_content` | 否 | `content` 类型、数量、字段或 `role` 不符合要求 |
| `400` | `invalid_resolution` | 否 | `resolution` 不是 `720p` 或 `2k` |
| `400` | `invalid_ratio` | 否 | `ratio` 不是 `16:9` 或 `9:16` |
| `400` | `invalid_duration` | 否 | `duration` 不在 5 至 15 秒范围内 |
| `400` | `invalid_input_reference` | 否 | 素材 URL 或 Base64 无效 |
| `401` | `unauthorized` | 否 | API Key 缺失或无效 |
| `404` | `not_found` | 否 | 任务不存在或已被清理 |
| `409` | `not_ready` | 是 | 视频结果尚未生成 |
| `410` | `output_missing` | 否 | 本地结果文件已经不存在 |
| `429` | `queue_full` | 是 | 队列已满，响应包含 `Retry-After: 5` |
| `500` | `store_error` | 是 | 服务端存储异常 |
| `503` | `queue_unavailable` | 是 | Redis 队列暂时不可用 |

调用方应同时检查 HTTP 状态码和 `error.retryable`。HTTP `429` 时优先遵循 `Retry-After`，重试创建请求时继续使用原 `Idempotency-Key`。

## 7. 推荐接入流程

```text
POST /v1/videos
        ↓
保存返回的 video id
        ↓
轮询 GET /v1/videos/{id}
        ↓
 ┌───────────────┬────────────────┬──────────────────┐
 │ 非终态         │ succeeded      │ failed/canceled  │
 │ 继续轮询       │ 读取 output_url │ 记录 error       │
 └───────────────┴────────────────┴──────────────────┘
```

轮询时间建议每30s一次，通常情况下生成时间10s大概在5分钟。
