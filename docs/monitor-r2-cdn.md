# 监控 HTML 预览的 R2 加速配置

建议当前项目采用“私有 R2 桶 → Worker → 独立 HTTPS 域名 → 边缘缓存”。桶中同时保存 `prompt.txt`、`response.txt` 和 `result.json`，直接公开整个共享桶会开放这些对象及桶内其他文件。Worker 只开放监控前缀下的 `artwork.html`，无需公开桶或分发临时签名 URL。

## 推荐：私有桶 + Worker

1. Cloudflare 控制台进入 **Workers & Pages → Create → Worker**，使用项目的 `deploy/monitor-preview-worker/worker.mjs` 作为模块代码。
2. 在 Worker 的 **Bindings** 添加 **R2 bucket** 绑定，变量名 `ARTWORKS`，选择当前用于监控上传的桶。绑定不是 Access Key，不需要在代码中填写密钥。
3. 在 Worker 的变量中设置 `SVG_PREFIX`，与监控的“R2 对象前缀”一致，默认 `monitor-svg`。
4. 在 Worker 的 **Settings → Domains & Routes → Add → Custom Domain** 配置独立域名，例如 `preview.example.com`；该域名所在 zone 应在同一 Cloudflare 账号管理。不要复用主站域名。
5. 在本站监控设置中的 **SVG 公开访问地址（可选）** 填入 `https://preview.example.com`。此处填域名根地址，不重复添加对象前缀。S3 Endpoint、上传凭据、Bucket 保持原配置。
6. 新生成的作品将返回稳定的 `https://preview.example.com/monitor-svg/<分组哈希>/<记录ID>/artwork.html`。无需 CORS：页面直接通过 iframe 导航加载作品，不在主页面读取跨域 HTML。

Worker 只接受 GET/HEAD，仅公开符合路径规则的 HTML。它通过 Workers Cache API 缓存成功响应，浏览器缓存 5 分钟、边缘缓存 1 小时；查询参数不分裂缓存。此 Worker 路径不需要另外添加 HTML Cache Rule。它保留 R2 桶私有状态，未启用公开的 r2.dev 地址。

验证：对**同一作品、同一地区**连续请求两次，检查响应 `X-Preview-Cache` 从 `MISS` 变成 `HIT`，并比较 TTFB。第一次请求仍可能回源；边缘缓存可能提前驱逐，不保证永久命中。该响应头是示例 Worker 自己添加的，不要求 `CF-Cache-Status` 为 HIT。

```powershell
curl.exe -I "https://preview.example.com/monitor-svg/<分组哈希>/<记录ID>/artwork.html"
```

HEAD 未命中时只读取元数据，不写入空缓存；实际 GET 才填充缓存。用浏览器刷新两次，或使用 `curl.exe -D - -o NUL "<完整HTML地址>"` 两次验证 GET 命中。

R2 删除对象并不会立即清除 CDN 或浏览器副本。当前示例允许已缓存文件最多再存活约一小时（另有浏览器短缓存）；需要立即下线时，应同时清除对应 CDN URL 缓存。项目不会自动调用 Cloudflare purge API。

## 备选：R2 直接绑定自定义域名

适合**只存公开作品 HTML 的专用桶**。在 R2 桶的 **Settings → Custom Domains → Connect Domain** 绑定 `preview.example.com`，然后设置 Cloudflare Cache Rule：

- Hostname 等于 `preview.example.com`，路径以 `/monitor-svg/` 开头且以 `/artwork.html` 结尾。
- Cache eligibility 设为 Eligible for cache；HTML 默认不被缓存，只有绑定域名仍不足以获得 HTML CDN 命中。
- Edge TTL 选择忽略源站 Cache-Control 并使用 1 小时。当前上传的对象元数据可能是 `private`，必须显式覆盖，否则不会获得预期缓存。
- Browser TTL 建议 5 分钟，便于作品清理后及时失效。

使用专用桶需要同时调整上传目标；当前功能复用图片生成的 R2 连接，不会自动拆分桶。因此现有共享桶优先使用上面的 Worker。

## 应用中的加载优化和兼容

- 同一作品 ID 的 iframe 不因每 30 秒获取到不同签名而重新导航；切换作品或重新打开预览时才采用最新 URL。
- 新 HTML 自带受 CSP 哈希允许的应用缩放脚本，把整份文档等比缩放并居中放入容器。模型返回的脚本与事件属性仍被移除，iframe 不授予同源权限。
- 旧 HTML 没有缩放脚本，首次通过本站 `/api/monitor/preview` 从 R2 读取并应用当前预览处理，浏览器缓存 5 分钟；原始归档文件不改写。旧版作品继续走此兼容路径，新生成作品走配置的 CDN 地址。
- 自定义域名解决稳定地址和缓存，不能保证所有地区都低延迟。中国大陆访问仍取决于 Cloudflare 的实际网络路径，应在用户所在网络测试。

参考 Cloudflare 官方文档：

- [R2 Public buckets / 自定义域名](https://developers.cloudflare.com/r2/buckets/public-buckets/)
- [Presigned URLs：签名仅支持 S3 API 域名，不能替换为自定义域名](https://developers.cloudflare.com/r2/api/s3/presigned-urls/)
- [默认缓存行为：HTML、JSON 默认不缓存](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)
