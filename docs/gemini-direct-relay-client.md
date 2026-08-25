# Gemini 图像直连客户端对接

本文档描述 NewAPI 的可选 Gemini 图像直连协议。直连模式只减少生成结果图片从 NewAPI 返回客户端时的下行流量；客户端发给 NewAPI 的原始请求（包括参考图）仍然会经过 NewAPI。

## 1. 能力声明

客户端继续使用原来的 NewAPI 请求地址、鉴权方式和 Gemini 原生请求体，不需要增加任何直连专用请求头。服务端根据 Gemini 直连总开关、全局授权开关或用户级授权、以及请求和渠道是否满足条件，决定返回直连票据或普通 Gemini 中转响应。

符合直连条件时返回 `object: "newapi.direct_ticket"` 票据；其他情况直接返回原有 Gemini 响应体和状态码。客户端必须先检查响应体中的 `object`，识别为票据才执行下文的上游调用；非票据响应交给原有 Gemini 响应处理逻辑。这样关闭服务端开关、撤销全局或用户权限、或直连条件不满足时，客户端无需改变请求方式即可自动回退。

管理员可以在系统设置中启用 Gemini 图像直连总开关。默认情况下，还需要在用户编辑页单独为用户启用“Gemini 图像客户端直连”；如果开启 Gemini 全局直连授权，则无需逐个启用用户设置，所有满足其他条件的用户都会尝试使用直连。用户设置默认关闭。

首版直连只适用于以下请求：

- `POST` 原生 Gemini `models/{model}:generateContent`；
- 非流式请求，不能使用 `streamGenerateContent` 或任何 query 参数（包括 `alt=sse`）；
- Gemini 图像生成模型；
- 原生 Gemini API Key 渠道，且渠道没有服务端 Proxy；
- 不使用 Vertex Service Account、ADC 或服务端生成访问令牌；
- 渠道没有系统提示词、参数覆盖、Header 覆盖或 Thinking 自动适配。
- tiered billing 表达式不能使用 `param()` 或 `hour()`、`minute()`、`weekday()`、`month()`、`day()` 等时间函数；这些表达式会自动回退服务器中转，避免延迟 callback 改变计费条件。`header()` 使用冻结的请求 Header，可以直连。

不支持的请求会自动回退服务器中转。直连成功选定渠道后，服务端不会为这张票据自动切换渠道或重试上游请求。

## 2. 票据响应

符合条件时，NewAPI 返回 `200` 和如下对象：

```json
{
  "object": "newapi.direct_ticket",
  "version": 1,
  "request_id": "req_xxx",
  "ticket_id": "ticket_xxx",
  "attempt_id": "attempt_xxx",
  "expires_at": 1780000000,
  "callback_deadline": 1780001800,
  "upstream": {
    "method": "POST",
    "url_b64": "...",
    "api_key_b64": "...",
    "model_b64": "...",
    "headers": {
      "Content-Type": "application/json",
      "x-goog-api-key": "..."
    },
    "action": "generateContent"
  },
  "callback": {
    "url": "https://newapi.example/api/direct-relay/gemini/callback",
    "token": "one_time_callback_token"
  },
  "request_fingerprint": "sha256"
}
```

`url_b64`、`api_key_b64`、`model_b64` 和 `headers.x-goog-api-key` 使用不带 `=` 填充的 Base64URL。客户端解码示例：

```text
base64url_decode(ticket.upstream.url_b64)
base64url_decode(ticket.upstream.api_key_b64)
base64url_decode(ticket.upstream.model_b64)
```

Base64URL 不是加密。运行中的客户端可以被调试工具读取上游 URL 和 API Key；它只避免凭据在普通界面中直接显示。客户端不得把票据、API Key 或 callback token 写入普通日志。

票据不包含原始请求体、参考图 Base64、生成图片、图片 URL 或 NewAPI 用户 Token。客户端必须保留首次发送给 NewAPI 的原始 JSON 请求体，并使用该请求体向上游发起请求。

客户端应计算保留请求体的 SHA-256，并与 `request_fingerprint` 比较。两者不一致时不得调用上游，也不得尝试修补请求体；应把该票据视为本地协议错误。

## 3. 一次上游调用

收到票据后，客户端按以下规则执行一次调用：

1. 在内存中解码 `url_b64`、`api_key_b64` 和 `model_b64`。
2. 使用原始请求体发送 `POST` 到解码后的 URL。
3. 设置票据中的 `headers`；如果客户端自行设置 Header，必须保留 `Content-Type: application/json` 和解码后的 `x-goog-api-key`。
4. 只能调用一次，不自动重试，不跟随跳转到其他域名。
5. 校验响应域名仍是票据 URL 的目标域名，并记录响应状态、耗时、字节数和 SHA-256。
6. 不等待 callback 网络请求即可把完整上游响应（成功或失败）交给本地业务调用方，但必须在本次请求生命周期结束前把 callback 任务持久化，避免进程退出后丢失计费回调。

发往 Gemini 的请求不能携带 NewAPI 的 `Authorization`、NewAPI 用户 Token、callback token 或其他 NewAPI 私有 Header。`model_b64` 是映射后的上游模型名，主要用于校验和审计；实际目标模型已经包含在解码后的 URL 中，客户端不得自行选择其他模型。

直连成功时，不调用 NewAPI OSS 上传接口，也不把图片内容、Base64 或图片 URL 上传回 NewAPI。

`expires_at` 是票据建议有效期。客户端应在过期后停止使用票据；服务端在 `callback_deadline` 后会优先退款。上游可能已经成功但回调丢失时，存在“图片已生成、额度已退款”的财务风险，客户端应尽快提交回调。

## 4. 回调认证与重试

回调地址：

```http
POST {ticket.callback.url}
X-NewAPI-Direct-Relay-Callback: {ticket.callback.token}
Content-Type: application/json
```

callback token 是票据专用凭据。服务端只保存 hash，并使用常量时间比较。callback token 只能放在 `X-NewAPI-Direct-Relay-Callback` Header 中，不能放在 URL、查询参数或普通日志中。

客户端回调任务必须持久化，不能只放在内存中：

- 网络错误、连接超时和 HTTP `5xx` 使用指数退避重试；
- 不因回调失败重试上游 Gemini 请求；
- 收到 HTTP `200` 且返回 `status` 为 `settled` 或 `refunded` 后删除本地任务；
- 收到 HTTP `200` 但 `status` 为 `allocating`、`settling` 或 `refunding` 时保留任务并继续退避重试；`allocating` 只会出现在服务端刚持久化票据但尚未完成发布的极短异常窗口；
- HTTP `4xx` 通常表示协议或票据错误，应记录并停止无限重试；
- 服务端结算暂时失败会返回 `5xx`，客户端应继续重试。

成功回调缺少或无法解析 `usage_metadata` 时，服务端按票据冻结的预扣额度结算并记录 `usage_missing`；客户端不要提交自行计算的 quota。

建议退避序列为 1、2、4、8、16、32 秒，之后按分钟级间隔重试，直到 `callback_deadline`。服务端会对重复成功、重复失败和成功/失败竞争进行幂等处理。

## 5. 成功回调

成功回调只提交计费和审计摘要，不提交 Gemini 完整响应：

```json
{
  "ticket_id": "ticket_xxx",
  "attempt_id": "attempt_xxx",
  "outcome": "success",
  "upstream_status": 200,
  "usage_metadata": {
    "promptTokenCount": 123,
    "cachedContentTokenCount": 0,
    "candidatesTokenCount": 258,
    "thoughtsTokenCount": 0,
    "toolUsePromptTokenCount": 0,
    "totalTokenCount": 381,
    "promptTokensDetails": [],
    "toolUsePromptTokensDetails": [],
    "candidatesTokensDetails": [{"modality": "IMAGE", "tokenCount": 258}]
  },
  "candidate_count": 1,
  "image_count": 1,
  "response_bytes": 7340032,
  "response_sha256": "sha256_of_full_response_bytes",
  "upstream_request_id": "optional",
  "elapsed_ms": 4200
}
```

`response_sha256` 是完整上游响应字节的 SHA-256 十六进制字符串。回调中不能包含 `error_body`、图片 Base64、图片 URL 或完整 Gemini 响应。

上例中的 `sha256_of_full_response_bytes` 和 `sha256_of_full_error_body` 只是字段占位符，实际请求必须替换为 64 个小写十六进制字符。

`usage_metadata` 应原样取自 Gemini 成功响应的 `usageMetadata`。`candidate_count` 是 `candidates` 数组长度；`image_count` 是所有 candidate 中 MIME 类型以 `image/` 开头的 `inlineData` part 数量。计数时只能读取响应摘要，不能把 part 的 `data` 放入 callback。SHA-256 使用 64 个小写十六进制字符。

客户端不提交最终 quota。服务端使用票据创建时冻结的价格、分组、模型映射、tiered billing 表达式和预扣额度结算。`usage_metadata` 完整可解析时，服务端按 Gemini 中转相同的 usage 归一化规则计费；缺失或无法解析时按冻结的预扣上限结算，并将票据标记为 `usage_missing`。

## 6. 失败回调

失败回调必须保留完整的上游错误正文，最多保留原始前 `1 MiB`：

```json
{
  "ticket_id": "ticket_xxx",
  "attempt_id": "attempt_xxx",
  "outcome": "error",
  "upstream_status": 400,
  "error_content_type": "application/json",
  "error_body": "{\"error\":{...}}",
  "error_body_bytes": 382,
  "error_body_sha256": "sha256_of_full_error_body",
  "error_body_truncated": false,
  "upstream_request_id": "optional",
  "elapsed_ms": 800
}
```

超过 `1 MiB` 时，客户端必须：

- `error_body` 只保留前 `1 MiB` 原始字节；
- 设置 `error_body_truncated: true`；
- `error_body_bytes` 填写完整错误正文总字节数；
- `error_body_sha256` 填写完整错误正文（截断前）的 SHA-256。

服务端会将该回调视为上游失败并退款。错误正文不能放入普通日志；票据审计最多保存 `1 MiB`，同时保存总字节数、hash 和截断标记。

如果请求在收到 HTTP 响应前发生 DNS、TLS、连接或读取错误，可使用 `upstream_status: 0`，把客户端获得的完整 transport error 文本放入 `error_body`，并使用合适的 `error_content_type`（通常为 `text/plain; charset=utf-8`）。这仍然只对应一次上游尝试，不得因为 transport error 再次请求 Gemini。

失败回调的 `upstream_status` 不能是 `2xx`；成功回调的 `upstream_status` 必须是 `2xx`。回调正文中不得包含完整 Gemini 响应、图片 Base64、图片 URL 或任何图片内容。

## 7. 本地队列伪代码

```text
response = post_newapi(original_request)

if response.object != "newapi.direct_ticket":
    return response                       // 普通中转回退

ticket = response
upstream_url = base64url_decode(ticket.upstream.url_b64)
api_key = base64url_decode(ticket.upstream.api_key_b64)

try:
    upstream_response = post_once(
        url=upstream_url,
        headers={"Content-Type": "application/json", "x-goog-api-key": api_key},
        body=original_request.body,
        follow_redirects=false,
    )
    result = upstream_response.body       // 先返回给本地业务
    if 200 <= upstream_response.status < 300:
        callback = build_success_summary(upstream_response)
    else:
        callback = build_error_summary(upstream_response, max_body_bytes=1*1024*1024)
except error as err:
    result = err.body                    // 先返回完整错误给本地业务
    callback = build_error_summary(err, max_body_bytes=1*1024*1024)
finally:
    durable_callback_queue.put_and_flush(ticket, callback)

return result
```

本地队列需要保存 `ticket_id`、`attempt_id`、callback URL/token、回调正文、下次重试时间和重试次数。成功或失败回调都只属于这一个 ticket；客户端不得使用同一票据再次调用 Gemini。

本地业务返回不应等待 callback 请求成功，但队列写入必须具备崩溃恢复能力，例如 SQLite 事务、带 `fsync` 的日志或移动端平台提供的可靠任务存储。只放在内存中的重试任务不符合协议要求。

## 8. 安全与兼容边界

- NewAPI 和上游地址可以使用 `http` 或 `https`。生产环境建议使用 HTTPS；HTTP 仅适合可信内网、测试环境或已有安全隧道的部署。使用 HTTPS 时不要关闭证书校验。
- 不要跟随上游重定向到非票据域名。
- 不要在客户端日志、崩溃报告、分析 SDK 或剪贴板中记录 API Key/token。
- 直连总开关和全局授权开关默认关闭；关闭总开关、撤销全局授权或撤销用户授权后无需升级客户端，客户端会自动按普通中转处理。
- 首版没有真正的端到端加密。未来可以在不改变票据字段语义的情况下增加 JWE/HPKE。
- 参考图上传流量不会减少；只有生成响应图片不再经过 NewAPI。
