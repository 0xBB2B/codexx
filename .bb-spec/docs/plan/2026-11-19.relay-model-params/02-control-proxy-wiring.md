---
name: 02-control-proxy-wiring
description: control 服务和 proxy 改用接口参数：保存配置、探测计数、重建目录不再读参数表，ultra→max 只对接口等级生效
---
# control 与 proxy 接入接口参数

## 目标
主程序保存配置、刷新模型、转发关着时的计数，都基于接口返回的模型和参数。转发服务只在思考等级取自接口时，把 ultra 改回 max。

## 业务规则（来源：PRD 01-relay-params-from-api）
- 保存配置（`save_relay`）时获取到的模型和参数，原样交给目录生成，并写入缓存。
- 主窗口的模型数 = 模型选择器里可见的中转模型数。和官方隐藏模型同名的不算。转发开着和关着时，同样的数据算出同样的数。
- 转发关着、本机又找不到 ChatGPT 时，没有官方清单，无法判断哪些隐藏，就把接口返回的模型全部算进去。
- 默认模型规则不变：第一个有 `auto` 的配置的 `relay-auto`。
- 思考等级取自接口的中转模型：请求里的 `reasoning.effort` 是 `ultra` 时，改成 `max` 再发；思考等级取自官方条目的，原样发。
- 程序里不再加载内置参数表，加载失败的分支一并删除。

## 涉及文件
- 修改 `core/internal/control/service.go`
- 修改 `core/internal/proxy/route.go`、`core/internal/proxy/server.go`（只改字段名）
- 修改 `core/internal/control/service_test.go`、`core/internal/proxy/*_test.go`、`core/cmd/model-bridge-core/main_test.go`（按新 API 调整，删掉依赖参数表的断言）

## 函数清单
### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `SaveRelay` | 改用 `catalog.FetchRelayModels`，把结果放进 `relayPlan.models` |
| `relayPlan` | `ids map[string][]string` 改成 `models map[string][]catalog.RelayModel` |
| `cacheAll` / `fetchAll` | 跟着字段改名 |
| `probe` | 去掉 `BuiltinParams`；改用 `FetchRelayModels`、新的缓存读写、`catalog.ShownRelayNames` 计数；找不到 ChatGPT 时传空官方清单，结果是全部算可见 |
| `rebuild` | 去掉 `BuiltinParams` 和 `Params`；`RelaySource.Models` 取自 plan |
| `pushRelay` | 把 `RelayRoute.UltraToMax` 传给 `proxy.RelayTarget.UltraToMax` |

### core/internal/proxy/route.go
| 函数名 | 职责 |
|---|---|
| `RelayTarget` | `FromParams` 改名 `UltraToMax`，注释改成"思考等级取自中转接口时为真" |
| `resolve`（现有路由解析函数） | 用 `t.UltraToMax` 设置 `ultraToMax` |

## 协作关系
`SaveRelay`、`refresh_models`、启动流程 → `refreshRelay` → 转发开着走 `rebuild`（调 `catalog.Build`），关着走 `probe`。`pushRelay` → `proxy.SetRelay` → 发请求时 `relayBody` 按 `ultraToMax` 改写推理强度。依赖 01 提供的 `catalog.RelayModel`、`FetchRelayModels`、`Store.SaveRelayModels`/`LoadRelayModels`、`ShownRelayNames`、`RelayRoute.UltraToMax`。

## 验证方式
- 测试入口：control 包的 `Service` 公开方法（`Init`、`SaveRelay`、`RefreshModels`、`Status` 等现有测试用的入口）、proxy 包的 `Server` 处理请求的入口。
- 测试输入：中转桩返回带参数的模型、`auto`、和官方隐藏模型同名的模型；有测试桩 codex 和没有测试桩 codex 两种情况。
- 预期结果：
- [ ] 中转返回 `[model-a, auto, review-x]`，其中 `review-x` 和官方隐藏模型同名，转发开着 → `Status` 里该配置的 `model_count`=2，默认模型是 `relay-auto`。
- [ ] 同样数据、转发关着 → `model_count`=2。找不到 ChatGPT 时 → `model_count`=3。
- [ ] 中转返回一个以前从没见过的模型名，`save_relay` → `model_count` 计入它，生成的目录里有它。
- [ ] 路由 `UltraToMax`=true，请求 effort 为 `ultra` → 中转桩收到 `max`。`UltraToMax`=false → 收到 `ultra`。
- [ ] `go build ./...` 和 `go test ./...`（在 `core/` 下）全部通过；`grep -rn BuiltinParams core` 没有结果。
