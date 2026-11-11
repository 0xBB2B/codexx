---
name: 09-macos-ui
description: macOS 外壳界面：首次设置窗口、主窗口、菜单栏菜单和各种确认弹窗
---
# macOS 外壳：界面

## 目标
macOS 上有原生的首次设置窗口、主窗口和菜单栏菜单，各种状态和弹窗与已确认的原型一致。

## 业务规则（来源：PRD 01、02、03、04、05、06；已确认原型）
样式全部按仓库根目录 `DESIGN.md`：主色 #0f766e、窗口宽 620px、分组卡片、按钮和输入框高 44px、菜单宽 286px、菜单项最小高 44px、聚焦描边 2px。窗口用系统标题栏（交通灯）。

**首次设置窗口**（标题"设置模型接入助手"；没有保存过 Base URL 时打开应用就显示）
- 标题"连接到中台"，说明"填写中台提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中台模型。"
- 两个必填项：Base URL（占位 `https://relay.example.com/v1`，帮助"以 https:// 开头的完整地址。"）、API Key（默认遮蔽，右侧显隐按钮，帮助"保存在系统钥匙串中。"）。
- 「完成设置」：两项为空时各自显示必填提示；Base URL 不以 https:// 开头时在字段下显示"Base URL 要以 https:// 开头。"，不发请求；否则按钮变"正在连接中台…"并禁用 → 调 save_connection。失败 → 顶部错误提示"没能连上中台"+错误码说明，输入保留。成功 → 存钥匙串和设置 → set_forwarding true → 注册登录启动 → 显示成功提示"设置完成，转发已开启"，正文"已获取 N 个中台模型。重启 ChatGPT 后……助手会在你登录电脑时自动启动，在菜单栏里常驻。"，按钮「稍后自己重启」「立即重启 ChatGPT」。

**主窗口**（标题"模型接入助手"）
- 「转发」卡片：开关"转发到中台"+"已开启/已关闭"文字和帮助说明。ChatGPT 在运行时拨动 → 弹窗（打开："打开转发并重启 ChatGPT？"；关闭："关闭转发并重启 ChatGPT？"，正文说明重启会中断 Codex 里正在进行的任务；按钮「取消」「打开并重启 / 关闭并重启」）→ 确认后显示"正在还原 Codex 配置并重启 ChatGPT"→ 完成。ChatGPT 没运行 → 直接切换。找不到 ChatGPT → 开关禁用，顶部错误"没有找到 ChatGPT"和「重新检测」。
- 「中台连接」卡片：Base URL、API Key（遮蔽、显隐），「保存修改」→"正在保存并重新获取模型…"→ 列表变化且 ChatGPT 在运行：弹窗"已保存，重启 ChatGPT 以更新模型列表？"（「稍后」「立即重启」）；没变化：成功提示"已保存，不需要重启 ChatGPT"；失败：错误"没有保存"+原因，输入保留。
- 「运行状态」卡片：ChatGPT（已检测到/未找到）、中台模型（"已获取 N 个，<时间> 更新" / 沿用上次 / 暂无 / 转发关闭时不获取）。
- 顶部提示条（按 Status）：taken_over 非空 → 信息"已接管 Codex 原有的接入设置"；relay.stale → 警告"中台模型列表本次没有更新"+原因、上次时间、「重试」；relay 从未成功 → 错误"还没有获取到中台模型"+原因、「重试」；restore_blocked 非空 → 错误"没能自动还原 Codex 配置"+键名和 config_path、「打开所在文件夹」「重新检查」；proxy.error → 错误"本机转发服务没能启动"+「重试」。
- 底部状态区：`v<版本>`、运行状态、「检查更新」；有新版时显示"发现新版本 X"和「打开下载页」。
- 关闭按钮只隐藏窗口。

**菜单栏菜单**（图标点击弹出）
- 标题"模型接入助手"+"转发已开启/已关闭 · 后台运行中"
- 「转发到中台」（勾选，规则同主窗口开关）、「打开配置…」(⌘,)
- 「登录时启动」（勾选；转发开着时取消 → 弹窗"关闭「登录时启动」？"，按钮「取消」「仍然关闭」）、「检查更新」或"发现新版本 X · 打开下载页"
- 「卸载模型接入助手…」、「退出模型接入助手」(⌘Q)
- 退出：转发开着 → 弹窗"退出模型接入助手？"（「取消」「还原并退出」）→ shutdown restore:true；转发关着直接 shutdown。
- 卸载：弹窗列出会删除和不会删除的内容（「取消」「卸载」）→"正在卸载"→ prepare_uninstall；restore_blocked → 弹窗"卸载暂停：有设置被手动改过"+键名、配置路径（「取消卸载」「打开所在文件夹」「重新检查」）；成功 → 删除钥匙串 Key、shell-settings、注销登录启动 → 弹窗"已卸载模型接入助手"（「稍后自己重启」「立即重启 ChatGPT」）→ 把 ModelBridge.app 移到废纸篓 → 退出。
- 手动检查更新：已是最新 → "已是最新版本"；失败 → "暂时无法检查更新，请稍后再试"。
- 界面任何地方都不显示中台模型名。

## 涉及文件
- macos/Sources/ModelBridge/App.swift（修改：菜单栏入口、窗口管理）
- macos/Sources/ModelBridge/UI/AppModel.swift（新建：界面状态与动作）
- macos/Sources/ModelBridge/UI/SetupWindow.swift（新建）
- macos/Sources/ModelBridge/UI/MainWindow.swift（新建）
- macos/Sources/ModelBridge/UI/StatusMenu.swift（新建）
- macos/Sources/ModelBridge/UI/Dialogs.swift（新建）
- macos/Sources/ModelBridge/UI/Components.swift（新建：按钮、输入框、开关、卡片、提示条按 DESIGN.md）
- macos/Tests/ModelBridgeTests/AppModelTests.swift（新建）

## 函数清单
### UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `AppModel.completeSetup` | 首次设置流程：校验 → save_connection → 存储 → 开转发 → 注册登录启动 |
| `AppModel.toggleForwarding` | 按 ChatGPT 是否运行决定直接切换或请求确认，确认后切换并重启 |
| `AppModel.saveConnection` | 保存修改流程与三种结果 |
| `AppModel.setLoginStart` | 勾选/取消登录启动，转发开着时取消先请求确认 |
| `AppModel.quit` / `uninstall` | 退出与卸载流程 |
| `AppModel.checkUpdate` | 手动检查并给出三种提示 |
| `AppModel.banners` | 由 Status 推出顶部提示条列表 |
### UI/SetupWindow.swift、MainWindow.swift、StatusMenu.swift、Dialogs.swift、Components.swift
| 函数名 | 职责 |
|---|---|
| 各 SwiftUI 视图 | 按「业务规则」渲染文案、状态和交互，只调用 AppModel |

## 协作关系
视图 → AppModel → 系统层（08 的 CoreProcess、KeychainStore、ShellSettings、LoginItem、ChatGPTApp）。

## 验证方式
- 测试入口：`cd macos && swift test`（AppModel 用假系统层测试）；界面外观在 /exec 收尾时与原型逐项对比
- 测试输入：假 CoreProcess（可设定返回结果和 Status）、假 ChatGPTApp（运行/未运行）
- 预期结果：
- [ ] completeSetup：空字段 → 两个必填错误、不调用核心；`relay.example.com` → Base URL 格式错误、不调用核心；核心返回 relay_401 → 任务错误文字正确、未写钥匙串；成功 → 依次调用 save_connection、钥匙串保存、set_forwarding true、登录启动注册
- [ ] toggleForwarding：ChatGPT 运行 → 进入"待确认"，取消后不调用核心；确认后调用 set_forwarding 并 restart；未运行 → 直接调用、不 restart
- [ ] saveConnection：models_changed 且运行中 → 弹重启询问；未变化 → "已保存，不需要重启 ChatGPT"；失败 → "没有保存"且输入保留
- [ ] setLoginStart：转发开着时取消 → 先确认
- [ ] quit：转发开着 → 确认后 shutdown restore:true；关着 → 直接 shutdown restore:false
- [ ] uninstall：restore_blocked → 不删钥匙串、不注销登录启动；成功 → 三者都清理
- [ ] banners：各 Status 组合推出规定的提示条
