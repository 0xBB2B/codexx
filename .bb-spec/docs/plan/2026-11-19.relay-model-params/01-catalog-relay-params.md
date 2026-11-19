---
name: 01-catalog-relay-params
description: catalog 包从中转模型列表接口读取参数、连参数一起缓存、按接口参数生成中转目录条目，删除内置参数表
---
# catalog 包：中转模型参数取自接口

## 目标
`catalog.Build` 生成的合并模型目录里，每个中转模型的上下文、思考等级、输入类型都来自中转模型列表接口（或其缓存）；包内不再有内置参数表。

## 业务规则（来源：PRD 01-relay-params-from-api）
- 范围：接口返回的每个模型（`id` 非空）都生成一条中转目录条目，不再要求"和官方同名或在参数表里"。
- 参数来源（`auto` 除外）：上下文窗口、`max_context_window` = `context_length`；`input_modalities` = `architecture.input_modalities`；思考等级 = `reasoning.supported_efforts`，按接口顺序，只保留 none、minimal、low、medium、high、xhigh、max 七个名字，其余丢弃。和官方同名的模型同样用接口值。
- `auto`：不管接口给什么，上下文 1000000、输入 `["text","image"]`、没有思考等级。
- 缺参数（`auto` 除外）。"缺"的含义：字段不存在、`context_length` 不是正整数、`input_modalities` 为空列表。缺哪项补哪项：
  - 和官方同名：缺的那项用同名官方条目的值；缺思考等级时，`supported_reasoning_levels` 和 `default_reasoning_level` 都用官方条目的。
  - 不同名：上下文和输入类型用官方清单第一个条目的值；缺思考等级就设为没有思考等级。
- `reasoning.supported_efforts` 存在但为空列表，或过滤后为空：算"有这个字段、但没有等级"，不算缺。
- 默认思考等级（思考等级取自接口时）：有 high 就选 high；没有 high，就按 none<minimal<low<medium<high<xhigh<max 取列表中最高的一档。列表为空时：删掉 `default_reasoning_level`，`supports_reasoning_effort_updates` 设为 false。
- 思考等级取自接口时，`max` 在目录里写成 `ultra`，默认等级同样换算；这条路由要标记"发送时把 ultra 改回 max"。思考等级取自官方条目时，等级名原样，路由不标记。
- 显示：同名官方条目是隐藏的（`visibility` 存在且不是 `list`），中转条目就沿用这个隐藏状态，进 `Routes` 和 `RelayModels`，不进 `RelayShown`。其他中转条目设 `visibility: "list"`，进 `RelayShown`。
- 顺序：按配置顺序；同一个配置内按接口返回顺序，`priority` 依次连续加 1（和现在一样，从官方最大 priority + 1 开始）。
- `Result.ReasoningLevels`：所有中转条目最终写进目录的等级名并集，按首次出现的顺序。
- 缓存：获取成功时，把模型名和参数一起写入 `relay-models-<id>.json`。读缓存时，有 `models` 字段就用它；只有 `ids` 字段的旧格式，每个模型按"全部缺参数"处理。编号 1 的旧版单文件 `relay-models.json` 的回退读取逻辑保留不变。
- 条目的其余字段，规则不变：同名的克隆同名官方条目，不同名的克隆官方第一个条目，然后套用 `applyRelayOverrides`。

## 涉及文件
- 修改 `core/internal/catalog/relay.go`
- 修改 `core/internal/catalog/store.go`
- 修改 `core/internal/catalog/merge.go`
- 修改 `core/internal/catalog/catalog.go`
- 删除 `core/internal/catalog/params.go`、`core/internal/catalog/params.json`、`core/internal/catalog/params_test.go`
- 修改 `core/internal/catalog/*_test.go`（删掉依赖参数表的用例，按新规则重写）

## 成品定义
缓存文件 `relay-models-<id>.json` 的新格式。`reasoning_efforts` 为 `null` 表示接口没给这个字段；`[]` 表示接口给了、但没有等级。`context_length` 为 0、`input_modalities` 为 `null` 表示没给：

```json
{
  "fetched_at": "2026-11-19T08:00:00Z",
  "models": [
    {"id": "model-a", "context_length": 1048576, "input_modalities": ["text"], "reasoning_efforts": ["low", "high", "max"]},
    {"id": "auto", "context_length": 0, "input_modalities": null, "reasoning_efforts": null}
  ]
}
```

旧格式（只读不写）：`{"fetched_at": "...", "ids": ["model-a", "auto"]}`

## 函数清单
### core/internal/catalog/relay.go
| 函数名 | 职责 |
|---|---|
| `RelayModel`（类型） | 一个中转模型：ID、上下文、输入类型、思考等级（nil 表示接口没给） |
| `FetchRelayModels` | 取代 `FetchRelayIDs`：请求 `/models`，解析每个模型的 id、`context_length`、`architecture.input_modalities`、`reasoning.supported_efforts`。错误码和现在完全一样 |

### core/internal/catalog/store.go
| 函数名 | 职责 |
|---|---|
| `SaveRelayModels` | 改为保存 `[]RelayModel`，写成新格式 |
| `LoadRelayModels` | 改为返回 `[]RelayModel`；读到旧格式（只有 ids）时，转成全部缺参数的 RelayModel |

### core/internal/catalog/merge.go
| 函数名 | 职责 |
|---|---|
| `RelayGroup` | 字段改成 ID、Name、`Models []RelayModel`，去掉 ParamOnly 和 SameName |
| `MergeRelay` | 按上面的规则，为每个配置的每个模型生成条目、路由、汇总 |
| `relayEntry` | 新增：为单个模型确定基底条目，并写入上下文、输入类型、思考等级、默认等级、可见性 |
| `relayLevels` | 新增：过滤未知等级名，把 max 换成 ultra，算出默认等级 |
| `ShownRelayNames` | 改为接收 `[]RelayModel` 和官方条目，返回不隐藏的中转原名，按接口顺序 |
| `RelayRoute` | `FromParams` 改名为 `UltraToMax`，含义是"思考等级取自接口" |
| `Result` | 删除 `RelayFromParams` 字段 |
| `RelayNames` | 删除 |

### core/internal/catalog/catalog.go
| 函数名 | 职责 |
|---|---|
| `RelaySource` | `IDs []string` 改成 `Models []RelayModel` |
| `BuildOptions` | 删除 `Params` 字段 |
| `Build` | 改用 `FetchRelayModels` 和新的缓存读写；`ModelCount` = 该配置不隐藏的条目数 |
| `loadCached` | 返回 `[]RelayModel` |

## 协作关系
`Build` → `FetchRelayModels`，失败时走 `loadCached` → `Store.LoadRelayModels`。拿到模型后 → `Store.SaveRelayModels` → `MergeRelay` → `relayEntry` → `relayLevels`。外部依赖：中转的 `GET <BaseURL>/models`；官方清单来自 `LoadOfficial`。

## 验证方式
- 测试入口：`catalog.Build`（用测试桩 codex 和 `httptest` 中转桩）、`catalog.FetchRelayModels`、`catalog.MergeRelay`、`catalog.ShownRelayNames`、`Store.SaveRelayModels`/`LoadRelayModels`。
- 测试输入：中转桩返回带参数和不带参数的模型。官方桩里有一个同名可见模型、一个同名隐藏模型（visibility `hide`）。
- 预期结果：
- [ ] 接口值 1048576、`["low","high","max"]`、`["text"]` → 条目 `context_window`=`max_context_window`=1048576；等级依次是 low、high、ultra；默认 high；`input_modalities`=`["text"]`；路由 `UltraToMax`=true。
- [ ] 同名模型：接口给 1050000，官方条目是 272000 → 条目是 1050000，等级来自接口。
- [ ] `["low","turbo","medium"]` → 等级 low、medium，默认 medium。`["none","low","medium","xhigh"]` → 默认 xhigh。`[]` → 没有 `default_reasoning_level`，`supports_reasoning_effort_updates`=false。
- [ ] `auto` 接口给 500，或什么都没给 → 1000000，`["text","image"]`，没有等级。
- [ ] 同名模型全部缺参数 → 上下文、输入、等级、默认等级等于官方条目，等级里的 max 和 ultra 原样保留；路由 `UltraToMax`=false。
- [ ] 不同名模型 `context_length` 为 0 → 用官方第一个条目的上下文；只缺思考等级 → 没有等级。
- [ ] 和官方隐藏模型同名 → 条目 visibility 是 `hide`，在 `Routes` 里、不在 `RelayShown` 里，`ModelCount` 不计它。
- [ ] 一个本包里从没出现过的模型名 → 出现在目录里。
- [ ] 两个配置分别返回 `[m2,m1]` 和 `[m3]` → 中转部分的顺序是 m2、m1、m3。
- [ ] 成功获取后再失败 → 目录里的中转参数和成功那次相同。只有旧格式缓存时 → 模型都在，按缺参数补。
- [ ] `go test ./internal/catalog/...` 全部通过；包内没有 params.json 和 `BuiltinParams`。
