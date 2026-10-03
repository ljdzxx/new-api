# R2 图片 URL 转存 Worker

开启图片 R2 存储后，`/v1/images/generations` 和 `/v1/images/edits` 的非流式结果及 SSE 图片事件统一尝试转存，成功时返回 R2 签名 URL。Base64 由 Go 后端直接上传；远程 HTTP/HTTPS URL 由这个 Worker 下载并写入同一个 R2 bucket，图片内容不经过 Go 服务器。URL 转存失败时由 Go 后端保留原始上游 URL，不影响其他图片的转存和响应。

## 部署

1. 修改 `wrangler.toml`：`bucket_name` 和 `R2_BUCKET_NAME` 必须与项目的图片 R2 bucket 一致；`OBJECT_PREFIX` 必须与项目对象前缀一致。
2. 上游主机动态变化时，将 `ALLOWED_SOURCE_HOSTS` 留空（默认），无需预先知道图片 URL 的主机名。Worker 会在每次下载和每次重定向前通过 Cloudflare DoH 检查 A/AAAA 记录，拒绝没有地址、解析失败或包含非公网地址的来源。允许 HTTP（默认端口 80）和 HTTPS（默认端口 443）的无用户名密码域名 URL，包括两种协议之间的重定向；拒绝其他协议、非默认端口、IP 字面量、本地域名、内网/回环/链路本地/保留地址。
   如果来源确实固定，也可以填写逗号分隔的准确主机名作为额外限制；此时重定向目标也必须在名单内。不支持通配符。已部署旧版的用户需要清空该变量并重新部署 Worker。
3. 在本目录执行 `bunx wrangler secret put IMPORT_SECRET`，输入至少 32 字符的随机密钥；然后执行 `bunx wrangler deploy`。
4. 在项目「绘图设置」填写「R2 Worker 地址」（完整的 `https://<worker-host>/import`）和「R2 Worker 密钥」（上一步的密钥）。原有 R2 Account ID / Endpoint、Bucket、Access Key ID、Secret Access Key 仍需配置，Go 使用这些凭证生成签名 URL。
5. 开启「R2 图片存储」。Worker 密钥仅保存在后端，不在配置查询中返回给前端。

## 协议

`POST /import`，请求头 `Authorization: Bearer <IMPORT_SECRET>`：

```json
{"source_url":"https://allowed-host/image.png","object_key":"generated-images/20261003/1/request-0.png","bucket":"your-image-bucket"}
```

成功返回 `200 {"object_key":"..."}`。Worker 不公开 bucket，也不生成公开 URL；后端使用该对象路径生成有时效的 S3 签名 URL。

## 行为及限制

- 流式预览图和最终图均转存，事件中的 `b64_json` 替换成 `url`；事件类型及 usage 保留。预览事件不会加入最终历史结果。
- JSON 转 SSE 的渠道也经过同一出口；支持直接写响应的渠道，不局限于 OpenAI 适配器。
- 远程 URL 转存失败（例如 Worker 未配置、超时、拒绝来源、下载或上传失败）时，Go 后端记录警告并返回该图片的原始上游 URL，不产生 `image_storage_error`。JSON、SSE、JSON 转 SSE 均支持这一兜底；成功生成的历史记录保存最终响应中的 URL，计费照常结算。多图响应逐张处理，失败图片不会阻止其他图片转存。
- Base64 转存失败仍沿用错误处理：非流式返回 HTTP 502，流式发送 `image_storage_error` 错误事件并结束。上游已经完成生成的请求仍按上游 usage 结算；历史记录标记失败。`url` 中的 Base64 Data URL 也按 Base64 处理，不属于远程 URL 兜底。
- HTTP 来源图片采用明文传输，内容可能被网络中间人篡改。Worker 调用地址和 DNS 查询仍使用 HTTPS，避免泄露 Worker 鉴权密钥。
- 已写入 R2 的对象不会因为后续某张图片转存失败而自动删除。建议为对象前缀设置生命周期规则，也用于清理流式预览图片。
- Worker 支持 PNG、JPEG、GIF、WebP，通过文件头验证；图片最多 32 MiB、下载最多 120 秒、重定向最多 5 次。可以调低 `MAX_IMAGE_BYTES`，不能调高硬上限。下载按块读取并限量缓冲后上传，兼容没有 Content-Length 的来源。
- 远程图片的对象名使用 `.png` 后缀，但实际类型写入 R2 Content-Type 元数据，URL 下载以真实内容为准。
- R2 关闭时保持现有响应行为。签名 URL 的过期时间沿用项目的「URL 有效期」设置。
- 确保项目的流式超时允许图片生成和转存耗时；流式预览会额外增加上传次数和延迟。
- 动态域名模式不是无鉴权的任意 URL 代理：只有持有 `IMPORT_SECRET` 的后端可调用，鉴权密钥不能发给客户端，也不要暴露绕过后端的 URL 导入入口。
- DNS 检查是防御加固，不是对 DNS rebinding 的完全防护：检查与 Worker `fetch()` 解析不是同一次操作，且对外部域名不能用 `resolveOverride` 固定解析地址。来源域名恶意变化时仍有 DNS 检查和使用之间的竞态。如果部署环境要求严格防御任意恶意来源，使用能固定已验证连接 IP、并强制公网出口 ACL 的独立抓取代理；不要给此 Worker 添加内网访问绑定。

## 测试

```sh
node --test worker.test.mjs
```
