---
name: 03-relay-only-catalog
description: 合并模型目录支持只放中台模型，中台条目和「官方 + 中台」时完全相同
---
# 只放中台模型的模型目录

## 目标
`catalog.Build` 能生成只含中台模型的目录，供「只用中台」方式使用。

## 业务规则（来源：PRD 08-access-mode C6，02-model-routing C7~C9、C20~C22）
- 「只用中台」方式下，写出的模型目录里只有中台模型，没有任何官方模型条目。
- 每个中台条目（内部名 `relay-<原名>`、显示名「中台 · 原名」、上下文、思考等级、max→ultra、图片输入、优先级数值）和「官方 + 中台」方式下目录里中台部分逐项相同，顺序也相同。
- 中台条目仍以 ChatGPT 自带的官方模型清单为底板生成，所以官方清单读不出来时照常报错（和现在一样）。
- 「只用中台」不按账号筛选官方模型，不读取 `models_cache.json`。
- 返回结果里的中台模型名单、可见数量、参数表来源名单、思考等级、获取时间和获取错误，和「官方 + 中台」方式下完全相同。
- 不带新选项时行为和现在完全一致。

## 涉及文件
- 修改 `core/internal/catalog/catalog.go`
- 修改 `core/internal/catalog/catalog_test.go`

## 函数清单
### core/internal/catalog/catalog.go
| 函数名 | 职责 |
|---|---|
| `BuildOptions`（结构体） | 新增字段 `RelayOnly` |
| `Build` | `RelayOnly` 时写出的目录只含中台条目，跳过账号筛选；其余字段计算不变 |

## 协作关系
- 复用现有 `LoadOfficial`、`RelayNames`、`MergeRelay`、`ShownRelayNames`、`Store.WriteCatalog`。
- 调用方：`control.Service.rebuild`（plan 05）按当前接入方式传入。

## 验证方式
- 测试入口：`catalog.Build`（`go test ./internal/catalog`），沿用现有测试里的假 codex 程序和假中台。
- 测试输入：官方清单含 `gpt-a`、`gpt-b`；中台返回 `glm-5.3`（参数表里有）、`gpt-a`（和官方同名）、`unknown-x`（两边都没有）；`ModelsCachePath` 指向只含 `gpt-a` 的文件。
- 预期结果与验证项：
- [ ] `RelayOnly: true`：写出的目录文件 `models` 只有 `relay-glm-5.3`、`relay-gpt-a` 两项，没有 `gpt-a`、`gpt-b`。
- [ ] 同样输入分别用 `RelayOnly` 为 true 和 false 构建：前者目录的全部条目，和后者目录里 slug 以 `relay-` 开头的条目逐项相等（JSON 深度相等、顺序相同）。
- [ ] 两次构建返回的 `RelayModels`、`RelayShown`、`RelayFromParams`、`ReasoningLevels` 相同。
- [ ] `RelayOnly: true` 且 `ModelsCachePath` 指向不存在的文件：结果和上一条相同。
- [ ] 假 codex 返回失败：`RelayOnly: true` 时 `Build` 返回 `chatgpt_unsupported` 错误。
- [ ] `RelayOnly: false` 时现有全部测试保持通过。
