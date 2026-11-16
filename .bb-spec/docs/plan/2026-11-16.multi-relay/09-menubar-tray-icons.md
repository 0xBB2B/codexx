---
name: 09-menubar-tray-icons
description: macOS 菜单栏改用模板图标，Windows 托盘改用单色图标并跟随任务栏深浅色切换
---
# 菜单栏与托盘图标

## 目标
macOS 菜单栏、Windows 托盘显示应用图标的单色版：macOS 跟随系统自动变色，Windows 按任务栏深浅在两套颜色间切换。

## 业务规则（来源：PRD 04-app-icon）
- macOS：菜单栏图标 18×18（@2x 36×36），用 `MenuBarIcon.png` / `MenuBarIcon@2x.png`（由图标生成程序产出，打包时放在 App 包 `Contents/Resources`），设为模板图标，颜色由系统按菜单栏深浅自动换。替换现在的系统符号 `arrow.left.arrow.right.circle`。
- Windows：托盘图标 16×16（高 DPI 用 32×32），浅色任务栏用深色 #172b3a 版（`TrayIconLight.ico`），深色任务栏用白色版（`TrayIconDark.ico`）。任务栏深浅以注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize` 的 `SystemUsesLightTheme` 为准（1 为浅色，缺省按深色）；系统发出设置变化通知后重新读取并更新托盘图标，不用重启本工具。替换现在运行时画的青色实心圆。
- 窗口左上角标题栏、任务栏使用 exe 的应用图标。

## 涉及文件
- 修改 `macos/Sources/ModelBridge/App.swift`（菜单栏图标）、`macos/Package.swift`（如采用 SwiftPM 资源方式加载）
- 修改 `windows/ModelBridge.App/UI/TrayMenu.cs`
- 新建 `windows/ModelBridge.App/System/TaskbarTheme.cs`
- 新建 `windows/ModelBridge.Tests/TaskbarThemeTests.cs`；修改 `macos/Tests/ModelBridgeTests/SmokeTests.swift`（如需）

## 函数清单
### macos/Sources/ModelBridge/App.swift
| 函数名 | 职责 |
|---|---|
| `menuBarImage` | 读取菜单栏图标资源并设为模板图片 |
| 状态栏按钮设置处 | 改用 `menuBarImage` |
### windows/ModelBridge.App/System/TaskbarTheme.cs
| 函数名 | 职责 |
|---|---|
| `TaskbarTheme.IsLight` | 从注册表值判断任务栏是否浅色（值读取通过可替换的读取函数，便于测试） |
| `TaskbarTheme.IconFor` | 按深浅返回对应托盘图标资源名 |
### windows/ModelBridge.App/UI/TrayMenu.cs
| 函数名 | 职责 |
|---|---|
| `UpdateTrayIcon` | 设置托盘图标 |
| 设置变化订阅 | 收到用户偏好变化时调用 `UpdateTrayIcon` |

## 协作关系
依赖 plan 08 生成的 `MenuBarIcon*.png`、`TrayIcon*.ico`。

## 验证方式
- 测试入口：macOS `cd macos && swift test`（菜单栏图片可加载、是模板图片）；Windows 在 CI 上 `dotnet test --filter TaskbarThemeTests`（推临时分支前征得用户同意）。
- 预期结果：
  - [ ] macOS：`menuBarImage` 返回非空、`isTemplate` 为 true、尺寸 18×18 点。
  - [ ] Windows：读取函数返回 1 → `IsLight` 为 true、`IconFor` 返回 `TrayIconLight.ico`；返回 0 或读不到 → 深色版。
  - [ ] Windows 源码里不再有画青色实心圆的代码。
  - [ ] 真机：切换深浅色模式，菜单栏、托盘图标跟着变（记入待用户验证）。
