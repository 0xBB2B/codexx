---
name: 10-windows-system
description: Windows 外壳的系统层：对接核心程序、凭据管理器、本机设置、登录启动、单实例、检测和重启 ChatGPT
---
# Windows 外壳：系统层

## 目标
Windows 外壳能启动核心程序并按协议通信，能把 Key 存进凭据管理器、管理登录时启动、只运行一份、检测和重启 ChatGPT。

## 业务规则（来源：PRD 01、03、04、05；AGENTS.md）
- 核心程序和外壳在同一安装目录：`model-bridge-core.exe`，以子进程启动：`model-bridge-core.exe --data-dir %LOCALAPPDATA%\ModelBridge`，不显示控制台窗口；标准输入输出逐行 JSON。协议以仓库里的 `core/PROTOCOL.md` 为准（方法 init、status、save_connection、set_forwarding、refresh_models、check_update、prepare_uninstall、shutdown；事件 status）。init 时把 ChatGPT 安装目录作为 `chatgpt_dir` 传给核心。
- 中台 Key 只存在凭据管理器的通用凭据里（目标名 `ModelBridge/relay-api-key`，CredWrite/CredRead/CredDelete，持久化类型 LOCAL_MACHINE 以外的 ENTERPRISE 不用，用 CRED_PERSIST_LOCAL_MACHINE），读出后只经标准输入交给核心；不写文件、日志、命令行参数、环境变量。
- Base URL 存在 `%LOCALAPPDATA%\ModelBridge\shell-settings.json` 的 `base_url`。
- 登录时启动：写当前用户注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，值名 `ModelBridge`，内容 `"<安装目录>\ModelBridge.exe" --login`；取消时删除该值。不需要管理员权限。带 `--login` 启动时只在后台运行、不弹主窗口。
- 单实例：命名互斥量 `Local\ModelBridge.SingleInstance`；已有实例 → 通过命名管道 `ModelBridge.Activate` 通知它显示主窗口，自己退出。
- ChatGPT 检测：先看运行中的 `ChatGPT.exe` / `Codex.exe` 进程路径；再按 `%LOCALAPPDATA%` 下 `Programs\ChatGPT`、`Programs\OpenAI ChatGPT`、`ChatGPT`、`OpenAI\ChatGPT`、`Programs\Codex`、`OpenAI Codex`（含 `app-*` 子目录）查 `ChatGPT.exe` / `Codex.exe`；再查当前用户已安装的应用商店包里名称含 `ChatGPT` 的包的安装目录。**应用商店安装位置需要在用户的 Windows 电脑上实测确认，实测结果记入 PROGRESS.md。**
- 重启 ChatGPT：对其主窗口发送正常关闭，最多等 30 秒；退出后用找到的路径（应用商店包用其应用 ID）重新启动；超时报告"ChatGPT 没有退出"，不强制结束。
- 核心进程意外退出 → 2 秒后重启一次并重新 init；再失败在主窗口显示错误。
- 支持 64 位 Windows 10 及以上。

## 涉及文件
- windows/ModelBridge.App/System/CoreProcess.cs（新建）
- windows/ModelBridge.App/System/CoreProtocol.cs（新建）
- windows/ModelBridge.App/System/CredentialStore.cs（新建）
- windows/ModelBridge.App/System/ShellSettings.cs（新建）
- windows/ModelBridge.App/System/LoginItem.cs（新建）
- windows/ModelBridge.App/System/SingleInstance.cs（新建）
- windows/ModelBridge.App/System/ChatGPTApp.cs（新建）
- windows/ModelBridge.Tests/*Tests.cs（新建，与实现文件一一对应）

## 函数清单
### System/CoreProcess.cs
| 函数名 | 职责 |
|---|---|
| `CoreProcess.Start` | 无窗口启动核心子进程并建立管道 |
| `CoreProcess.CallAsync` | 发请求、按 id 等响应 |
| `CoreProcess.StatusChanged` | status 事件 |
| `CoreProcess.OnExited` | 意外退出时重启一次并重新 init |
### System/CoreProtocol.cs
| 函数名 | 职责 |
|---|---|
| `CoreRequest` / `CoreResponse` / `CoreStatus` / `CoreErrorText` | 协议类型（System.Text.Json）和错误码到界面文字的映射 |
### System/CredentialStore.cs
| 函数名 | 职责 |
|---|---|
| `CredentialStore.Save` / `Load` / `Delete` | 通过 P/Invoke 调 CredWrite/CredRead/CredDelete |
### System/ShellSettings.cs
| 函数名 | 职责 |
|---|---|
| `ShellSettings.Load` / `Save` / `DeleteAll` | 读写删除 shell-settings.json |
### System/LoginItem.cs
| 函数名 | 职责 |
|---|---|
| `LoginItem.IsEnabled` / `Enable` / `Disable` | 读写 HKCU Run 值 |
### System/SingleInstance.cs
| 函数名 | 职责 |
|---|---|
| `SingleInstance.ClaimOrForward` | 互斥量判定，已有实例时经命名管道通知并返回"应退出" |
| `SingleInstance.ListenForActivation` | 主实例监听激活请求 |
### System/ChatGPTApp.cs
| 函数名 | 职责 |
|---|---|
| `ChatGPTApp.FindInstall` | 按规则查找安装目录或应用商店包 |
| `ChatGPTApp.IsRunning` | 是否在运行 |
| `ChatGPTApp.RestartAsync` | 正常关闭、等最多 30 秒、重新启动 |

## 协作关系
界面层（11）通过持有以上对象的 AppModel 调用；启动顺序：SingleInstance → 读设置和 Key → CoreProcess.Start → init。卸载入口（12 的安装程序）以 `ModelBridge.exe --uninstall` 调用同一套流程。

## 验证方式
- 测试入口：`dotnet test windows/ModelBridge.sln`（windows-2025 构建机）
- 测试输入：假核心（测试用小控制台程序或脚本）；测试专用凭据目标名；测试专用注册表值名；临时目录
- 预期结果：
- [ ] CallAsync 按 id 收到响应；status 事件可订阅；假核心退出后重启一次并重新 init
- [ ] 子进程的命令行和环境变量里没有 Key（假核心把收到的参数和环境写文件后检查）
- [ ] CredentialStore 存取删正确
- [ ] LoginItem Enable 后 HKCU Run 下有规定的值；Disable 后没有
- [ ] 错误码映射覆盖 PROTOCOL.md 全部错误码，文字一致
- [ ] FindInstall 能在构造好的 %LOCALAPPDATA% 目录结构里找到 `app-*` 子目录下的 ChatGPT.exe
- [ ] RestartAsync 在目标 30 秒不退出时返回超时且不强制结束（可替换的进程接口）
