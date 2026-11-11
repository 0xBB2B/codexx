---
name: 11-windows-ui
description: Windows 外壳界面：首次设置窗口、主窗口、托盘菜单和各种确认弹窗
---
# Windows 外壳：界面

## 目标
Windows 上有原生的首次设置窗口、主窗口和任务栏通知区域菜单，状态和弹窗与已确认原型一致。

## 业务规则（来源：PRD 01、02、03、04、05、06；已确认原型）
样式全部按仓库根目录 `DESIGN.md`：主色 #0f766e、窗口宽 620px、分组卡片、按钮和输入框高 44px、菜单宽 286px、菜单项最小高 44px、聚焦描边 2px；窗口用系统标题栏（最小化、最大化、关闭）。

**首次设置窗口**（标题"设置模型接入助手"；没有保存过 Base URL 时打开应用就显示）
- 标题"连接到中台"，说明"填写中台提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中台模型。"
- 必填 Base URL（占位 `https://relay.example.com/v1`，帮助"以 https:// 开头的完整地址。"）、API Key（默认遮蔽、显隐按钮，帮助"保存在系统凭据管理器中。"）。
- 「完成设置」：空字段 → 各自必填提示；不以 https:// 开头 → "Base URL 要以 https:// 开头。"，不发请求；否则"正在连接中台…"并禁用 → save_connection；失败 → 顶部"没能连上中台"+错误码说明、输入保留；成功 → 存凭据和设置 → set_forwarding true → 写登录启动 → 成功提示"设置完成，转发已开启"，正文"已获取 N 个中台模型……助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。"，按钮「稍后自己重启」「立即重启 ChatGPT」。

**主窗口**（标题"模型接入助手"）
- 「转发」卡片：开关"转发到中台"+"已开启/已关闭"。ChatGPT 运行时拨动 → 弹窗"打开转发并重启 ChatGPT？"/"关闭转发并重启 ChatGPT？"（说明会中断 Codex 当前任务；「取消」「打开并重启/关闭并重启」）→"正在还原 Codex 配置并重启 ChatGPT"。未运行 → 直接切换。找不到 ChatGPT → 开关禁用，顶部"没有找到 ChatGPT"+「重新检测」。
- 「中台连接」卡片：Base URL、API Key（遮蔽、显隐）、「保存修改」→"正在保存并重新获取模型…"→ 变化且运行中：弹窗"已保存，重启 ChatGPT 以更新模型列表？"（「稍后」「立即重启」）；无变化："已保存，不需要重启 ChatGPT"；失败："没有保存"+原因、输入保留。
- 「运行状态」卡片：ChatGPT、中台模型（"已获取 N 个，<时间> 更新"等）。
- 顶部提示条按 Status：taken_over → "已接管 Codex 原有的接入设置"；relay.stale → "中台模型列表本次没有更新"+原因、上次时间、「重试」；从未成功 → "还没有获取到中台模型"+「重试」；restore_blocked → "没能自动还原 Codex 配置"+键名、config_path、「打开所在文件夹」「重新检查」；proxy.error → "本机转发服务没能启动"+「重试」。
- 底部状态区：`v<版本>`、运行状态、「检查更新」/"发现新版本 X"+「打开下载页」。关闭按钮只隐藏窗口。

**托盘菜单**（左键或右键点通知区域图标弹出，菜单在图标上方）
- 标题"模型接入助手"+"转发已开启/已关闭 · 后台运行中"
- 「转发到中台」（勾选）、「打开配置…」
- 「登录时启动」（勾选；转发开着时取消 → 弹窗"关闭「登录时启动」？"「取消」「仍然关闭」）、「检查更新」或"发现新版本 X · 打开下载页"
- 「卸载模型接入助手…」、「退出模型接入助手」
- 退出：转发开着 → 弹窗"退出模型接入助手？"（「取消」「还原并退出」）→ shutdown restore:true。
- 卸载（菜单或 `--uninstall`）：说明弹窗（「取消」「卸载」）→"正在卸载"→ prepare_uninstall；restore_blocked → "卸载暂停：有设置被手动改过"（「取消卸载」「打开所在文件夹」「重新检查」），进程以退出码 2 结束；成功 → 删凭据、shell-settings、登录启动值 → "已卸载模型接入助手"（「稍后自己重启」「立即重启 ChatGPT」）→ 菜单发起时启动安装目录里的卸载程序 `unins000.exe /SILENT` 删除程序文件；`--uninstall` 发起时以退出码 0 结束。
- 手动检查更新："已是最新版本" / "暂时无法检查更新，请稍后再试"。
- 任何地方不显示中台模型名。

## 涉及文件
- windows/ModelBridge.App/App.xaml.cs（修改：解析 --login、--uninstall，托盘入口）
- windows/ModelBridge.App/UI/AppModel.cs（新建）
- windows/ModelBridge.App/UI/SetupWindow.xaml(.cs)（新建）
- windows/ModelBridge.App/UI/MainWindow.xaml(.cs)（新建）
- windows/ModelBridge.App/UI/TrayMenu.cs（新建：NotifyIcon + 自绘 WPF 弹出菜单）
- windows/ModelBridge.App/UI/Dialogs.xaml(.cs)（新建）
- windows/ModelBridge.App/UI/Theme.xaml（新建：按 DESIGN.md 的颜色、尺寸、控件样式）
- windows/ModelBridge.Tests/AppModelTests.cs（新建）

## 函数清单
### UI/AppModel.cs
| 函数名 | 职责 |
|---|---|
| `AppModel.CompleteSetupAsync` | 首次设置流程 |
| `AppModel.ToggleForwardingAsync` | 直接切换或请求确认，确认后切换并重启 ChatGPT |
| `AppModel.SaveConnectionAsync` | 保存修改与三种结果 |
| `AppModel.SetLoginStart` | 勾选或取消，转发开着时取消先确认 |
| `AppModel.QuitAsync` / `UninstallAsync` | 退出与卸载流程，卸载返回退出码 |
| `AppModel.CheckUpdateAsync` | 手动检查与提示 |
| `AppModel.Banners` | 由 Status 推出顶部提示条 |
### UI 各窗口与 TrayMenu
| 函数名 | 职责 |
|---|---|
| 各 XAML 视图与 `TrayMenu.Show` | 按「业务规则」渲染与交互，只调用 AppModel |

## 协作关系
视图 → AppModel → 系统层（10）。安装程序（12）卸载时调用 `ModelBridge.exe --uninstall` 并按退出码决定是否继续。

## 验证方式
- 测试入口：`dotnet test windows/ModelBridge.sln`（AppModel 用假系统层）；界面在用户的 Windows 电脑上按验收清单实机检查
- 测试输入：假 CoreProcess、假 ChatGPTApp、假 CredentialStore、假 LoginItem
- 预期结果：
- [ ] CompleteSetupAsync：空字段、错误格式不调用核心；relay_401 → 错误文字正确、未存凭据；成功 → 依次 save_connection、存凭据、set_forwarding true、写登录启动
- [ ] ToggleForwardingAsync：运行中需确认，取消不调用核心；未运行直接切换不重启
- [ ] SaveConnectionAsync 三种结果符合规定
- [ ] SetLoginStart：转发开着取消时先确认
- [ ] QuitAsync：转发开着 → 确认后 shutdown restore:true
- [ ] UninstallAsync：restore_blocked → 返回 2、不清理；成功 → 清理凭据、设置、登录启动并返回 0
- [ ] Banners：各 Status 组合推出规定提示条
