---
name: 01-core-relay-only
description: 核心保存「只用中转」开关、按开关和检测结果定接入方式，新增 set_relay_only 方法和 access 两个字段
---
# 核心：「只用中转」开关

## 目标
核心能保存开关状态，开关开着时接入方式固定为「只用中转」，并通过协议把开关状态和"上次检测是否已登录"告诉两端界面。

## 业务规则（来源：PRD 01-relay-only-switch）
- 接入方式：开关开着 → `relay`，不管检测结果；开关关着 → 和现在一样按检测结果（已登录 `both`，没登录 `relay`，检测失败沿用上次检测成功时的方式，从没成功过 `relay`）。开关开着时检测结果变化不会让 `access.target` 和 `access.mode` 不同，所以界面不会自动切换、不弹"已切换到…"。
- 开关保存在本工具数据目录的 settings.json 里：本工具重启、关掉再打开转发都保留，只有调 `set_relay_only` 才改。第一次使用（没有这个字段）是关着。
- `last_detected_mode` 仍只记登录检测的结果（已登录 `both`、没登录 `relay`），不受开关影响；界面靠它判断开关能不能点：上次检测成功且已登录 → 能点，否则不能点（包括从没检测成功过）。
- 转发开着时调 `set_relay_only` 切换接入方式：Codex 配置和"关闭转发、按新方式重新打开"得到的内容逐字节一致；之后关闭转发，配置和第一次打开转发前逐字节一致（沿用现有 activate / Restore，不另写配置逻辑）。
- 写 Codex 配置失败：开关、接入方式、settings.json 和 Codex 配置都保持调用前的样子，返回和 `apply_access` 相同的错误。
- 转发关着时调 `set_relay_only`：只保存开关，不改 Codex 配置。

## 涉及文件
- 修改 core/internal/control/settings.go
- 修改 core/internal/control/service.go
- 修改 core/internal/control/protocol.go
- 修改 core/PROTOCOL.md
- 修改 core/internal/control/service_test.go、core/internal/control/protocol_test.go（测试）

## 成品定义

settings.json（本工具数据目录，`Settings` 结构新增一个字段，JSON 名 `relay_only`，布尔，缺省为 false）：
```json
{"forwarding": true, "access_mode": "relay", "last_detected_mode": "both", "next_relay_id": 2, "relay_only": true}
```

core/PROTOCOL.md「方法」表在 `apply_access` 行后新增一行：
```markdown
| set_relay_only | {"enabled": 布尔} | Status（保存「只用中转」开关；转发开着时按新的目标方式切换接入方式，写 Codex 配置失败时开关和接入方式保持原样并返回错误） |
```

core/PROTOCOL.md「Status」示例里的 `access` 改为：
```json
  "access": {"mode": "both", "target": "relay", "detect": "not_logged_in", "relay_only": false, "logged_in": false}
```

core/PROTOCOL.md 说明 `access` 的那一段替换为：
```markdown
`access` 是接入方式：`mode` 为 Codex 配置当前按哪种方式写入（`both` 官方 + 中转，`relay` 只用中转；转发关着为 null），`target` 为按「只用中转」开关和最近一次检测得出的目标方式（开关开着时总是 `relay`；没检测过为 null），`detect` 为最近一次检测结果（`logged_in`、`not_logged_in`、`failed`，没检测过为 null），`relay_only` 为「只用中转」开关是否开着，`logged_in` 为最近一次检测成功时是否已登录（从没检测成功过为 false）。
```

## 函数清单
### core/internal/control/settings.go
| 函数名 | 职责 |
|---|---|
| `Settings`（结构） | 新增 `RelayOnly` 字段，JSON 名 `relay_only` |

### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `AccessStatus`（结构） | 新增 `RelayOnly`（`relay_only`）和 `LoggedIn`（`logged_in`）两个布尔字段 |
| `detect` | 照旧按登录结果算出方式并写 `LastDetectedMode`；之后开关开着时把 `accessTarget` 定为 `relay` |
| `SetRelayOnly`（新增） | 加锁；保存开关；转发开着且目标和当前方式不同时 activate；失败回滚开关、目标，重新 activate 原方式，返回错误；成功 saveSettings |
| `statusLocked` | 填 `access.relay_only`、`access.logged_in`（`LastDetectedMode == both`） |

### core/internal/control/protocol.go
| 函数名 | 职责 |
|---|---|
| 分发 switch | 新增 `set_relay_only`，解码 `{"enabled": 布尔}` 后调 `SetRelayOnly` |

## 协作关系
`set_relay_only` → `Service.SetRelayOnly` → 复用 `activate`（写 Codex 配置、更新目录和本机转发服务）、`saveSettings`；`Init` 读 settings 时带出开关，`redetect`/`detect` 使用它。

## 验证方式
- 测试入口：`control.Service` 公开方法（`Init`、`SetForwarding`、`DetectAccess`、`ApplyAccess`、`SetRelayOnly`、`Status`）和协议 `Serve` 的 `set_relay_only` 方法；沿用 `newEnv`、`withDetect`、`withSettings`、`requireAccess`、`settingsJSON`、`isRelayConfig`、`isBothConfig`。
- 测试输入：假登录检测结果（已登录 / 没登录 / 失败）、预写的 settings.json、转发开或关。
- 预期结果：
- [ ] 已登录、转发开、`SetRelayOnly(true)` → `access` 为 mode=relay、target=relay、relay_only=true、logged_in=true；Codex 配置是只用中转方式；settings.json 有 `"relay_only":true`。
- [ ] 接着 `SetRelayOnly(false)` → mode=both；配置是官方 + 中转方式，且和"关转发再按官方 + 中转打开"的字节一致；再关转发 → 配置和第一次打开前逐字节一致。
- [ ] 开关开着：检测改成没登录再改回已登录，每次 `DetectAccess` 后 target 都是 relay、与 mode 相同；`last_detected_mode` 分别是 relay、both；logged_in 分别是 false、true。
- [ ] settings.json 预写 `relay_only:true`、转发开 → 新建 Service 后 `Init`：已登录时 mode=relay；关转发再开仍是 relay。
- [ ] 没有 `relay_only` 字段的旧 settings.json → relay_only=false；从没检测成功过且检测失败 → logged_in=false。
- [ ] 转发关着调 `SetRelayOnly(true)` → Codex 配置文件没有变化，settings.json 保存了开关。
- [ ] 让 Codex 配置无法写入（配置文件不合法）后调 `SetRelayOnly(true)` → 返回错误；mode、relay_only 和调用前相同；settings.json、Codex 配置文件和调用前逐字节一致。
- [ ] 协议：`set_relay_only` 带 `{"enabled":true}` 返回 Status，其中 `access.relay_only` 为 true；参数不是布尔时返回参数错误。
