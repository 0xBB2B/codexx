---
name: 05-access-mode-service
description: 后台服务按登录检测结果决定接入方式，提供检测与切换接口，状态里带接入方式
---
# 后台服务的接入方式

## 目标
控制服务在启动、打开转发、重新检测时检测登录状态，按结果选择接入方式写入 Codex 配置、生成目录、设置本机转发，并把接入方式通过状态告诉桌面端。

## 业务规则（来源：PRD 08-access-mode C1~C9）
- 检测时机：转发开着时本工具启动（`init`）；每次打开转发（`set_forwarding` 开）；桌面端请求检测（`detect_access`）。转发关着时启动不检测。
- 检测结果 → 目标方式：已登录 →「官方 + 中台」(`both`)；没登录 →「只用中台」(`relay`)；检测失败 → 上次检测成功时的方式，从没成功过 → `relay`。每次检测成功都记住它对应的方式（重启后仍记得）。
- `init` 时转发开着：先按**上次实际写入的方式**恢复转发（没有记录时按 `both`，因为旧版本只有这种方式），再检测；方式不同时**不自动切换**，由桌面端决定是直接切换还是先弹窗确认（ChatGPT 是否在运行只有桌面端知道）。
- 打开转发：本次检测得到的目标方式直接写入。
- 切换方式（`apply_access`）：转发开着且目标方式和当前写入方式不同 → 直接按目标方式调用 `codexconfig.Enable`（它发现状态里记的是另一种方式时，会先在内存里还原再按新方式写入，一次落盘；被用户手改过的项保留用户的值，新方式也要写这一项时记为接管）。结果等同于"关闭转发后按新方式打开"。方式相同或转发关着 → 什么都不改。
- 「只用中台」：Codex 配置写模型服务（plan 01 的 `RelayOnly`），目录只放中台模型（plan 03 的 `RelayOnly`），本机转发拒绝非中台请求（plan 04 的 `RelayOnly`）。「官方 + 中台」保持现有行为。
- 关闭转发、退出、卸载：照常还原，不检测。
- 重新获取中台模型、保存连接（会重写目录和配置）时，按当前写入的方式重写，不改变方式。
- 检测命令的输出不进入任何日志、状态或错误信息。

## 涉及文件
- 修改 `core/internal/control/settings.go`、`core/internal/control/settings_test.go`
- 修改 `core/internal/control/service.go`、`core/internal/control/service_test.go`
- 修改 `core/internal/control/protocol.go`、`core/internal/control/protocol_test.go`

## 成品定义：控制协议新增内容
状态对象新增字段 `access`（转发关着且从没检测过时三个值都是 null）：

```json
"access": {
  "mode":   "both" | "relay" | null,   // Codex 配置当前按哪种方式写入；转发关着为 null
  "target": "both" | "relay" | null,   // 最近一次检测得出的目标方式；还没检测过为 null
  "detect": "logged_in" | "not_logged_in" | "failed" | null  // 最近一次检测结果
}
```

新增两个方法，参数都为空，返回值都是完整状态对象（和 `status` 相同）：

| 方法 | 作用 |
|---|---|
| `detect_access` | 运行一次登录检测，更新 `target`、`detect`，不改 Codex 配置 |
| `apply_access` | 转发开着且 `target` 不为 null 且和 `mode` 不同时，按 `target` 切换；否则不改动 |

设置文件 `settings.json` 新增两个字段（缺失时按 null 处理）：

```json
{"forwarding": true, "access_mode": "both", "last_detected_mode": "relay"}
```
`access_mode` 是当前写入的方式（关闭转发时清空），`last_detected_mode` 是最近一次检测成功对应的方式。

## 函数清单
### core/internal/control/settings.go
| 函数名 | 职责 |
|---|---|
| `Settings`（结构体） | 新增 `AccessMode`、`LastDetectedMode` 两个字段 |

### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `Options`（结构体） | 新增 `DetectLogin` 可替换检测函数（默认调用 `codexauth.Detect`），便于测试 |
| `AccessStatus`（结构体） | 状态里的 `access` 对象 |
| `Service.Init` | 转发开着时按记录的方式恢复，再检测 |
| `Service.SetForwarding` | 打开时检测并按目标方式写入；关闭时清空当前方式 |
| `Service.DetectAccess` | 检测并更新目标方式，不改配置 |
| `Service.ApplyAccess` | 按目标方式切换（还原后重新写入），失败时保持原方式 |
| `Service.detect` | 调用检测函数、按规则得出目标方式、记住检测成功的方式并保存设置 |
| `Service.rebuild` / `Service.enable` / `Service.pushRelay` | 按当前方式传 `RelayOnly` 给目录、配置、本机转发 |
| `Service.statusLocked` | 填充 `access` |

### core/internal/control/protocol.go
| 函数名 | 职责 |
|---|---|
| `dispatch` | 新增 `detect_access`、`apply_access` 两个分支 |

## 协作关系
- `detect` → `locateCodex` 拿到 codex 路径 → `Options.DetectLogin`（默认 `codexauth.Detect`）。找不到 codex 视为检测失败。
- `ApplyAccess` → `rebuild`（`catalog.Build` 带 `RelayOnly`）→ `enable`（`codexconfig.Enable` 带 `RelayOnly`）→ `pushRelay`（`proxy.RelayConfig.RelayOnly`）→ `SaveSettings`。
- 依赖 plan 01~04 新增的字段和函数。

## 验证方式
- 测试入口：`control.NewService` 的公开方法和 `control.Serve` 的行协议；沿用现有假 codex、假中台、临时 Codex 配置；用 `Options.DetectLogin` 注入检测结果。
- 预期结果与验证项：
- [ ] 检测返回没登录时打开转发：config 里 `model_provider = "modelbridge"`、无 `openai_base_url`；目录只有 `relay-` 条目；向本机转发发官方模型请求得到 400 `official_unavailable`；状态 `access` 为 `{mode: relay, target: relay, detect: not_logged_in}`。
- [ ] 检测返回已登录时打开转发：配置、目录、分流和现有行为一致；`access.mode == "both"`。
- [ ] 检测失败且设置里 `last_detected_mode` 为 `both` → 目标 `both`；没有记录 → 目标 `relay`；检测成功后设置文件里记住对应方式。
- [ ] 转发开着、记录方式为 `both`，`init` 时检测为没登录：配置仍是 `both` 的写法，状态 `{mode: both, target: relay}`；随后 `apply_access` → 配置变为 `relay` 写法，和"关闭转发后按 relay 打开"的结果逐字节一致，设置里 `access_mode` 变为 `relay`。
- [ ] `apply_access` 在方式相同、或转发关着时：配置文件不变。
- [ ] 「官方 + 中台」开着时手改 `openai_base_url`，再 `apply_access` 切到 `relay` → 成功，`openai_base_url` 保持手改值，其余为 `relay` 写法；状态 `taken_over` 照常反映接管项。
- [ ] `detect_access` 只更新 `target` / `detect`，配置文件不变。
- [ ] 转发关着时 `init`：检测函数没有被调用。
- [ ] 关闭转发：配置和第一次打开前逐字节一致，`access.mode` 为 null。
- [ ] 「只用中台」下保存新连接或重新获取模型：配置仍是 `relay` 写法，目录仍只有中台条目。
- [ ] 行协议：`{"id":1,"method":"detect_access"}`、`{"id":2,"method":"apply_access"}` 都返回带 `access` 字段的状态；现有 control 测试全部通过。
