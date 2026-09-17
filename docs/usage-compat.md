# `/v1/usage` 余额查询兼容接口

对接 `sub2api` 的 `GET /v1/usage`，返回原始 JSON，不套 `success/data` 外层。

## 请求

```bash
curl 'https://你的域名/v1/usage?days=30' \
  -H 'Authorization: Bearer sk-你的密钥'
```

密钥按以下优先级读取：

1. `Authorization: Bearer <key>`，Bearer 大小写不敏感。
2. `x-api-key: <key>`。
3. `x-goog-api-key: <key>`。

Authorization 不是 Bearer 或其内容为空时，继续读取下一个请求头；高优先级提供了非空无效密钥时，不回退。密钥最多 128 字节。沿用本项目密钥的 `sk-` 展示前缀。

`key`、`api_key` 查询参数有非空内容时返回 HTTP 400。过期、额度耗尽、钱包余额不足不影响查询；禁用/删除的 Key、禁用用户、不符合 IP 白名单或分组权限的请求仍被拒绝。

| 可选参数 | 规则 |
| --- | --- |
| `days` | 每日统计范围，默认 30；整数 1–90；纯空白按默认值；其他无效值返回 400 |
| `timezone` | 每日统计范围的 IANA 时区；缺省或无效时使用服务器时区 |
| `start_date` | 模型统计起始日期 `YYYY-MM-DD`，默认当前时间减 30 天 |
| `end_date` | 模型统计结束日期 `YYYY-MM-DD`，包含当天，默认当前时间 |

无效日期沿用默认值，日期倒序不报错。日期参数不改变 `usage.today/total`。与参考实现一致，`timezone` 改变每日查询范围边界，日期分桶仍使用服务器时区。

## 响应

有限额度 Key 示例：

```json
{
  "mode": "quota_limited",
  "isValid": true,
  "status": "active",
  "remaining": 1.5,
  "unit": "USD",
  "quota": {"limit": 2, "used": 0.5, "remaining": 1.5, "unit": "USD"}
}
```

`status` 对应 `active`、`expired`、`quota_exhausted`；后两者仍返回 `isValid: true`。设置了到期时间时，另返回 RFC3339 格式 `expires_at` 和整数 `days_until_expiry`（向下取整，最低 0）。

无限额度 Key 的钱包示例：

```json
{
  "mode": "unrestricted",
  "isValid": true,
  "planName": "钱包余额",
  "remaining": 5,
  "balance": 5,
  "unit": "USD"
}
```

订阅模式返回 `mode/isValid/planName/remaining/unit/subscription`。`subscription` 字段为：

- `daily_usage_usd`、`weekly_usage_usd`、`monthly_usage_usd`。
- `daily_limit_usd`、`weekly_limit_usd`、`monthly_limit_usd`，未配置的周期为 `null`。
- `weekly_window_start`（时间或 `null`）、`expires_at`（时间）。

无限额度订阅的 `remaining` 为 `-1`。仅订阅用户无可用订阅时，省略 `remaining/subscription`。

统计查询成功时，上述响应另包含：

- `usage.today`、`usage.total`：`requests/input_tokens/output_tokens/cache_creation_tokens/cache_read_tokens/total_tokens/cost/actual_cost`。
- `usage.average_duration_ms`、`usage.rpm`、`usage.tpm`；RPM/TPM 为最近 5 分钟的总量除以 5 后取整。
- `daily_usage` 数组：`date/requests/input_tokens/output_tokens/cache_read_tokens/cache_write_tokens/total_tokens/cost/actual_cost`。无数据返回 `[]`，不补零日期。
- `model_stats` 数组：`model/requests/input_tokens/output_tokens/cache_creation_tokens/cache_read_tokens/total_tokens/cost/actual_cost/account_cost`。无数据则省略，按总 token 数降序排列。

统计仅包含当前 Key、当前用户的消费日志；日志库查询失败时省略统计，余额仍返回成功。

## 本项目数据映射

- 金额始终使用 `quota / common.QuotaPerUnit` 转为 USD，不受面板展示币种或 `DisplayTokenStatEnabled` 影响。
- 本项目的 `UnlimitedQuota` 决定 Key 是否有限额，有限额但总额度为 0 的 Key 仍显示 `quota_limited`，避免显示成无限额度。
- 无限额 Key 按用户钱包/订阅优先设置选择来源。多个套餐按结算顺序选择首个适用且有余额的套餐，全部耗尽时展示首个适用套餐。
- 本项目一个套餐只有一个重置周期，映射至同名日/周/月字段；不重置和自定义周期仅通过 `remaining` 表示可用额度，不虚构日/周/月限额。
- 本项目没有 Key 级 5h/1d/7d 金额限速配置，因此不返回参考接口的可选 `rate_limits`。
- `actual_cost` 来自消费日志扣费，`cost` 在日志保存了正分组倍率时除去该倍率，否则使用扣费金额。历史日志未单独保存上游账号成本，`account_cost` 返回 0。统计受日志保留和消费日志开关影响。

## 错误格式

鉴权错误与参考接口一致，使用 `{"code":"API_KEY_REQUIRED","message":"..."}`，例如缺少密钥/密钥无效/禁用返回 401，IP 或分组拒绝返回 403。

无效 `days` 返回 HTTP 400：

```json
{"type":"error","error":{"type":"invalid_request_error","message":"Invalid days, allowed range is 1-90"}}
```
