# Channel Scheme

本文总结一条请求进入 `new-api` 之后，渠道选择相关策略的执行顺序、优先级，以及哪些步骤会覆盖前一步的结果。

## 总结版

一条请求的渠道执行顺序可以概括为：

1. 解析请求与基础上下文
2. 如果 token 指定了固定渠道，直接使用该渠道
3. 如果没有固定渠道，先做 token 模型权限校验
4. 尝试命中 `channel affinity`
5. 如果 affinity 未采用，走普通分组选路
6. 进入实际 relay 前，判断是否需要 `channel forward`
7. 如果请求失败且允许重试，按重试策略重新选路
8. 请求成功后，回写 affinity 缓存

其中真正会“改变最终渠道”的优先级，从高到低可以理解为：

1. `specific_channel_id` 固定渠道
2. `channel forward` 改派
3. `channel affinity` 命中并通过校验
4. 普通分组选路
5. 重试时的后续候选渠道

注意：

- `channel affinity` 是“优先选用某渠道”，不是绕过分组校验的后门
- `channel forward` 发生在渠道已经选出来之后，所以它可以把前面选中的渠道改成另一个渠道
- 日志里最终记录的 `channel_id`，通常是“最终实际执行的渠道”

## 1. 请求入口

入口在 [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:23)。

这里先做几件基础工作：

- 解析请求里的模型名、路径对应的 relay mode
- 读取 token 相关上下文
- 判断这类请求是否需要选渠道

不同 API 路径提取模型名的方式不同，例如：

- `/v1/responses`
- `/v1/chat/completions`
- `/v1/audio/*`
- `/v1/images/*`
- `/pg/chat/completions`

只有在 `shouldSelectChannel=true` 的情况下，才会进入真正的渠道选择流程。

## 2. 固定渠道优先级最高

如果 token 或上下文里带了 `specific_channel_id`，分发器会直接取这个渠道，不再走普通选路。

相关代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:36)

这个阶段还会额外校验：

- 渠道存在
- 渠道状态为启用
- 当前用户等级允许使用该渠道

如果这里命中固定渠道，后面的 `affinity` 和普通分组选路都不会再参与首次选路。

## 3. Token 模型权限校验

如果没有固定渠道，会先检查 token 是否允许访问当前模型。

相关代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:64)

如果 token 没有该模型权限，请求会直接失败，不会继续走 affinity 或普通选路。

这是“能不能选这个模型”的前置门禁，不是选哪个渠道。

## 4. Affinity 优先于普通分组选路

如果请求需要选渠道，分发器会先尝试：

- `GetPreferredChannelByAffinity(c, model, usingGroup)`

相关代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:109)
- [service/channel_affinity.go](/e:/go_project/new-api/service/channel_affinity.go:532)

### 4.1 Affinity 的作用

Affinity 的本质是“渠道粘性”：

- 某类请求之前成功走过某个渠道
- 后续相同 key 的请求优先继续走这个渠道

它不是强制命中，也不是跳过分组和模型能力校验。

### 4.2 Affinity 命中条件

是否参与 affinity，取决于 `channel_affinity_setting` 中的规则。

默认规则定义在：

- [setting/operation_setting/channel_affinity_setting.go](/e:/go_project/new-api/setting/operation_setting/channel_affinity_setting.go:54)

例如当前默认的 `codex cli trace`：

- 只匹配 `gpt-*`
- 只匹配 `/v1/responses`
- 从请求体中读取 `prompt_cache_key`
- 缓存 key 中包含 `using_group`

### 4.3 Affinity 命中后仍需再次校验

即使 affinity 缓存里拿到了一个渠道 id，也不会直接采用。

分发器还会继续检查：

- 渠道是否存在
- 渠道是否启用
- 用户等级是否允许
- 该渠道是否满足当前 `group + model`

关键校验代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:117)
- [model/channel_satisfy.go](/e:/go_project/new-api/model/channel_satisfy.go:8)

只有全部通过，affinity 才真正接管本次选路。

## 5. 普通分组选路

如果 affinity 没命中，或者命中后校验不通过，就回到普通选路：

- `CacheGetRandomSatisfiedChannel(...)`

相关代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:138)
- [service/channel_select.go](/e:/go_project/new-api/service/channel_select.go:228)

### 5.1 普通选路依据什么

普通选路依据的是 `abilities`，不是只看 `channels.group` 这一列文本。

底层能力判断和渠道列表来自：

- [model/channel_cache.go](/e:/go_project/new-api/model/channel_cache.go:221)
- [model/ability.go](/e:/go_project/new-api/model/ability.go:67)

也就是说，真正决定“某分组下某模型能否走某渠道”的，是：

- `group`
- `model`
- `enabled`
- `priority`
- `weight`

### 5.2 选路顺序

普通选路大致按这个顺序：

1. 找到满足 `group + model` 的渠道集合
2. 优先选择最高 `priority`
3. 同优先级下按 `weight` 做带权随机

如果 token group 是 `auto`，还会在多个自动分组之间切换。

## 6. Forward 会在选路之后再次改派渠道

这是最容易让人误判的一层。

即使前面的 affinity 或普通选路已经选出了渠道，真正 relay 前还会再执行一次：

- `maybeApplyChannelForward(...)`

相关代码：

- [controller/relay.go](/e:/go_project/new-api/controller/relay.go:327)
- [service/channel_forward.go](/e:/go_project/new-api/service/channel_forward.go:17)

### 6.1 Forward 的优先级为什么高

因为 forward 是在“渠道已经选出来之后”执行的。

所以它可以把：

- affinity 选中的渠道
- 普通分组选中的渠道

再次改成另一个渠道。

因此从“最终实际执行渠道”的角度看，forward 的优先级高于 affinity 和普通分组。

### 6.2 Forward 如何判断是否触发

`ShouldForwardChannelRequest(...)` 会根据请求内容抽取文本，再用渠道自己的 `forward_match_regex` 去匹配。

不同格式抽取的文本不同，例如：

- OpenAI Chat：system + 最后一条 user
- OpenAI Responses：`instructions` + 最新 user 输入
- Claude：system + 最后一条 user

只要命中 regex，就会把当前渠道改派到 `forward_target_channel_id`。

### 6.3 Forward 的日志

当前项目已经增加了 forward 改派日志。

如果发生改派，会在日志中出现类似内容：

```text
channel forward applied: from #2(gpt-mimo) to #3(jucodex), group=vip, model=gpt-5.4, path=/v1/responses
```

这条日志的位置在：

- [controller/relay.go](/e:/go_project/new-api/controller/relay.go:294)

## 7. 重试策略

如果请求执行失败，系统会决定是否重试。

相关代码：

- 文本 relay: [controller/relay.go](/e:/go_project/new-api/controller/relay.go:408)
- 任务 relay: [controller/relay.go](/e:/go_project/new-api/controller/relay.go:703)

重试时会使用：

- `GetNextRetryChannel(...)`

相关代码：

- [service/channel_select.go](/e:/go_project/new-api/service/channel_select.go:167)

### 7.1 重试候选如何生成

重试候选会基于当前请求的：

- 分组
- 模型
- 用户等级允许的渠道

先构造成一个候选列表，再按以下顺序排序：

1. `priority` 降序
2. `groupOrder` 升序
3. `weight` 降序
4. `channelID` 升序

失败过的渠道会被标记并跳过。

### 7.2 哪些情况不重试

以下情况通常不会继续重试：

- 固定渠道请求
- affinity 规则要求失败后不重试
- 本地错误
- 某些明确不可重试的状态码

## 8. 成功后回写 Affinity

请求成功后，分发器会执行：

- `RecordChannelAffinity(...)`

相关代码：

- [middleware/distributor.go](/e:/go_project/new-api/middleware/distributor.go:169)
- [service/channel_affinity.go](/e:/go_project/new-api/service/channel_affinity.go:660)

默认配置里 `SwitchOnSuccess=true`，表示：

- 缓存写入的是“本次最终成功的渠道”

如果这次请求中间发生过 forward，那么写回 affinity 的就是 forward 之后最终成功的渠道。

但下一次 affinity 再命中这个渠道时，仍然要重新通过 `group + model` 校验，不是无条件采用。

## 9. 最终优先级排序

如果从“最终请求到底落到哪个渠道”的角度看，优先级建议这样理解：

1. `specific_channel_id`
   直接锁定首次选路，不走普通分发。
2. `channel forward`
   在首次选路完成后仍可改写最终渠道。
3. `channel affinity`
   在普通分发前优先尝试，但必须通过分组和模型能力校验。
4. 普通分组选路
   按 `abilities + priority + weight` 选择。
5. 重试链路
   只在执行失败后进入，选择后续候选渠道。
6. affinity 回写
   这是成功后的后置动作，不影响当前请求，只影响后续请求。

## 10. 排查建议

看到“为什么请求走到了某个渠道”时，建议按下面顺序排查：

1. 有没有 `specific_channel_id`
2. affinity 日志里是否有 `selected_group` 和 `channel_id`
3. 是否出现 `channel forward applied`
4. 当前 `abilities` 中该 `group + model` 对应有哪些启用渠道
5. 渠道的 `priority` 和 `weight` 是什么
6. 是否发生了 retry，`use_channel` 里是否出现多个渠道

如果日志中只有：

- `channel_affinity.reason`
- `override_template`

但没有：

- `selected_group`
- `channel_id`

通常表示“规则参与了，但 affinity 没有真正决定本次渠道”。
