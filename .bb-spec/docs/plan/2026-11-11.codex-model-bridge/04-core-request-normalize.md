---
name: 04-core-request-normalize
description: 请求整理：中途换模型时去掉对方解不开的内容、中台请求去掉官方专有工具、代为完成中台模型的上下文压缩
---
# 核心：请求整理

## 目标
同一对话在官方模型和中台模型之间切换不报错；中台模型的长对话在压缩时不报错；中台收不到官方专有工具。

## 业务规则（来源：PRD 02-model-routing C3、C16、C17、C18）
- 发往中台的请求：去掉对话记录（`input` 数组）里所有带 `encrypted_content` 的 `reasoning` 条目，以及不是本工具生成的 `compaction` 条目；本工具生成的压缩摘要换成一条普通用户消息（文字为"以下是之前对话的摘要：" + 摘要）；可见消息和工具调用记录原样保留。
- 发往中台的请求：从 `tools` 数组里去掉 `type` 不是 `function` 的工具（如 web_search、image_generation 等官方专有工具）。
- 发往官方的请求：只去掉两类内容，其余逐字节保留——①条目 id 记录在"中台推理 id 集合"里的 `reasoning` 条目；②本工具生成的压缩摘要条目，换成同上的普通用户消息。两类都没有时，请求体不改（保持原样和原压缩格式）。
- 中台推理 id 集合：转发中台回复时，旁路读取 SSE 里 `response.output_item.done` 事件中 `item.type == "reasoning"` 的 `item.id` 记下来，持久化到数据目录 `relay-reasoning-ids.json`，最多保留最近 5000 个。不修改中台回复内容。
- 压缩接管：发往中台的请求，`input` 最后一条是 `{"type":"compaction_trigger"}` 时，不原样转发，改为：
  1. 向同一中台模型发一次非流式请求：`instructions` 为"请为另一位编程助手总结下面的对话。保留目标、决定、约束、仓库状态、改过的文件、测试结果、失败、进行中的工作和下一步。只输出总结正文。"，`input` 为整理后的对话记录（去掉 trigger）；
  2. 取回复里的文字作为摘要，生成本工具的压缩条目：`{"type":"compaction","encrypted_content":"{\"type\":\"model_bridge_compaction\",\"summary\":\"<摘要>\"}"}`；
  3. 按 Codex 远端压缩的流式返回格式把这个条目交还给 Codex。事件顺序和字段以 Ollama（MIT，commit 8a89b76）`server/responses_compact.go` 的 `handleResponsesCompaction` 流式分支为准：response.created → response.output_item.added → response.output_item.done（item 为上面的 compaction 条目）→ response.completed。
  4. 中台失败 → 按中台错误中性化规则返回错误，Codex 原对话不变。
- `POST /v1/responses/compact`（单独压缩接口）且模型是中台模型时，同样执行第 1、2 步，返回 `{"output":[<compaction 条目>]}`。
- 本工具生成的压缩条目识别方法：`type == "compaction"` 且 `encrypted_content` 能解析为 JSON、其 `type == "model_bridge_compaction"`。

## 涉及文件
- core/internal/normalize/relay.go（新建）
- core/internal/normalize/official.go（新建）
- core/internal/normalize/compaction.go（新建）
- core/internal/normalize/reasoning_ids.go（新建）
- core/internal/normalize/*_test.go（新建，与实现文件一一对应）
- core/internal/proxy/server.go（修改：在分流后调用整理函数；中台回复旁路记录推理 id；识别压缩请求后交给压缩接管）

## 函数清单
### core/internal/normalize/relay.go
| 函数名 | 职责 |
|---|---|
| `ForRelay` | 整理发往中台的请求体：去掉对方加密内容、展开本工具摘要、去掉非 function 工具；返回新体和是否改动 |
### core/internal/normalize/official.go
| 函数名 | 职责 |
|---|---|
| `ForOfficial` | 整理发往官方的请求体：去掉中台推理条目、展开本工具摘要；无改动时报告未改动 |
### core/internal/normalize/compaction.go
| 函数名 | 职责 |
|---|---|
| `IsCompactionTrigger` | 判断请求是否以 compaction_trigger 结尾 |
| `IsOwnCompaction` | 判断条目是否本工具生成的压缩摘要 |
| `BuildSummaryRequest` | 生成发给中台的总结请求体 |
| `ExtractSummary` | 从中台非流式回复里取摘要文字，空摘要报错 |
| `WriteCompactionStream` | 按规定事件顺序把压缩条目以 SSE 写给 Codex |
| `WriteCompactionJSON` | 单独压缩接口的 JSON 返回 |
### core/internal/normalize/reasoning_ids.go
| 函数名 | 职责 |
|---|---|
| `ReasoningIDStore` | 并发安全的中台推理 id 集合，带容量上限和持久化 |
| `ObserveSSE` | 包装中台回复流，边转发边记录 reasoning 条目 id，不改内容 |

## 协作关系
proxy.ServeHTTP 分流后：中台去向 → IsCompactionTrigger 为真走压缩接管（BuildSummaryRequest → 发中台 → ExtractSummary → WriteCompactionStream），否则 ForRelay 后转发，回复经 ObserveSSE；官方去向 → ForOfficial，有改动时用新体并去掉 Content-Encoding。外部依赖：中台 /responses 接口。

## 验证方式
- 测试入口：`cd core && go test ./internal/normalize/... ./internal/proxy/...`
- 测试输入：手写的 Codex 风格请求体 JSON（含官方加密 reasoning、中台 reasoning、本工具 compaction、OpenAI compaction、web_search 工具、function 工具）；假中台
- 预期结果：
- [ ] ForRelay：官方加密 reasoning 和 OpenAI compaction 被去掉，本工具摘要变成普通用户消息，web_search 被去掉，function 工具和可见消息保留
- [ ] ForOfficial：记录过的中台 reasoning 被去掉、摘要被展开、其余字节不变；不含两类内容时返回"未改动"
- [ ] 中台回复流经 ObserveSSE 后，客户端收到的字节和中台发出的完全一致，reasoning id 被记录；重启后集合仍在；超过 5000 个时最旧的被淘汰
- [ ] 以 compaction_trigger 结尾的中台请求 → 假中台收到一次非流式总结请求，其中没有 compaction_trigger；客户端收到规定顺序的 4 个事件，item 是可识别的本工具压缩条目
- [ ] 压缩时中台返回 500 → 客户端收到中性错误
- [ ] `/v1/responses/compact` + 中台模型 → 返回 `{"output":[compaction 条目]}`
