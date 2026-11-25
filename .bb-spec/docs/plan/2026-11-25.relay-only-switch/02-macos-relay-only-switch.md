---
name: 02-macos-relay-only-switch
description: macOS 主窗口「接入方式」一行加「只用中转」开关、三种说明文字、切换提示和失败提示
---
# macOS：「只用中转」开关

## 目标
macOS 主窗口能看到并操作「只用中转」开关，说明文字、提示和 PRD 一致。

## 业务规则（来源：PRD 01-relay-only-switch）
- 核心契约：Status 的 `access` 多两个布尔字段 `relay_only`（开关是否开着）、`logged_in`（最近一次检测成功时是否已登录，从没成功过为 false）；新方法 `set_relay_only`，参数 `{"enabled": 布尔}`，返回 Status，写配置失败时返回错误、开关和接入方式保持原样。
- 布局：转发开着时「接入方式」一行从左到右：「接入方式 <方式名称> <检测结果>」、「只用中转」开关（旁边写「只用中转 · 已开启 / 已关闭」，「已开启 / 已关闭」字重 600、前面名字字重 400，文字 13px，开关和文字间 8px）、「重新检测」，各项间 12px，只排一行；转发关着时没有这一行；卡片仍是两行，高度不变。
- 开关能点：`access.logged_in` 为 true；否则禁用（背景 disabled-bg，圆点位置照开或关，旁边照常写已开启 / 已关闭，鼠标显示禁止），保持原来的开或关。正在检测时照常能点。
- 标题下的说明（转发开着时），只占一行，放不下末尾省略号、鼠标悬停显示全文：
  - mode=both：「官方模型在上，中转模型在下；选官方模型时仍访问官方服务。」
  - mode=relay 且 relay_only 且 logged_in：「已登录 ChatGPT，手动选了只用中转。只有中转模型，默认选中「<default_relay> · auto」。」
  - mode=relay 其他情况：「没有登录 ChatGPT，只能用中转模型，默认选中「<default_relay> · auto」。登录后才能改接入方式。」
  - 后两种 `default_relay` 为 null 时，把「默认选中「<配置名> · auto」」换成「没有默认选中的模型」。
- 点开关切换成功、ChatGPT 正在运行：成功提示（4 秒自动消失）。切到只用中转：标题「已切换到「只用中转」」，正文「重启 ChatGPT 后生效。重启后请新建对话：用「官方 + 中转」聊过的旧对话接不上。」；切回：标题「已切换到「官方 + 中转」」，正文「重启 ChatGPT 后生效。重启后请新建对话：用「只用中转」聊过的旧对话接不上。」；ChatGPT 没运行不弹。
- 点开关失败：错误提示（不自动消失），标题「没能切换接入方式」，正文「没能修改 Codex 配置，接入方式保持原样。请稍后再试。」；界面保持切换前。
- 「重新检测」结果和当前方式相同、mode=relay、relay_only 且 detect=logged_in：信息提示标题「接入方式没有变化」，正文「仍然检测到 ChatGPT 登录。你手动选了只用中转，继续只用中转模型。」；其他情况沿用现有两句。
- 键盘：Tab 顺序 转发开关 →「只用中转」开关（禁用时跳过）→「重新检测」；开关读屏名「只用中转」，并关联标题下的说明文字。
- 切换中（含转发开关、接入方式切换）不并发：沿用现有 `isSwitchingForwarding` 防重入。

## 涉及文件
- 修改 macos/Sources/ModelBridge/System/CoreProtocol.swift
- 修改 macos/Sources/ModelBridge/UI/AppModel.swift
- 修改 macos/Sources/ModelBridge/UI/MainWindow.swift
- 修改 macos/Sources/ModelBridge/UI/Controls.swift
- 新建 macos/Tests/ModelBridgeTests/RelayOnlySwitchTests.swift（测试）

## 函数清单
### macos/Sources/ModelBridge/System/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `CoreStatus.Access` | 新增 `relayOnly`、`loggedIn` 可选布尔字段 |

### macos/Sources/ModelBridge/UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `relayOnlyEnabled`（新增，计算属性） | 开关是否开着，取自 `access.relayOnly` |
| `relayOnlySwitchUsable`（新增，计算属性） | 开关能不能点：转发开着且 `access.loggedIn` |
| `accessHelpText` | 按上面三种情况给说明文字 |
| `setRelayOnly`（新增） | 防重入；调 `set_relay_only`；成功应用 Status 并按方向弹成功提示（ChatGPT 运行时）；失败弹规定的错误提示 |
| `recheckAccess` | 结果不变时，手动只用中转且已登录用新的那句正文 |

### macos/Sources/ModelBridge/UI/Controls.swift
| 函数名 | 职责 |
|---|---|
| `SwitchRow` | 支持"名字写在状态前面"的写法和规定的间距、字重；禁用时背景 disabled-bg；可关联说明文字供读屏 |

### macos/Sources/ModelBridge/UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| `ForwardingOverview` | 「接入方式」一行加「只用中转」开关；标题下说明单行、尾部省略、悬停显示全文 |

## 协作关系
`ForwardingOverview` 开关 → `AppModel.setRelayOnly` → `core.call("set_relay_only")` → `apply(CoreStatus)`；提示走现有 `addNotice`；ChatGPT 是否运行用 `chatGPT.isRunning()`。

## 验证方式
- 测试入口：`AppModel` 公开属性和方法（沿用 `AppModelTests.swift` 的 `Harness`、`FakeCore` 预置 `set_relay_only` / `detect_access` 响应与错误、假 ChatGPT 运行状态）；视图用 `ViewHarness.swift`。
- 测试输入：预置的 Status JSON（access 含 mode、target、detect、relay_only、logged_in、default_relay）。
- 预期结果：
- [ ] logged_in=true → 开关能点；logged_in=false → 不能点且显示的开或关等于 relay_only；正在检测时能点。
- [ ] 三种说明文字和 default_relay 为 null 时的写法与规定一致。
- [ ] `setRelayOnly(true)` 成功且 ChatGPT 运行 → FakeCore 收到 `set_relay_only` `{"enabled":true}`，弹规定的成功提示；ChatGPT 没运行 → 不弹。关掉方向同理。
- [ ] `set_relay_only` 返回错误 → 弹「没能切换接入方式」错误提示，正文规定的那句，开关和接入方式保持原样。
- [ ] 重新检测结果不变、relay_only 且 logged_in → 弹规定的「接入方式没有变化」正文。
- [ ] 视图：转发开着时「接入方式」一行有「只用中转 · 已关闭」文字；转发关着时没有；卡片两行。
