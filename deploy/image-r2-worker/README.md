# R2 图片 URL 转存 Worker

开启图片 R2 存储后，`/v1/images/generations` 和 `/v1/images/edits` 的非流式结果及 SSE 图片事件统一尝试转存，成功时返回 R2 签名 URL。Base64 由 Go 后端直接上传；远程 URL 由 Worker 下载并写入同一个 R2 bucket，图片内容不经过 Go 服务器。URL 转存失败时由 Go 后端保留原始上游 URL，不影响其他图片的转存和响应。

## 部署

1. 根据 `wrangler.toml.example` 配置本地 `wrangler.toml`。`bucket_name` 和 `R2_BUCKET_NAME` 必须与项目的图片 R2 bucket 一致。
2. 在本目录执行 `bun x wrangler secret put IMPORT_SECRET`，输入至少 32 字符的随机密钥，然后执行 `bun x wrangler deploy --config wrangler.toml`。
3. 在项目「绘图设置」填写「R2 Worker 地址」（完整的 `https://<worker-host>/import`）和「R2 Worker 密钥」（上一步的密钥）。原有 R2 Account ID / Endpoint、Bucket、Access Key ID、Secret Access Key 仍需配置，Go 使用这些凭证生成签名 URL。
4. 开启「R2 图片存储」。Worker 密钥仅保存在后端，不在配置查询中返回给前端。

新版不再使用 `ALLOWED_SOURCE_HOSTS`、`MAX_IMAGE_BYTES` 或 Worker 的 `OBJECT_PREFIX`。旧配置保留这些变量也不会限制转存，不需要修改生产密钥；项目「R2 对象前缀」仍用于后端生成对象名。

## 协议

`POST /import`，请求头 `Authorization: Bearer <IMPORT_SECRET>`：

```json
{"source_url":"https://image-host/image.png","object_key":"generated-images/20261003/1/request-0.png","bucket":"your-image-bucket"}
```

成功返回 `200 {"object_key":"..."}`。Worker 不生成公开 URL；后端使用该对象路径生成有时效的 S3 签名 URL。

## 转存行为

- 直接通过 `fetch(source_url, { redirect: 'follow' })` 下载并写入 R2，不查询 DoH、不检查主机白名单或公网 IP，也不附加端口、来源凭据、图片格式、32 MiB 大小、5 次重定向或 120 秒下载限制。具体地址和重定向能否访问由 Workers 的 `fetch` 处理。
- 下载内容按原样写入 R2，Content-Type 沿用来源响应；来源未提供时使用 `application/octet-stream`，不进行文件头检查或格式转换。
- 只保留 Worker 鉴权、R2 绑定、必填字段及 bucket 一致性检查。不要向客户端泄露 `IMPORT_SECRET`；新版不替持有密钥的调用方检查来源地址是否可信。
- 下载与上传仍受运行环境、网络及后端请求超时影响；取消的是本脚本额外设置的限制，并非绕过平台本身的限制。当前实现读取完整响应后上传，也支持没有 Content-Length 的来源。
- 流式预览图和最终图均转存，事件中的 `b64_json` 替换成 `url`；事件类型及 usage 保留。JSON 转 SSE 和直接写响应的渠道也经过同一出口。
- URL 转存失败时，Go 后端记录警告并返回原始上游 URL，不产生 `image_storage_error`；多图响应逐张处理，失败图片不会阻止其他图片转存。历史记录保存最终响应中的 URL，计费照常结算。
- Base64 转存失败仍沿用错误处理：非流式返回 HTTP 502，流式发送 `image_storage_error` 错误事件并结束。`url` 中的 Base64 Data URL 也按 Base64 处理。
- 已写入 R2 的对象不会因后续转存失败自动删除。R2 关闭时保持原有响应行为；签名 URL 有效期沿用项目设置。

## 排查

在 Windows PowerShell 中执行无密钥探测，不会下载或上传图片：

```powershell
curl.exe -i -X POST "https://<worker-host>/import"
```

- `401 Unauthorized`：已到达 `/import`，无密钥探测的预期结果。GET `/import` 应返回 405。
- `404 Not found`：检查是否遗漏 `/import`，或误写成 `/import/`。Cloudflare 返回 `error code: 1042` 时，先检查生产 `workers.dev` 域名是否启用、路由是否指向正确 Worker。
- `500` / `error code: 1101`：查看同一已发布版本的异常日志。模块入口应保留 `async fetch`；额外包装处理函数时不能遗漏返回内部 Promise。
- `502`：查看 `[image r2] import failed` 的安全诊断；新版不再出现 DNS 检查阶段。

| stage / reason | 含义 |
| --- | --- |
| `request_parse / invalid_request_json` | 请求 JSON 无法读取或解析。 |
| `source_download / source_fetch_failed` | Workers 无法下载来源 URL。 |
| `source_download / source_http_error` | 来源返回非成功 HTTP 状态，`upstream_status` 为实际状态码。 |
| `image_read / image_read_failed` | 下载响应读取失败。 |
| `r2_upload / r2_put_failed` | R2 写入失败。 |

日志不记录密钥、来源 URL 或任意异常消息。新版诊断兼容现有后端，只需重新部署 Worker 即可采用简化转存流程。

## 测试

```powershell
node --test worker.test.mjs
```
