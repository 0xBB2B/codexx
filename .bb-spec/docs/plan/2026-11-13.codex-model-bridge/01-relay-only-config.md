---
name: 01-relay-only-config
description: Codex 配置写入支持「只用中台」接入项：注册本工具自己的模型服务，不写 openai_base_url
---
# 「只用中台」的 Codex 配置写入

## 目标
`codexconfig.Enable` 能按「只用中台」方式写入接入项，关闭转发时照常逐项还原。

## 业务规则（来源：PRD 08-access-mode C5、C9，03-forwarding-toggle C1~C5）
- 「只用中台」方式下，在 Codex 用户配置文件里写入：
  - 根上 `model_provider = "modelbridge"`；
  - 模型服务表的三项：`model_providers.modelbridge.name = "模型接入助手"`、`model_providers.modelbridge.base_url = "http://127.0.0.1:47311/v1"`、`model_providers.modelbridge.wire_api = "responses"`（配置里已有 `[model_providers.modelbridge]` 表头时写进表里，没有时以点号键写在根上，两种写法 TOML 语义相同）；
  - 根上 `model_catalog_json`；中台有 `auto` 时根上 `model = "relay-auto"`。
  - 不写 `openai_base_url`，不写 `[desktop] enabled-reasoning-efforts`。配置文件里其他内容一律不改。
- 「官方 + 中台」方式（现有行为）完全不变：写 `openai_base_url`、`model_catalog_json`、`model`、思考等级，移除 `model_provider`。
- 每个写入项都和现有接入项一样记录原值（原来没有就记"原来没有"）；配置里已有不是本工具写的同名项时备份原值再覆盖，算作"接管"，返回在接管列表里。
- 关闭转发（`Restore`）时逐项还原；被手改过的项不动并报 `restore_blocked`（现有逻辑覆盖新增项）。
- 写入或还原后配置文件必须能被正常解析；写入中途失败，配置文件保持原样（沿用现有原子写入）。
- 切换接入方式由调用方用"先 `Restore` 再按新方式 `Enable`"完成，结果必须和"关闭转发后按新方式打开"逐字节一致；本 plan 保证 `Restore` 之后再 `Enable` 能正常工作，不需要在 `Enable` 内部处理"上一次是另一种方式"的情况。

## 涉及文件
- 修改 `core/internal/codexconfig/apply.go`
- 修改 `core/internal/codexconfig/apply_test.go`

## 函数清单
### core/internal/codexconfig/apply.go
| 函数名 | 职责 |
|---|---|
| `EnableInput`（结构体） | 新增字段 `RelayOnly`，为 true 时按「只用中台」写入 |
| `Enable` | 按 `RelayOnly` 选择要管理和写入的接入项；「只用中台」写入 provider 与模型服务表三项，不写 `openai_base_url` 和思考等级 |
| `typeOK` | 新增的几项都按字符串校验类型 |

新增导出常量 `ProviderID = "modelbridge"`，供本机转发与服务层引用同一个名字。

## 协作关系
- `Enable` 复用现有 `get` / `setString` / `removeKey` / `record` / `SaveState` / `atomicWrite`；`Restore` 不改，靠状态文件里的条目还原新增项。
- 调用方：`control.Service`（plan 05）在打开转发和切换接入方式时调用。

## 验证方式
- 测试入口：包级函数 `codexconfig.Enable`、`codexconfig.Restore`、`codexconfig.TakenOverKeys`（`go test ./internal/codexconfig`）。
- 测试输入：临时目录里的 config.toml（含无关设置 `sandbox = "read-only"`、可选的 `model_provider = "custom"`、可选的 `[model_providers.modelbridge]` 表），`EnableInput{CatalogPath, HasAutoModel, RelayOnly: true}`。
- 预期结果与验证项：
- [ ] RelayOnly 写入后：配置能被 TOML 解析，`model_provider == "modelbridge"`，`model_providers.modelbridge` 的 name/base_url/wire_api 为规定值，`model_catalog_json` 为传入路径，HasAutoModel 时 `model == "relay-auto"`；不存在 `openai_base_url` 和 `desktop.enabled-reasoning-efforts`；`sandbox` 行原样保留。
- [ ] RelayOnly 写入后 `Restore`：配置文件和写入前逐字节一致（含原来没有配置文件的情况：还原后文件不存在）。
- [ ] 原配置有 `model_provider = "custom"`：RelayOnly 写入后返回的接管列表含 `model_provider`，`Restore` 后恢复为 `"custom"`。
- [ ] 原配置已有 `[model_providers.modelbridge]` 表且含 `base_url = "http://other.example.com"`：写入后值被覆盖并记为接管，`Restore` 后恢复原值。
- [ ] 先按「官方 + 中台」`Enable`，再 `Restore`，再按 RelayOnly `Enable`：结果和直接按 RelayOnly `Enable` 逐字节一致；反方向同理。
- [ ] RelayOnly 写入后手动把 `model_providers.modelbridge.base_url` 改掉再 `Restore`：返回 `restore_blocked` 且 Keys 含该项，该项保持手改值，其他项已还原。
- [ ] `RelayOnly: false` 时现有全部测试保持通过（行为不变）。
