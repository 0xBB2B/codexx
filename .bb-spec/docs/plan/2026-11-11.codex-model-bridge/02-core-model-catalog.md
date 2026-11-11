---
name: 02-core-model-catalog
description: 内置参数表、获取官方清单和中台列表、生成合并模型目录、记住上次成功的中台列表
---
# 核心：模型目录

## 目标
核心程序能生成 Codex 可读的合并模型目录：官方模型在前，中台模型在后；中台模型只取"中台列表 ∩ 参数表"，参数来自参数表。

## 业务规则（来源：PRD 02-model-routing）
- 官方清单来自当前 ChatGPT 自带的 codex 程序：执行 `<codex> debug models --bundled`，CODEX_HOME 指向一个新建的空临时目录，工作目录也设为它，并从环境变量里去掉 OPENAI_API_KEY、CODEX_API_KEY、CODEX_HOME 原值；30 秒超时。绝不读取或复制 `auth.json`。
- codex 程序位置：macOS 依次找 `/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex`、`~/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex`；Windows 由外壳在 init 时传入 ChatGPT 安装目录，在其下找 `resources\codex-cli\bin\codex.exe`、`resources\codex.exe`。找不到 → 错误码 `chatgpt_not_found`；执行失败或输出不是含非空 `models` 数组的 JSON → `chatgpt_unsupported`。
- 中台列表：`GET <Base URL>/models`，请求头 `Authorization: Bearer <中台 Key>`，15 秒超时；读取 `data[].id`。
- 中台模型 = 中台列表 ∩ 参数表（按 id 完全相等匹配）；参数表外的不显示。
- 合并目录：官方条目原样在前（顺序不变）；中台条目在后，priority 从"官方最大 priority + 1"开始递增。
- 中台条目：复制官方清单第一条的全部字段作为底板（保证字段集合跟得上当前 Codex 版本），再按下面「成品定义」覆盖字段。
- 中台列表获取失败时沿用上次成功的列表（含获取时间）；从没成功过 → 目录里只放官方模型。
- 失败的状态码映射为错误码：401→`relay_401`、403→`relay_403`、404→`relay_404`、429→`relay_429`、5xx→`relay_5xx`、超时或网络错误→`relay_unreachable`。不保存、不记录中台返回的正文。
- 界面、日志里不出现中台模型名，只报数量。

## 涉及文件
- core/internal/catalog/params.json（新建）
- core/internal/catalog/params.go（新建）
- core/internal/catalog/official.go（新建）
- core/internal/catalog/relay.go（新建）
- core/internal/catalog/merge.go（新建）
- core/internal/catalog/store.go（新建）
- core/internal/catalog/*_test.go（新建，与实现文件一一对应）

## 成品定义

`core/internal/catalog/params.json` 的格式和第一条（auto）。其余主流公开模型由实现时逐个补充：每条必须带 `source`，填模型提供方官方文档里写明上下文窗口和输入类型的页面地址；找不到官方出处的模型不收录。
```json
{
  "models": [
    {
      "id": "auto",
      "context_window": 1000000,
      "input_modalities": ["text", "image"],
      "reasoning_levels": [],
      "default_reasoning_level": null,
      "source": "用户确认（2026-11-11）：中台 auto 模型，1M 上下文，支持文字和图片，思考等级由中台自动匹配"
    }
  ]
}
```
字段说明：`reasoning_levels` 取值只能是 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`；为空表示 Codex 里不显示思考等级选项。

中台条目在底板上覆盖的字段（其余字段保留底板值）：
```json
{
  "slug": "<id>",
  "display_name": "<id>",
  "description": "中台模型",
  "visibility": "list",
  "supported_in_api": true,
  "priority": "<官方最大 priority + 1 + 序号>",
  "context_window": "<参数表 context_window>",
  "max_context_window": "<参数表 context_window>",
  "input_modalities": "<参数表 input_modalities>",
  "supported_reasoning_levels": "<参数表 reasoning_levels 逐个转成 {\"effort\": 等级, \"description\": 等级}>",
  "default_reasoning_level": "<参数表 default_reasoning_level>",
  "supports_reasoning_effort_updates": "<reasoning_levels 非空时 true，否则 false>",
  "supports_search_tool": false,
  "web_search_tool_type": null,
  "experimental_supported_tools": [],
  "service_tiers": [],
  "additional_speed_tiers": [],
  "upgrade": null,
  "availability_nux": null,
  "model_messages": null
}
```

数据目录里的中台列表记录 `relay-models.json`：
```json
{ "fetched_at": "2026-11-10T09:40:00Z", "ids": ["auto", "deepseek-v3.2"] }
```

## 函数清单
### core/internal/catalog/params.go
| 函数名 | 职责 |
|---|---|
| `LoadParams` | 读取嵌入的 params.json，校验每条都有 source、等级取值合法 |
| `Lookup` | 按模型 id 取参数，没有时报告不存在 |
### core/internal/catalog/official.go
| 函数名 | 职责 |
|---|---|
| `FindCodexExecutable` | 按平台规则找到 ChatGPT 自带的 codex 程序 |
| `FetchOfficial` | 在空临时目录、干净环境下执行 `debug models --bundled`，返回官方条目原始 JSON 列表 |
### core/internal/catalog/relay.go
| 函数名 | 职责 |
|---|---|
| `FetchRelayIDs` | 请求中台 /models，返回模型 id 列表或错误码 |
| `ErrorCodeForStatus` | 把 HTTP 状态或网络错误转成 relay_* 错误码 |
### core/internal/catalog/merge.go
| 函数名 | 职责 |
|---|---|
| `SelectRelayModels` | 中台 id 与参数表取交集，保持中台返回的顺序 |
| `BuildMerged` | 生成合并目录 JSON：官方在前，中台条目按底板加覆盖字段生成 |
| `RequiredReasoningLevels` | 汇总本次中台模型用到的全部思考等级，供配置写入使用 |
### core/internal/catalog/store.go
| 函数名 | 职责 |
|---|---|
| `LoadLastRelay` | 读上次成功的中台列表记录 |
| `SaveLastRelay` | 原子写入本次成功的中台列表记录 |
| `WriteMerged` | 原子写入合并目录文件到数据目录 `model-bridge-catalog.json` |

## 协作关系
控制层（06）在启动、保存设置、手动重试时调用：FindCodexExecutable → FetchOfficial → FetchRelayIDs（失败则 LoadLastRelay）→ SelectRelayModels → BuildMerged → WriteMerged。转发层（03）用 SelectRelayModels 的结果判断哪些模型发往中台。外部依赖：ChatGPT 自带的 codex 程序、中台 /models 接口。

## 验证方式
- 测试入口：`cd core && go test ./internal/catalog/...`
- 测试输入：假 codex 脚本（打印固定 JSON、记录收到的环境变量和 CODEX_HOME）；httptest 假中台（返回指定 id 列表或状态码）；临时数据目录
- 预期结果：
- [ ] 假 codex 收到的 CODEX_HOME 是空目录，环境变量里没有 OPENAI_API_KEY、CODEX_API_KEY；整个过程没有打开 `auth.json`（在临时 Codex 目录放一个不可读的 auth.json，流程照常成功）
- [ ] 中台返回 auto、某参数表内模型、unknown-model-x → 合并目录只多出前两个，排在全部官方条目之后，priority 连续递增
- [ ] 中台条目的 context_window、input_modalities、supported_reasoning_levels 与参数表一致；supports_search_tool 为 false；底板里其他字段保留
- [ ] 中台返回 401/403/404/429/500/超时 → 分别得到规定错误码；返回正文里放特殊字符串，错误值里找不到它
- [ ] 中台失败且有上次记录 → 用上次 ids；没有记录 → 合并目录只有官方条目
- [ ] codex 不存在 → `chatgpt_not_found`；输出非法 JSON → `chatgpt_unsupported`
- [ ] params.json 每条都有 source；出现非法等级时 LoadParams 报错
