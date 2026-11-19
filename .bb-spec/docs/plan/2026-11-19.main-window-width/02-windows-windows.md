---
name: 02-windows-windows
description: Windows 主窗口和首次设置窗口固定 760px，最大化变灰，状态区每项只排一行，有新版本时最右是一个按钮
---
# Windows 窗口加宽与状态区排版

## 目标
Windows 的主窗口和首次设置窗口固定 760px 宽、不能拖动改宽，主窗口「最大化」不可用，底部状态区排成一行，有新版本时最右只有一个按钮「新版本 <版本号>」。

## 业务规则（来源：PRD 01-app-windows）
- 主窗口、首次设置窗口宽 760px，用户不能拖动改变宽度；高度随内容变化。
- 主窗口标题栏的「最大化」不可用（变灰，点了没反应），和首次设置窗口现在一样；首次设置窗口保持「最大化」不可用。
- 底部状态区：版本号、运行状态、「ChatGPT」+ 取值、「中转模型」+ 取值，最右一个文字按钮；每一项都只排一行，不折行、不被裁掉。
- 没有新版本时最右是「检查更新」，行为不变。
- 有新版本时最右只有一个文字按钮「新版本 <版本号>」，点了打开下载页；可访问名称（AutomationProperties.Name）是「发现新版本 <版本号>，打开下载页」。不再显示「发现新版本 X」文字和「打开下载页」按钮。
- 其他内容、文字、交互不变；托盘菜单里的「发现新版本 X / 打开下载页」不在本次范围。

## 涉及文件
- 修改 `windows/ModelBridge.App/UI/MainWindow.xaml`
- 修改 `windows/ModelBridge.App/UI/MainWindow.xaml.cs`
- 修改 `windows/ModelBridge.App/UI/SetupWindow.xaml`
- 修改 `windows/ModelBridge.Tests/MainWindowLayoutTests.cs`（宽度断言由 620 改为 760）

## 函数清单
### windows/ModelBridge.App/UI/MainWindow.xaml
| 函数名 | 职责 |
|---|---|
| `Window` 属性 | 宽 760、不能拖动改宽（去掉 MinWidth 可拖窄，宽度固定） |
| `MaximizeButton` | 不可用 |
| 底部状态区 `UpdateText` / `UpdateButton` | 只保留一个按钮，删掉 `UpdateText` |
### windows/ModelBridge.App/UI/MainWindow.xaml.cs
| 函数名 | 职责 |
|---|---|
| 刷新状态区的现有方法（第 76-78 行附近） | 按钮文字：无新版本「检查更新」，有新版本「新版本 X」；有新版本时设可访问名称 |
| `MaximizeClick` | 本次改动导致不再使用则删除 |
### windows/ModelBridge.App/UI/SetupWindow.xaml
| 函数名 | 职责 |
|---|---|
| `Window` 属性 | 宽 760，保持不可拖动改宽 |

## 协作关系
`UpdateClick` 现有逻辑：无新版本时检查更新，有新版本时打开下载页，保持不变。窗口用 `WindowChrome` 自绘标题栏。

## 验证方式
- 测试入口：`windows/ModelBridge.Tests`（MSTest，`UiTestHost` 在 STA 线程里建窗口并量 `ActualWidth`；`MainWindowLayoutTests` 有宽度用例可参考）。**Windows 测试只能在 CI 上跑**；本机只能 `dotnet build windows/ModelBridge.sln -p:EnableWindowsTargeting=true` 做编译检查。
- 测试输入：主窗口用测试模型，分别是无新版本、有新版本 0.2.0；首次设置窗口。
- 预期结果：
- [ ] 主窗口 `ActualWidth` 760（误差 0.5）；首次设置窗口 760。
- [ ] 主窗口不能被拖动改宽：`ResizeMode` 不允许改宽，或 `MinWidth == MaxWidth == 760`。
- [ ] `MaximizeButton.IsEnabled == false`。
- [ ] 有新版本 0.2.0：`UpdateButton.Content == "新版本 0.2.0"`，`AutomationProperties.Name == "发现新版本 0.2.0，打开下载页"`；窗口里没有文字「发现新版本 0.2.0」「打开下载页」。无新版本：`UpdateButton.Content == "检查更新"`。
- [ ] 状态区高度和单行时相同（版本、各项、按钮都没折行）。
- [ ] 本机编译通过；CI 上 Windows 测试全部通过。
