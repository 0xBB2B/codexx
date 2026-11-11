---
name: 03-core-proxy-routing
description: 本机转发服务：仅本机连接、按模型分流、重建中台请求头、zstd 解压、流式转发、中台错误中性化
---
# 核心：本机转发与分流

## 目标
核心程序在 `127.0.0.1:47311` 提供转发服务，Codex 的请求按模型分到官方服务或中台，官方凭据不会到中台。

## 业务规则（来源：PRD 02-model-routing）
- 只接受来源地址是 127.0.0.1 或 ::1 的连接，其他一律 403。
- 路径前缀 `/v1`（Codex 的 `openai_base_url` 写成 `http://127.0.0.1:47311/v1`），另有 `/_health` 返回 `{"ok":true}`。
- 请求体按 `Content-Encoding: zstd` 解压后读 `model` 字段（解压上限 64MB）。
- `model` 属于当前中台模型集合 → 发往 `<Base URL>` + 去掉 `/v1` 前缀后的路径。清空原请求头，只保留 Accept、Content-Type、OpenAI-Beta、User-Agent，再加 `Authorization: Bearer <中台 Key>`；请求体用解压后的内容，不带 Content-Encoding。
- 其他请求 → 带 `ChatGPT-Account-ID` 头的发往 `https://chatgpt.com/backend-api/codex`，不带的发往 `https://api.openai.com/v1`。请求头原样；请求体原样（含原压缩格式），只有在「请求整理」计划判定需要改动时才用改动后的内容，这时去掉 Content-Encoding。
- WebSocket 升级请求返回 426，让 Codex 改用 HTTP。
- 上游回复立即逐段转给 Codex（不缓冲）。
- 中台返回 ≥400 或连不上时，丢弃中台正文，返回给 Codex 的错误只含中性说明和状态码：401「中台服务返回 401：API Key 无效或已过期」、403「中台服务返回 403：API Key 没有权限」、404「中台服务返回 404：Base URL 不正确」、429「中台服务返回 429：请求太频繁，请稍后重试」、5xx「中台服务返回 <码>：中台服务暂时不可用」、连不上「连不上中台，请检查网络和 Base URL」。官方的错误原样转发。
- 不保存、不记录任何请求头的值；日志只记：时间、去向（official/relay）、状态码、耗时，不记模型名。
- 中台 Key 只在内存里，由控制层注入。

## 涉及文件
- core/internal/proxy/server.go（新建）
- core/internal/proxy/route.go（新建）
- core/internal/proxy/relay_errors.go（新建）
- core/internal/proxy/body.go（新建）
- core/internal/proxy/*_test.go（新建，与实现文件一一对应）

## 成品定义

返回给 Codex 的错误格式（中台错误和 403/426 都用它）：
```json
{ "error": { "message": "中台服务返回 401：API Key 无效或已过期", "type": "model_bridge_error", "code": "relay_401" } }
```

## 函数清单
### core/internal/proxy/server.go
| 函数名 | 职责 |
|---|---|
| `NewServer` | 用路由状态、上游地址、日志器创建转发服务 |
| `Server.Start` | 监听 127.0.0.1:47311，端口被占用时返回 `proxy_port_in_use` |
| `Server.Stop` | 停止监听并等待进行中的请求结束（最多 5 秒） |
| `Server.ServeHTTP` | 校验本机来源、处理 `/_health`、426、读请求体、分流、转发 |
| `isLoopback` | 判断来源地址是否本机 |
### core/internal/proxy/route.go
| 函数名 | 职责 |
|---|---|
| `RouteState` | 并发安全地保存当前中台模型集合、Base URL、中台 Key，可整体替换 |
| `decideRoute` | 根据 model 和请求头得出去向和目标地址 |
| `rewriteRelayHeaders` | 清空并重建发往中台的请求头 |
### core/internal/proxy/body.go
| 函数名 | 职责 |
|---|---|
| `readBody` | 读原始请求体并按 zstd 解压，超过上限报错 |
| `extractModel` | 从 JSON 请求体取 model 字段 |
### core/internal/proxy/relay_errors.go
| 函数名 | 职责 |
|---|---|
| `neutralRelayError` | 按状态码或网络错误生成中性错误 JSON |

## 协作关系
ServeHTTP → readBody → extractModel → decideRoute →（中台：rewriteRelayHeaders）→ 调用「请求整理」计划的整理函数（本计划先直接透传，整理函数接口为"输入去向和请求体，返回新请求体和是否改动"）→ httputil.ReverseProxy（FlushInterval -1）→ 中台失败时 neutralRelayError。外部依赖：官方两个地址、中台 Base URL。

## 验证方式
- 测试入口：`cd core && go test ./internal/proxy/...`
- 测试输入：httptest 假官方（两个地址各一个）、假中台；用 httptest 客户端向转发服务发请求；用非本机地址模拟来源
- 预期结果：
- [ ] 非本机来源 → 403
- [ ] 中台模型请求 → 假中台收到的请求头只有规定 4 个加中台 Authorization；带了 Cookie、ChatGPT-Account-ID、官方 Authorization 的原请求，这些值都没到中台
- [ ] 带 ChatGPT-Account-ID 的官方请求到 chatgpt 地址，不带的到 api 地址；请求头、请求体逐字节一致（含 zstd 压缩体）
- [ ] zstd 压缩的中台请求 → 模型识别正确，中台收到解压后的体且无 Content-Encoding
- [ ] WebSocket 升级 → 426
- [ ] 假中台每 500ms 发一段 SSE → 客户端每段到达间隔约 500ms
- [ ] 假中台返回带特殊字符串的 401/403/404/429/500 和断开连接 → 客户端收到规定的中性错误，特殊字符串在响应和日志里都找不到
- [ ] 日志文件里搜不到任何请求头的值和模型名
- [ ] 端口被占用时 Start 返回 `proxy_port_in_use`
