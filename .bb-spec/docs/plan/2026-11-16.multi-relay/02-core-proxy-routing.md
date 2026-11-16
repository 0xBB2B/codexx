---
name: 02-core-proxy-routing
description: 本机转发按内部名查表，把请求发往对应中转配置；已删除配置的模型给出规定错误；错误文字改称「中转」
---
# 本机转发按配置分流

## 目标
本机转发收到请求后，按请求里的模型内部名找到所属配置，用该配置的 Base URL 和 Key 转发；找不到时按规则拒绝或走官方。

## 业务规则（来源：PRD 03-multi-relay-config、01-rename-relay）
- 内部名规则：配置编号 "1" 是 `relay-<模型>`，编号 N≥2 是 `relay<N>-<模型>`。转发时按完整内部名查对照表（内部名 → 配置的 Base URL、Key、原模型名、是否参数表模型），不靠去前缀猜。
- 命中对照表：发往该配置，请求体里的模型换回原模型名，带该配置的 Key；参数表模型照旧把 reasoning.effort 的 ultra 改回 max。
- 内部名符合中转内部名的格式（`relay-` 开头，或 `relay` + 数字 + `-` 开头）但不在对照表里：请求不发往任何服务，返回给 Codex 的错误文字是「这个模型所在的中转配置已删除，请换一个模型。」，日志里去向记为 rejected。
- 其他模型：「官方 + 中转」方式下走官方；「只用中转」方式下拒绝，错误文字「当前没有登录 ChatGPT，官方模型不可用，请切换到中转模型。」
- 本工具返回给 Codex 的其他错误文字里的「中台」都换成「中转」。
- 中转返回的错误原文仍不显示、不写日志（项目约束）。

## 涉及文件
- 修改 `core/internal/proxy/route.go`
- 修改 `core/internal/proxy/server.go`
- 修改 `core/internal/proxy/relay_errors.go`
- 修改对应测试：`route_test.go`、`relay_errors_test.go`、`server_test.go`

## 函数清单
### core/internal/proxy/route.go
| 函数名 | 职责 |
|---|---|
| `RelayTarget` | 一个内部名对应的去向：Base URL、Key、原模型名、是否参数表模型 |
| `RelayConfig` | 改为：内部名 → `RelayTarget` 的对照表，加「只用中转」标记 |
| `relayState.set` | 保存对照表 |
| `decideRoute` | 按完整内部名查表决定去向；识别"像中转内部名但查不到"的情况 |
| `looksLikeRelaySlug` | 判断内部名是否符合中转内部名格式 |

### core/internal/proxy/relay_errors.go
| 函数名 | 职责 |
|---|---|
| `relayDeletedError` | 已删除配置的错误体 |
| 现有错误文字 | 「中台」改「中转」 |

### core/internal/proxy/server.go
| 函数名 | 职责 |
|---|---|
| `SetRelay` | 接收新的 `RelayConfig` |
| 请求处理处 | 对新的拒绝原因返回 `relayDeletedError` |

## 协作关系
`control.Service` 在每次目录更新后调用 `Server.SetRelay`，对照表来自 `catalog.Build` 的结果。

## 验证方式
- 测试入口：`cd core && go test ./internal/proxy/...`；包级入口 `proxy.Server`（`httptest` 起假官方和两个假中转）。
- 预期结果：
  - [ ] 对照表有 `relay-auto`（中转 A）、`relay2-auto`（中转 B）→ 两个请求分别到达 A、B，请求体模型都是 `auto`，Authorization 分别是 A、B 的 Key。
  - [ ] 请求 `relay3-auto`（不在表里）→ 不向任何服务发请求，返回的错误 message 是「这个模型所在的中转配置已删除，请换一个模型。」
  - [ ] 「只用中转」下请求 `gpt-x` → 返回「当前没有登录 ChatGPT，官方模型不可用，请切换到中转模型。」
  - [ ] 「官方 + 中转」下请求 `gpt-x` → 发往官方。
  - [ ] proxy 包源码和错误体里搜不到「中台」。
