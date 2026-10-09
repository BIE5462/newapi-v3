# 速创API（SuChuang）渠道 · 客户调用文档

本文档说明如何通过中转站（new-api）调用速创API（api.wuyinkeji.com）平台的图片与视频模型。

## 1. 接入信息

| 项目 | 说明 |
|---|---|
| 中转站地址 | `https://你的域名`（new-api 部署地址） |
| 认证 | 请求头 `Authorization: Bearer {你的 API Key}`（在控制台创建令牌获取） |
| 计费 | 按运营后台为模型配置的价格扣费（图片按张、视频按次） |

## 2. 支持的模型

| 模型名 | 类型 | 调用协议 |
|---|---|---|
| `image_gpt_2.5_flare` | 图片生成/编辑 | OpenAI Images |
| `image_nanoBanana2` | 图片生成 | Gemini 协议 |
| `image_nanoBanana_pro` | 图片生成 | Gemini 协议 |
| `video_google_omni` | 视频生成 | 视频协议 |

> 模型名即速创平台的异步端点标识。平台新增模型时，只需在渠道里添加对应的模型名即可。

## 3. 图片生成（OpenAI 协议）

```bash
curl -X POST "https://你的域名/v1/images/generations" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "image_gpt_2.5_flare",
    "prompt": "一只在月球上骑自行车的宇航猫",
    "size": "1024x1024",
    "quality": "high",
    "n": 1,
    "response_format": "url"
  }'
```

响应（同步返回，内部已完成异步提交与轮询）：

```json
{
  "created": 1720000000,
  "data": [{ "url": "https://..." }]
}
```

参考图（图生图）用 `images` 字段传公网 URL（多个以逗号拼接）；遮罩、背景等差异化参数走 `extra_fields`：

```json
{
  "model": "image_gpt_2.5_flare",
  "prompt": "把猫换成狗",
  "images": ["https://example.com/cat.png"],
  "extra_fields": {
    "mask": "https://example.com/mask.png",
    "background": "transparent"
  }
}
```

## 4. 图片编辑（OpenAI 协议，multipart 上传）

```bash
curl -X POST "https://你的域名/v1/images/edits" \
  -H "Authorization: Bearer $KEY" \
  -F "model=image_gpt_2.5_flare" \
  -F "prompt=把背景换成星空" \
  -F "image=@/path/to/cat.png"
```

> 上传的图片文件会由中转站自动转存到已配置的 OSS 并生成公网 URL，再提交给上游。

## 5. 图片生成（Gemini 协议，nanobanana 系列）

```bash
curl -X POST "https://你的域名/v1beta/models/image_nanoBanana2:generateContent" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{
      "role": "user",
      "parts": [
        { "text": "把这只猫变成宇航员" },
        { "inlineData": { "mimeType": "image/png", "data": "base64..." } }
      ]
    }],
    "generationConfig": {
      "responseModalities": ["IMAGE"],
      "imageConfig": { "aspectRatio": "1:1", "imageSize": "2K" }
    }
  }'
```

响应（Gemini 原生格式，图片在 `inlineData`）：

```json
{
  "candidates": [{
    "content": {
      "role": "model",
      "parts": [
        { "inlineData": { "mimeType": "image/png", "data": "base64..." } }
      ]
    }
  }]
}
```

参数说明：

| 字段 | 说明 |
|---|---|
| `contents[].parts[].text` | 提示词 |
| `contents[].parts[].inlineData` | 参考图（base64，中转站自动转存 OSS 为 URL） |
| `generationConfig.imageConfig.imageSize` | 尺寸档位：`1K` / `2K` / `4K` |
| `generationConfig.imageConfig.aspectRatio` | 比例：`1:1` / `16:9` / `9:16` / `4:3` 等 |

## 6. 视频生成（GOOGLE_OMNI）

提交（返回任务 ID）：

```bash
curl -X POST "https://你的域名/v1/video/generations" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "video_google_omni",
    "prompt": "一只猫在雨中奔跑",
    "size": "1280x720",
    "seconds": "10",
    "image": "https://example.com/ref.png",
    "metadata": { "video": "https://example.com/ref.mp4" }
  }'
```

响应：

```json
{ "id": "task_xxx", "status": "queued" }
```

查询结果（轮询）：

```bash
curl "https://你的域名/v1/video/generations/task_xxx" \
  -H "Authorization: Bearer $KEY"
```

响应：

```json
{ "status": "completed", "url": "https://...视频地址..." }
```

失败时：`status` 为 `failed`，并附带 `reason` 字段说明原因。

### 视频参数速查

| 中转站字段 | 上游字段 | 说明 |
|---|---|---|
| `prompt` | `prompt` | 提示词（必填） |
| `image` / `images` | `images` | 参考图 URL，最多 1 张 |
| `size` | `size` | 视频尺寸，如 `1280x720`、`720x1280` |
| `seconds` / `duration` | `duration` | 时长（秒），字符串 |
| `metadata.video` | `video` | 参考视频 URL，最多 1 个 |

## 7. 注意事项

- 速创接口均为异步：图片走同步封装（客户端无感知），视频为真异步（提交后需轮询查询）。
- 参考图 / 参考视频必须是**公网可访问 URL**；base64 或本地文件需先转成 URL（图片编辑的 multipart 上传除外，中转站会自动转存 OSS）。
- 未在表中列出的参数会原样透传给上游；若上游报「存在未绑定的参数」，中转站会自动剔除该参数并重试。
