---
name: 01-core-relay-catalog
description: 核心程序的模型目录支持多个中转配置：每个配置单独获取、缓存，显示名「名字 · 模型」，内部名按配置编号区分
---
# 模型目录支持多个中转配置

## 目标
`catalog.Build` 接收多个中转配置，生成的模型目录里每个配置的模型都在，显示名是「<配置名> · <模型>」，内部名按配置编号区分。

## 业务规则（来源：PRD 03-multi-relay-config、01-rename-relay）
- 每个中转配置有一个不随名字变的编号（字符串，正整数的十进制，如 `"1"`、`"2"`）。内部名（slug）规则：编号 `"1"` 的模型是 `relay-<模型>`（和升级前一样，旧对话能继续）；编号 N（N≥2）的模型是 `relay<N>-<模型>`。两种前缀不会互相冲突。
- 显示名：`<配置名> · <模型>`；目录里中转模型的说明（description）写「中转模型」，不出现「中台」。
- 顺序：官方模型在前（「只用中转」时没有官方模型），中转模型按配置在列表里的顺序，再按每个配置原来的模型顺序（参数表模型在前、和官方同名的在后，与现在相同）。
- 每个配置单独获取模型列表（单次超时沿用 `RelayTimeout`）、单独缓存。某个配置获取失败：有缓存就沿用缓存并标记"没更新"，没缓存就没有它的模型；其他配置不受影响。
- 缓存文件按编号分开：`relay-models-<编号>.json`。编号 `"1"` 的缓存文件不存在、但旧的 `relay-models.json` 存在时，读旧文件作为它的缓存（升级迁移）。
- 调用方可以只刷新一个配置：传入"这次刚拉到的列表"的配置不再请求网络，其余配置用缓存，不发网络请求。
- 默认模型：按配置顺序第一个列表里有 `auto` 的配置，它的 auto 的内部名；都没有时为空。
- 需要给本机转发提供"内部名 → (配置编号, 原模型名, 是否参数表模型)"的完整对照。

## 涉及文件
- 修改 `core/internal/catalog/catalog.go`
- 修改 `core/internal/catalog/merge.go`
- 修改 `core/internal/catalog/store.go`
- 修改 `core/internal/catalog/catalog_test.go`、`merge_test.go`、`store_test.go`（如有）

## 函数清单
### core/internal/catalog/catalog.go
| 函数名 | 职责 |
|---|---|
| `RelaySource` | 一个中转配置的输入：编号、名字、Base URL、Key，以及可选的"刚拉到的列表" |
| `RelayResult` | 一个配置的结果：编号、可用模型数、获取时间、是否没更新、错误 |
| `Build` | 改为接收多个 `RelaySource`，逐个获取或读缓存，合并出目录，返回每个配置的结果、路由对照表和默认模型内部名 |

### core/internal/catalog/merge.go
| 函数名 | 职责 |
|---|---|
| `RelaySlug` | 按配置编号和模型名生成内部名 |
| `relayDisplayName` | 改为「<配置名> · <模型>」 |
| `applyRelayOverrides` | 改为接收配置编号和名字；description 写「中转模型」 |
| `MergeRelay` | 改为按配置分组合并，排序规则同上 |

### core/internal/catalog/store.go
| 函数名 | 职责 |
|---|---|
| `SaveRelayModels` / `LoadRelayModels` | 改为按配置编号读写；编号 "1" 读不到时退回旧文件 |

## 协作关系
`Build` → 每个 `RelaySource`：`FetchRelayIDs`（无刚拉到的列表、且需要刷新时）或 `Store.LoadRelayModels` → `RelayNames` → `MergeRelay` → `Store.WriteCatalog`。调用方是 `control.Service`（另一个 plan 修改）和本机转发的路由表。

## 验证方式
- 测试入口：`cd core && go test ./internal/catalog/...`；包级入口 `catalog.Build`、`catalog.RelaySlug`。
- 测试输入：临时数据目录、假的官方模型清单、用 `httptest` 起两个假中转（各返回模型列表，其中都含 `auto`）。
- 预期结果：
  - [ ] 两个配置（编号 "1" 名「中转」、编号 "2" 名「A家」）→ 目录里有 `relay-auto`（显示「中转 · auto」）和 `relay2-auto`（显示「A家 · auto」），顺序为官方、配置 1、配置 2；description 是「中转模型」，目录文件里搜不到「中台」。
  - [ ] 「只用中转」→ 没有官方模型。
  - [ ] 第二个假中转返回 500 且有缓存 → 它的模型沿用缓存，结果标没更新、带错误码；第一个配置照常。没缓存 → 目录里没有它的模型。
  - [ ] 只给配置 2 传"刚拉到的列表" → 不向任何假中转发请求。
  - [ ] 数据目录只有旧的 `relay-models.json` → 编号 "1" 读到它的内容。
  - [ ] 默认模型：配置 1 有 auto → `relay-auto`；配置 1 没有、配置 2 有 → `relay2-auto`；都没有 → 空。
  - [ ] 路由对照表包含每个内部名对应的编号和原模型名。
