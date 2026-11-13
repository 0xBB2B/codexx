---
name: 06-macos-access-mode-ui
description: macOS 主窗口显示接入方式和「重新检测」，切换前确认重启，首次设置完成页按方式显示文字
---
# macOS 界面：接入方式

## 目标
macOS 端显示当前接入方式、支持重新检测，检测结果变化时按 ChatGPT 是否在运行直接切换或弹窗确认，各处文字按接入方式显示。

## 业务规则（来源：PRD 08-access-mode C8、C10~C14，界面以已确认原型第 5 版为准）
- 后台服务状态里有 `access: {mode, target, detect}`（`mode`、`target` 取 `both` / `relay`，`detect` 取 `logged_in` / `not_logged_in` / `failed`）；方法 `detect_access`（只检测）、`apply_access`（按目标切换）都返回完整状态。
- 主窗口「转发」卡片：
  - 开关说明：开着"开启时，本助手按下方的接入方式修改 Codex 配置。"；关着"已关闭：Codex 配置已还原为开启前的样子。"；没找到 ChatGPT 时保持"找到 ChatGPT 后才能打开。"
  - 转发开着时在开关下面加一行「接入方式」：粗体方式名「官方 + 中台」或「只用中台」，后接灰字"· 已检测到 ChatGPT 登录" / "· 没有检测到 ChatGPT 登录"（`detect == failed` 时为"· 沿用上次的方式"），右侧「重新检测」次要按钮；下面一行说明：
    - both："模型列表里官方模型在上，中台模型在下；选官方模型时仍然访问官方服务。"
    - relay："不需要登录 ChatGPT。模型列表里只有中台模型，默认选中「中台 · auto」。登录 ChatGPT 后点「重新检测」，就能同时用官方模型。"
    - `detect == failed` 时说明上方多一行黄色警告"没能检测 ChatGPT 登录状态，沿用上次的方式。"
  - 转发关着时没有这一行。
- 点「重新检测」：按钮变成转圈加"正在检测…"并禁用 → 调 `detect_access` → 按下面的「对齐规则」处理；目标和当前相同且检测成功时，主窗口顶部出现信息提示"接入方式没有变化"，正文：relay 为"仍然没有检测到 ChatGPT 登录，继续只用中台模型。"，both 为"仍然检测到 ChatGPT 登录，继续同时使用官方模型和中台模型。"，带「知道了」。
- 对齐规则（启动后第一次收到转发开着的状态、以及每次重新检测后都执行）：`target` 不为 null 且不等于 `mode` 时：
  - ChatGPT 没在运行 → 直接调 `apply_access`，不弹窗；
  - ChatGPT 在运行 → 弹确认框。切到 both：标题"已检测到 ChatGPT 登录，切换到「官方 + 中台」并重启 ChatGPT？"，正文"切换后，Codex 里会同时显示官方模型和中台模型。"；切到 relay：标题"没有检测到 ChatGPT 登录，切换到「只用中台」并重启 ChatGPT？"，正文"切换后，Codex 不需要登录，模型列表里只有中台模型。"。两种正文后都接"\n\nChatGPT 正在运行，需要重启才能生效。重启会中断 Codex 里正在进行的任务。"，按钮「取消」「切换并重启」。确认 → `apply_access` 成功后重启 ChatGPT；取消 → 什么都不调。
  - `apply_access` 失败 → 和打开/关闭转发失败一样显示错误条（含 `restore_blocked` 的处理）。
- 开关转发的确认框（ChatGPT 在运行时）：
  - 关闭且当前 `mode == relay`：正文"关闭后会把 Codex 配置还原成开启前的样子。没有登录 ChatGPT 时，关闭后 Codex 里的模型都无法使用。"+ 重启说明；`mode == both` 保持现有正文。
  - 打开：先调 `detect_access`，`target == relay` 时正文"没有检测到 ChatGPT 登录，打开后 Codex 不需要登录，模型列表里只有中台模型。"+ 重启说明；否则保持现有正文。
- 首次设置完成页（转发已开启时）正文按状态里的 `access.mode`：
  - relay："已获取 N 个中台模型。没有检测到 ChatGPT 登录，接入方式为「只用中台」：Codex 不需要登录，模型列表里只有中台模型。重启 ChatGPT 后就能使用。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
  - both："已获取 N 个中台模型。已检测到 ChatGPT 登录，接入方式为「官方 + 中台」。重启 ChatGPT 后，就能在 Codex 的模型列表里选用它们。助手会在你登录电脑时自动启动，在菜单栏里常驻。"
- 样式只用 DESIGN.md 已有的颜色、字号、按钮、提示条和弹窗，不新增样式。菜单栏菜单不变。Windows 端本次不改（2026-11-12 用户决定先做 macOS）。

## 涉及文件
- 修改 `macos/Sources/ModelBridge/System/CoreProtocol.swift`
- 修改 `macos/Sources/ModelBridge/UI/AppModel.swift`
- 修改 `macos/Sources/ModelBridge/UI/MainWindow.swift`
- 修改 `macos/Sources/ModelBridge/UI/SetupWindow.swift`
- 修改 `macos/Tests/ModelBridgeTests/AppModelTests.swift`、`macos/Tests/ModelBridgeTests/CoreProtocolTests.swift`

## 函数清单
### macos/Sources/ModelBridge/System/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `CoreStatus.Access`（结构体）与 `CoreStatus.access` | 解码 `access` 对象 |

### macos/Sources/ModelBridge/UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `Confirmation.Kind.switchAccess` | 切换接入方式的确认 |
| `isDetectingAccess`（发布属性） | 重新检测进行中 |
| `accessModeName` / `accessReasonText` / `accessHelpText` / `accessDetectFailed` | 主窗口「接入方式」一行的文字 |
| `forwardingHelpText` | 开关说明 |
| `setupSucceededText` | 完成页正文 |
| `recheckAccess` | 重新检测并对齐，必要时给出"没有变化"提示 |
| `reconcileAccess` | 对齐规则：直接切换或弹确认 |
| `toggleForwarding` | 确认框正文按接入方式；打开前先检测 |
| `confirmPending` | 处理 `switchAccess`：切换后重启 ChatGPT |
| `apply` | 首次收到转发开着且带目标方式的状态时执行一次对齐 |

### macos/Sources/ModelBridge/UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| `forwardingCard` | 开关说明改用 `forwardingHelpText`，转发开着时加「接入方式」一行 |
| `accessRow` | 「接入方式」一行：名称、原因、「重新检测」按钮（检测中转圈禁用）、失败警告、说明 |

### macos/Sources/ModelBridge/UI/SetupWindow.swift
| 函数名 | 职责 |
|---|---|
| `succeeded` | 正文改用 `setupSucceededText` |

## 协作关系
- `AppModel` 通过现有 `core.call` 调 `detect_access` / `apply_access`；重启用现有 `restartChatGPT`；ChatGPT 是否运行用现有 `chatGPT.isRunning()`。
- 依赖 plan 05 的协议字段和方法。

## 验证方式
- 测试入口：`AppModel` 公开方法和属性（`swift test`，沿用 `AppModelTests` 里的假 core、假 ChatGPT）；`CoreStatus` 解码。
- 预期结果与验证项：
- [ ] 解码含 `"access":{"mode":"relay","target":"both","detect":"logged_in"}` 的状态；缺 `access` 时为 nil。
- [ ] 状态 relay/not_logged_in：名称"只用中台"，原因"· 没有检测到 ChatGPT 登录"，说明为规定的 relay 文字；detect failed：原因"· 沿用上次的方式"，`accessDetectFailed == true`。
- [ ] 开关说明在开、关、没找到 ChatGPT 三种状态下为规定文字。
- [ ] `recheckAccess`：调用 `detect_access` 期间 `isDetectingAccess == true`；返回 target==mode 且检测成功 → notice 标题"接入方式没有变化"、正文按方式；返回 target!=mode 且 ChatGPT 没运行 → 接着调用 `apply_access`，没有确认框；ChatGPT 运行 → 出现规定标题、正文、按钮的确认框，`confirmPending` 后调用 `apply_access` 再重启 ChatGPT，`cancelPending` 后没有调用 `apply_access`。
- [ ] 启动后第一次收到 `{forwarding: true, access: {mode: both, target: relay}}`：ChatGPT 没运行时自动调用一次 `apply_access`；后续同样的状态事件不再重复调用。
- [ ] ChatGPT 运行时关闭转发且 mode==relay：确认框正文为规定的 relay 文字；打开转发：先调用 `detect_access`，target==relay 时正文为规定文字。
- [ ] 首次设置成功后状态 mode==relay / both：`setupSucceededText` 为规定的两种文字（N 取保存结果里的模型数）。
- [ ] `apply_access` 返回 `restore_blocked`：出现现有的"没能自动还原 Codex 配置"错误条。
- [ ] 现有 macOS 测试全部通过。
