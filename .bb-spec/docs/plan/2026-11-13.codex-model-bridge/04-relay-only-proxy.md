---
name: 04-relay-only-proxy
description: 「只用中台」时本机转发拒绝非中台请求，返回中文提示，不发往任何服务
---
# 「只用中台」时拒绝官方请求

## 目标
本机转发服务在「只用中台」模式下，对不属于中台模型的请求直接返回规定的错误，不转发到官方服务或中台。

## 业务规则（来源：PRD 08-access-mode C7，02-model-routing C2、C3）
- 「只用中台」模式下，请求里的模型属于本次的中台模型（带 `relay-` 前缀、去掉前缀后在中台名单里）→ 和现在完全一样转发到中台。
- 其他请求（官方模型名、没有模型字段、不认识的模型）→ 不发往任何服务，直接返回 HTTP 400，`Content-Type: application/json`，正文：

```json
{"error":{"message":"当前没有登录 ChatGPT，官方模型不可用，请切换到中台模型。","type":"model_bridge_error","code":"official_unavailable"}}
```

- 日志照常一行，去向记为 `rejected`，不记录请求头和正文。
- 「官方 + 中台」模式（默认）行为完全不变。
- 模式可以在服务运行中切换，切换后下一个请求立即按新模式处理。
- 健康检查、仅本机连接、WebSocket 返回 426 等现有规则不变，且先于这条规则判断。

## 涉及文件
- 修改 `core/internal/proxy/route.go`
- 修改 `core/internal/proxy/relay_errors.go`
- 修改 `core/internal/proxy/server.go`
- 修改 `core/internal/proxy/server_test.go`

## 函数清单
### core/internal/proxy/route.go
| 函数名 | 职责 |
|---|---|
| `RelayConfig`（结构体） | 新增字段 `RelayOnly` |
| `relayState.set` | 一并保存 `RelayOnly` |
| `decideRoute` | 返回的路由带上"应拒绝"标记：`RelayOnly` 且不是中台模型 |

### core/internal/proxy/relay_errors.go
| 函数名 | 职责 |
|---|---|
| `officialUnavailableError` | 给出上面规定的错误码和中文文字 |

### core/internal/proxy/server.go
| 函数名 | 职责 |
|---|---|
| `ServeHTTP` | 路由带"应拒绝"标记时写 400 和规定正文，去向记 `rejected`，不调用转发 |

## 协作关系
- 正文用现有 `relayErrorBody` 生成。
- 调用方：`control.Service.pushRelay`（plan 05）按当前接入方式设置 `RelayOnly`。

## 验证方式
- 测试入口：`proxy.New(...).Handler()` 配 `httptest`，用 `SetRelay` 切换模式；假官方服务和假中台记录收到的请求数。
- 测试输入：`SetRelay(RelayConfig{Models: ["glm-5.3"], BaseURL: 假中台, Key: "sk-example-0000", RelayOnly: true})`，分别发 `POST /v1/responses` 模型为 `relay-glm-5.3`、`gpt-a`、无 model 字段、带 `ChatGPT-Account-ID` 头的 `gpt-a`。
- 预期结果与验证项：
- [ ] `relay-glm-5.3` → 假中台收到 1 次请求，模型名为 `glm-5.3`，客户端收到中台的回复。
- [ ] `gpt-a`、无 model 字段、带账号头的 `gpt-a` → 客户端收到 400 和上面规定的 JSON 正文（逐字节相等）；假官方服务和假中台都没有收到请求。
- [ ] 日志输出里这几条的去向是 `rejected`，日志里找不到请求头里的值。
- [ ] 运行中 `SetRelay` 改成 `RelayOnly: false` 后再发 `gpt-a` → 转发到假官方服务（现有行为）。
- [ ] `RelayOnly: true` 时 `/_health` 仍返回 `{"ok":true}`，WebSocket 升级仍返回 426。
- [ ] 现有 proxy 测试全部保持通过。
