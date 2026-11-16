---
name: 03-core-control-relays
description: 核心程序的控制协议和服务改成多个中转配置：init 带配置列表，新增保存、删除配置，状态按配置给出
---
# 控制协议与服务支持多个中转配置

## 目标
外壳通过控制协议传入多个中转配置，保存（新增或修改）、删除单个配置，按配置刷新；状态里按配置给出模型数和获取情况，以及默认模型所在配置名。

## 业务规则（来源：PRD 03-multi-relay-config、02-main-window）
- 配置由编号、名字、Base URL、API Key 组成，最少 1 个、最多 10 个，按列表顺序生效。核心程序只在内存里持有 Key，不写文件、不写日志。
- 名字校验（保存时做）：去掉首尾空格后 1~16 个字（按字符数，不按字节）；不能含「·」；和其他配置的名字不区分大小写不能相同。Base URL 必须 https；Key 不能为空。
- 保存（编号为空 = 新增）：先校验，再用填的 Base URL 和 Key 获取模型列表；成功才生效并返回新编号，失败返回错误、什么都不变。满 10 个时新增返回错误。
- 新编号：从核心设置里记录的"下一个编号"开始（至少比现有编号都大），用过的编号不再分配，保证删除的配置的内部名不会被新配置占用。
- 删除：只剩 1 个时拒绝；删除后重新生成目录、更新转发对照表。
- 刷新：可以刷新全部或只刷新一个配置。
- 「只用中转」等写入 Codex 配置的默认模型：由目录给出的默认模型内部名决定（第一个有 auto 的配置的 auto），没有时不写 `model`。原来只看"中台列表里有没有 auto"的逻辑改为用这个内部名。
- 外壳在调用前先按同样规则在本地校验，以便即时显示含名字、字数的错误文字；核心的校验是最后一道关。`name_duplicate` 错误的 `keys` 里放已有的那个名字。
- 状态里的 `default_relay` 是默认模型所在配置的名字（没有时为 null），外壳用它显示「默认选中「<名字> · auto」」。
- 错误码和界面说明里的「中台」换成「中转」；core 里其余源码、注释、测试中的「中台」也一并改成「中转」（`codexconfig/state.go`、`normalize/relay.go`、`catalog/relay.go`、`catalog/params.json` 等，plan 01、02 已覆盖的文件除外）。

## 涉及文件
- 修改 `core/PROTOCOL.md`（按下方成品）
- 修改 `core/internal/control/service.go`、`protocol.go`、`settings.go`
- 修改 `core/internal/codexconfig/apply.go`（`HasAutoModel` 改为默认模型内部名）
- 修改 core 里其余含「中台」的源码和测试（只换称呼）
- 修改对应测试：`service_test.go`、`protocol_test.go`、`settings_test.go`、`codexconfig/apply_test.go`

## 成品定义
`core/PROTOCOL.md` 的「方法」表、「Status」示例与说明、「错误码」表改成下面的内容（其余段落不变）：

```markdown
## 方法
| 方法 | params | result |
|---|---|---|
| init | {"relays": [{"id": 字符串, "name": 字符串, "base_url": 字符串, "api_key": 字符串}], "chatgpt_dir": 字符串或 null} | Status |
| status | {} | Status |
| save_relay | {"id": 字符串或 null, "name": 字符串, "base_url": 字符串, "api_key": 字符串} | {"id": 字符串, "name": 字符串, "model_count": 整数, "models_changed": 布尔}（id 为 null 表示新增；name 是去掉首尾空格后的名字） |
| delete_relay | {"id": 字符串} | Status |
| set_forwarding | {"enabled": 布尔} | Status |
| refresh_models | {"id": 字符串或 null} | Status（id 为 null 刷新全部配置） |
| detect_access | {} | Status（只检测登录状态，更新 access.target 和 access.detect，不改 Codex 配置） |
| apply_access | {} | Status（转发开着且 access.target 与 access.mode 不同时，按 target 切换接入方式；否则不改动） |
| check_update | {"manual": 布尔} | {"latest_version": 字符串或 null, "url": 字符串或 null, "is_newer": 布尔} |
| prepare_uninstall | {} | {} |
| shutdown | {"restore": 布尔} | {}（随后进程退出） |

## Status
{
  "version": "1.2.0",
  "forwarding": true,
  "proxy": {"running": true, "error": null},
  "chatgpt": {"found": true, "error": null},
  "relays": [
    {"id": "1", "name": "中转", "model_count": 6, "fetched_at": "2026-11-11T00:12:00Z", "stale": false, "error": null},
    {"id": "2", "name": "A家", "model_count": 4, "fetched_at": "2026-11-10T08:00:00Z", "stale": true, "error": "relay_unreachable"}
  ],
  "default_relay": "中转",
  "taken_over": ["openai_base_url"],
  "restore_blocked": [],
  "config_path": "/Users/x/.codex/config.toml",
  "update": {"latest_version": null, "url": null},
  "access": {"mode": "both", "target": "relay", "detect": "not_logged_in"}
}

`relays` 按配置顺序列出每个中转配置这次的模型情况：`model_count` 是这次 Codex 里可用的模型数（获取失败时为沿用的缓存数，从没成功过为 0），`fetched_at` 是最近一次成功获取的时间（从没成功过为 null），`stale` 为 true 表示这次没获取成功，`error` 是失败原因的错误码。`default_relay` 是「只用中转」方式下默认模型所在配置的名字，没有任何配置有 auto 时为 null。
`access` 是接入方式：`mode` 为 Codex 配置当前按哪种方式写入（`both` 官方 + 中转，`relay` 只用中转；转发关着为 null），`target` 为最近一次检测得出的目标方式（没检测过为 null），`detect` 为最近一次检测结果（`logged_in`、`not_logged_in`、`failed`，没检测过为 null）。

## 错误码
| 码 | 界面说明 |
|---|---|
| invalid_base_url | Base URL 要以 https:// 开头。 |
| name_empty | 请填写名字。 |
| name_too_long | 名字最多 16 个字。 |
| name_has_dot | 名字里不能有「·」。 |
| name_duplicate | 已有叫「<keys[0]>」的配置（不区分大小写），换一个名字。（keys 里是已有的那个名字） |
| relay_limit | 已有 10 个，达到上限。 |
| relay_last | 至少保留一个配置。 |
| relay_not_found | 这个配置不存在，请刷新后重试。 |
| relay_401 | 中转服务返回 401：API Key 无效或已过期 |
| relay_403 | 中转服务返回 403：API Key 没有权限 |
| relay_404 | 中转服务返回 404：Base URL 不正确 |
| relay_429 | 中转服务返回 429：请求太频繁，请稍后重试 |
| relay_5xx | 中转服务暂时不可用 |
| relay_4xx | 请求未被中转服务接受 |
| relay_unreachable | 连不上中转服务，请检查网络和 Base URL |
| chatgpt_not_found | 没有找到 ChatGPT |
| chatgpt_unsupported | 当前 ChatGPT 版本暂不支持 |
| proxy_port_in_use | 本机转发服务没能启动：端口被占用 |
| restore_blocked | 没能自动还原 Codex 配置 |
| config_invalid | Codex 配置文件格式有误，未做修改 |
| config_changed | Codex 配置文件刚被其他程序修改，请重试 |
| update_unavailable | 暂时无法检查更新，请稍后再试 |
| internal_error | 内部错误，请重试 |
| unknown_method | 未知请求 |
| bad_params | 请求参数有误 |
```

「响应」一段里关于 `keys` 的那句改成：`keys` 在 restore_blocked 和 config_invalid 时列出相关键名；在 name_duplicate 时放已有的那个配置名字。

核心设置文件 `settings.json` 新增字段 `"next_relay_id": 整数`（下一个可分配的编号，缺省按 2 处理）。

## 函数清单
### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `InitParams` / `RelayInput` | 改为配置列表 |
| `RelayEntryStatus`、`Status.Relays`、`Status.DefaultRelay` | 新的状态字段，取代 `RelayStatus` |
| `SaveRelay` | 校验、获取、生效，新增时分配编号；取代 `SaveConnection` |
| `DeleteRelay` | 删除一个配置，只剩一个时拒绝 |
| `RefreshModels` | 改为可选只刷新一个配置 |
| `validateRelayName` | 名字规则 |
| `refreshRelay` / `snapshot` / `rollback` | 改为多配置：调用 `catalog.Build`、`proxy.SetRelay`，失败整体回滚 |
| 写 Codex 配置处 | 用目录给出的默认模型内部名 |

### core/internal/control/protocol.go
| 函数名 | 职责 |
|---|---|
| 方法分发 | 新增 `save_relay`、`delete_relay`，删掉 `save_connection`，`refresh_models` 带可选 id |

### core/internal/control/settings.go
| 函数名 | 职责 |
|---|---|
| `Settings.NextRelayID` | 读写下一个编号 |

### core/internal/codexconfig/apply.go
| 函数名 | 职责 |
|---|---|
| `EnableInput.DefaultModel` | 取代 `HasAutoModel`：非空时写 `model = <内部名>` |

## 协作关系
外壳 → 控制协议 → `Service` → `catalog.Build`（plan 01）→ `proxy.Server.SetRelay`（plan 02）→ `codexconfig.Enable`。

## 验证方式
- 测试入口：`cd core && go test ./internal/control/... ./internal/codexconfig/...`；协议层用现有的按行 JSON 测试方式；假中转用 `httptest`。
- 预期结果：
  - [ ] init 两个配置 → Status.relays 两项，顺序、名字、模型数正确；default_relay 为第一个有 auto 的配置名。
  - [ ] save_relay 新增合法配置 → 返回新编号（比现有都大）、模型数；之后 status 多一项。
  - [ ] save_relay 名字为空、17 个字、含「·」、和已有「A家」比「a家」→ 分别返回 name_empty、name_too_long、name_has_dot、name_duplicate，状态不变；名字「  B家  」→ 保存为「B家」。
  - [ ] save_relay 第 11 个 → relay_limit；假中转返回 401 → relay_401 且状态不变。
  - [ ] 删除编号 2 后再新增 → 新编号不是 2；只剩 1 个时 delete_relay → relay_last。
  - [ ] refresh_models 带 id → 只请求该配置的假中转。
  - [ ] 「只用中转」打开转发：配置 1 没 auto、配置 2 有 → Codex 配置里 `model = "relay2-auto"`；都没有 → 没有 `model` 项。
  - [ ] 编辑配置 2 改名 → 它的模型内部名不变（`relay2-…`）。
  - [ ] 核心程序的数据目录里搜不到任何 Key；PROTOCOL.md 与成品一致；`grep -r 中台 core` 结果为 0。
