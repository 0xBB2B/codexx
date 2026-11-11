---
name: 08-macos-system
description: macOS 外壳的系统层：启动并对接核心程序、钥匙串、本机设置、登录时启动、单实例、检测和重启 ChatGPT
---
# macOS 外壳：系统层

## 目标
macOS 外壳能启动核心程序并按协议通信，能把 Key 存进钥匙串、管理登录时启动、保证只运行一份、检测和重启 ChatGPT。

## 业务规则（来源：PRD 01、03、04、05；AGENTS.md）
- 核心程序位于 `ModelBridge.app/Contents/MacOS/model-bridge-core`，以子进程启动：`model-bridge-core --data-dir ~/Library/Application Support/ModelBridge`，通过标准输入输出逐行 JSON 通信。协议以仓库里的 `core/PROTOCOL.md` 为准（方法 init、status、save_connection、set_forwarding、refresh_models、check_update、prepare_uninstall、shutdown；事件 status）。
- 中台 Key 只存在钥匙串的通用密码里（service `ModelBridge`，account `relay-api-key`），读出后只经标准输入交给核心；不写任何文件、日志、命令行参数或环境变量。只有外壳访问钥匙串。
- Base URL 存在 `~/Library/Application Support/ModelBridge/shell-settings.json` 的 `base_url` 字段。
- 登录时启动用 `SMAppService.mainApp`：首次设置成功后注册；菜单勾选或取消时注册或注销；勾选状态读它的当前状态。不需要管理员权限。
- 应用是菜单栏应用（Info.plist 设 `LSUIElement = true`），登录启动时只在后台运行、不弹主窗口；用户手动打开时（非登录启动）显示主窗口。
- 单实例：启动时发现已有同 bundle id 的实例在运行 → 通知那个实例显示主窗口，自己退出。
- ChatGPT 检测：bundle id `com.openai.codex` 的运行中应用；安装目录按 `/Applications/ChatGPT.app`、`~/Applications/ChatGPT.app` 顺序找。
- 重启 ChatGPT：请求它正常退出，最多等 30 秒；退出后用 NSWorkspace 重新打开；超时则报告"ChatGPT 没有退出"，不强制结束。
- 核心进程意外退出 → 外壳 2 秒后重启它一次并重新 init；再失败则在主窗口显示错误。
- 最低系统 macOS 13，只编译 arm64。

## 涉及文件
- macos/Sources/ModelBridge/System/CoreProcess.swift（新建）
- macos/Sources/ModelBridge/System/CoreProtocol.swift（新建）
- macos/Sources/ModelBridge/System/KeychainStore.swift（新建）
- macos/Sources/ModelBridge/System/ShellSettings.swift（新建）
- macos/Sources/ModelBridge/System/LoginItem.swift（新建）
- macos/Sources/ModelBridge/System/SingleInstance.swift（新建）
- macos/Sources/ModelBridge/System/ChatGPTApp.swift（新建）
- macos/Resources/Info.plist（新建）
- macos/Tests/ModelBridgeTests/*Tests.swift（新建，与实现文件一一对应）

## 成品定义

`macos/Resources/Info.plist`（打包脚本把版本号替换进 `__VERSION__`）：
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>io.github.modelbridge.app</string>
  <key>CFBundleName</key><string>ModelBridge</string>
  <key>CFBundleDisplayName</key><string>模型接入助手</string>
  <key>CFBundleExecutable</key><string>ModelBridge</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>__VERSION__</string>
  <key>CFBundleVersion</key><string>__VERSION__</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
  <key>LSArchitecturePriority</key><array><string>arm64</string></array>
  <key>NSHumanReadableCopyright</key><string>Open source</string>
</dict>
</plist>
```

## 函数清单
### System/CoreProcess.swift
| 函数名 | 职责 |
|---|---|
| `CoreProcess.start` | 启动核心子进程并建立管道 |
| `CoreProcess.call` | 发送请求、按 id 等待响应（异步） |
| `CoreProcess.onStatus` | 订阅 status 事件 |
| `CoreProcess.handleExit` | 意外退出时重启一次并重新 init |
### System/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `CoreRequest` / `CoreResponse` / `CoreStatus` / `CoreErrorCode` | 协议的 Codable 类型，错误码到界面文字的映射表 |
### System/KeychainStore.swift
| 函数名 | 职责 |
|---|---|
| `KeychainStore.save` / `load` / `delete` | 读写删除钥匙串里的中台 Key |
### System/ShellSettings.swift
| 函数名 | 职责 |
|---|---|
| `ShellSettings.load` / `save` / `deleteAll` | 读写删除 shell-settings.json |
### System/LoginItem.swift
| 函数名 | 职责 |
|---|---|
| `LoginItem.isEnabled` / `enable` / `disable` | 包装 SMAppService.mainApp |
| `LoginItem.launchedAtLogin` | 判断本次是否由登录启动 |
### System/SingleInstance.swift
| 函数名 | 职责 |
|---|---|
| `SingleInstance.claimOrForward` | 已有实例时通知它显示主窗口并返回"应退出" |
### System/ChatGPTApp.swift
| 函数名 | 职责 |
|---|---|
| `ChatGPTApp.installedPath` | 查找安装目录 |
| `ChatGPTApp.isRunning` | 是否在运行 |
| `ChatGPTApp.restart` | 正常退出、等待最多 30 秒、重新打开 |

## 协作关系
界面层（09）通过一个持有 CoreProcess、KeychainStore、ShellSettings、LoginItem、ChatGPTApp 的对象调用系统层。启动顺序：SingleInstance → 读设置和 Key → CoreProcess.start → init。依赖系统框架 Security、ServiceManagement、AppKit。

## 验证方式
- 测试入口：`cd macos && swift test`
- 测试输入：假核心程序（测试里用一个打印固定 JSON 的脚本当子进程）；测试专用钥匙串 service 名；临时设置目录
- 预期结果：
- [ ] call 能按 id 收到响应；status 事件能被订阅收到；假核心退出后会被重启一次并重新收到 init
- [ ] 传给子进程的命令行参数和环境变量里没有 Key（测试脚本把自己的 argv 和 env 打印到文件后检查）
- [ ] Keychain save → load 得到同值；delete 后 load 为空
- [ ] ShellSettings 只含 base_url，文件里搜不到 Key
- [ ] 错误码映射表覆盖 PROTOCOL.md 里全部错误码，文字与之一致
- [ ] ChatGPTApp.restart 在目标应用 30 秒不退出时返回超时且不强制结束（用可替换的应用接口测试）
