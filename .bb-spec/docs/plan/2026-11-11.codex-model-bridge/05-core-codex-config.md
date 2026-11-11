---
name: 05-core-codex-config
description: Codex 配置接入项的写入、还原、手改检测、备份接管与原子写入
---
# 核心：Codex 配置接入项

## 目标
核心程序能只改 Codex 配置里的接入项，并能准确还原；接入项被手改时不覆盖，并报告是哪一项。

## 业务规则（来源：PRD 03-forwarding-toggle、05-uninstall）
- 配置文件：macOS `~/.codex/config.toml`，Windows `%USERPROFILE%\.codex\config.toml`（环境变量 CODEX_HOME 有值时用 `$CODEX_HOME/config.toml`）。
- 接入项（只动这些，其他字节一律不变，用户注释和格式保留）：
  - 根 `openai_base_url` = `"http://127.0.0.1:47311/v1"`
  - 根 `model_catalog_json` = 合并目录的绝对路径
  - 根 `model` = `"auto"`（仅中台模型含 auto 时写，否则不碰）
  - `[desktop]` 表的 `enabled-reasoning-efforts`：在原列表末尾追加中台模型需要但缺少的等级，原有的一个不删；原来没有这一项时，新建的列表 = 中台模型需要的等级（不需要任何等级时不写）
  - 根 `model_provider`：移除
- 开启前记录每一项的原值（没有就记"原来没有"）和本工具写入的值，保存在数据目录 `access-state.json`。已有不是本工具写的 `openai_base_url`、`model_catalog_json`、`model_provider` 时，记为"已接管"并返回接管的键名，供界面提示。
- 首次开启前，若 `<配置目录>/config.toml.before-model-bridge` 不存在，把当时整份配置复制过去（配置不存在则写空文件）。这份备份卸载时也不删。
- 还原：逐项检查当前值是否等于本工具写入的值。全部相等 → 逐项恢复原值（原来没有的删掉），删除 access-state.json。有不相等的 → 不相等的项不动；其余项照常还原；返回 `restore_blocked` 和不相等的键名列表；access-state.json 保留未还原的项，供「重新检查」再试。
- 卸载用的还原是"先检查后动手"：任何一项不相等就什么都不改，直接返回 `restore_blocked`。
- 原配置文件不存在、还原后内容为空 → 删除文件。
- 写入：先在同目录写临时文件，解析校验通过后再替换原文件；任何一步失败，原文件保持不变。
- 写入或还原前后都要能被 TOML 正常解析，否则返回 `config_invalid`，不写。

## 涉及文件
- core/internal/codexconfig/locate.go（新建）
- core/internal/codexconfig/edit.go（新建）
- core/internal/codexconfig/state.go（新建）
- core/internal/codexconfig/apply.go（新建）
- core/internal/codexconfig/*_test.go（新建，与实现文件一一对应）

## 成品定义

`access-state.json`（数据目录）：
```json
{
  "entries": {
    "openai_base_url":   { "had_original": true,  "original": "https://proxy.example.org/v1", "written": "http://127.0.0.1:47311/v1", "taken_over": true },
    "model_catalog_json":{ "had_original": false, "original": null, "written": "/Users/x/Library/Application Support/ModelBridge/model-bridge-catalog.json", "taken_over": false },
    "model":             { "had_original": true,  "original": "gpt-6.1-sol", "written": "auto", "taken_over": false },
    "model_provider":    { "had_original": false, "original": null, "written": null, "taken_over": false },
    "desktop.enabled-reasoning-efforts": { "had_original": true, "original": ["low","medium","high"], "written": ["low","medium","high","none"], "taken_over": false }
  }
}
```
`written` 为 null 表示本工具让这一项"不存在"；检查手改时，"应不存在却存在"也算不相等。

## 函数清单
### core/internal/codexconfig/locate.go
| 函数名 | 职责 |
|---|---|
| `ConfigPath` | 按平台和 CODEX_HOME 得出 config.toml 路径 |
| `EnsureInitialBackup` | 首次开启前整份备份到 config.toml.before-model-bridge，已存在则跳过 |
### core/internal/codexconfig/edit.go
| 函数名 | 职责 |
|---|---|
| `ReadRootValue` | 用 go-toml/v2 的 unstable.Parser 读根上某个键的值和字节范围 |
| `SetRootString` | 改写或新增根上的字符串键；新增时插在第一个表头之前 |
| `RemoveRootKey` | 删除根上的某个键所在行 |
| `ReadDesktopEfforts` | 读 `[desktop]` 表（或根上 `desktop.` 点号写法）的 enabled-reasoning-efforts |
| `SetDesktopEfforts` | 写入该列表；没有 `[desktop]` 表时用根上点号写法 |
| `RemoveDesktopEfforts` | 删除该项 |
| `Validate` | 整份文本能否被 TOML 正常解析 |
### core/internal/codexconfig/state.go
| 函数名 | 职责 |
|---|---|
| `LoadState` | 读 access-state.json |
| `SaveState` | 原子写入 access-state.json |
### core/internal/codexconfig/apply.go
| 函数名 | 职责 |
|---|---|
| `Enable` | 记录原值、写入全部接入项、原子替换；返回被接管的键名 |
| `Restore` | 逐项检查并还原；有手改项时返回 restore_blocked 和键名，其余照常还原 |
| `RestoreAllOrNothing` | 卸载用：先全部检查，有任何手改项则不改任何东西 |
| `CheckTampered` | 只检查不改，返回手改项键名 |
| `atomicWrite` | 同目录临时文件 → 校验 → 替换 |

## 协作关系
控制层（06）在打开转发时调用 EnsureInitialBackup → Enable（需要合并目录路径和中台模型需要的等级，来自 02 的 RequiredReasoningLevels）；关闭转发、退出时调用 Restore；卸载时调用 RestoreAllOrNothing。依赖 go-toml/v2。

## 验证方式
- 测试入口：`cd core && go test ./internal/codexconfig/...`
- 测试输入：临时目录里的各种 config.toml 样本（空文件、不存在、带注释、已有 openai_base_url、已有 model_provider、有 [desktop] 表、只有 desktop.x 点号写法、有其他表）
- 预期结果：
- [ ] Enable 后与原文 diff，只有接入项所在行变化；注释、空行、其他表逐字节不变
- [ ] Enable → Restore 后文件与原文逐字节一致；中途追加一行无关配置，Restore 后这一行仍在
- [ ] 已有 openai_base_url → Enable 返回接管键名，state 里记下原值，Restore 后恢复原值
- [ ] 手改 openai_base_url 后 Restore → 该项不变，其他项已还原，返回 restore_blocked 和 ["openai_base_url"]；改回写入值再 Restore → 全部还原
- [ ] RestoreAllOrNothing 遇到手改项 → 文件逐字节不变
- [ ] enabled-reasoning-efforts 原有 ["low","high"]、需要 ["none","high"] → 写成 ["low","high","none"]
- [ ] 写入中途模拟替换失败 → 原文件不变；生成的文本不能解析时返回 config_invalid 且不写
- [ ] 原来没有 config.toml → Enable 创建，Restore 后文件被删除；before-model-bridge 备份存在且为空
