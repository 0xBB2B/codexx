---
name: 01-macos-windows
description: macOS 主窗口和首次设置窗口固定 760px，底部状态区每项只排一行，有新版本时最右是一个按钮
---
# macOS 窗口加宽与状态区排版

## 目标
macOS 的主窗口和首次设置窗口固定 760px 宽，底部状态区在所有状态下排成一行，有新版本时最右只有一个按钮「新版本 <版本号>」。

## 业务规则（来源：PRD 01-app-windows）
- 主窗口、首次设置窗口宽 760px，用户不能拖动改变宽度（窗口本来就不可拖动改宽，保持不变）。
- 底部状态区从左到右：版本号「v<版本>」、圆点 + 运行状态文字、「ChatGPT」+ 取值、「中转模型」+ 取值（可带括号说明），最右是一个文字按钮。每一项都只排一行，不折行、不截断。
- 没有新版本时，最右是「检查更新」，点了检查更新，行为不变。
- 有新版本时，最右只有一个文字按钮「新版本 <版本号>」（如「新版本 0.2.0」），点了打开下载页；它的可访问名称是「发现新版本 <版本号>，打开下载页」。界面上不再显示「发现新版本 X」文字和「打开下载页」按钮。
- 文字最长时也要一行放下：版本 `0.10.0-local`、后台服务已停止、ChatGPT 版本暂不支持、转发关闭（中转模型写「转发关闭时不获取」）、有新版本 0.11.0。
- 窗口里其他内容、文字、交互都不变。菜单栏菜单里的「发现新版本 X / 打开下载页」不在本次范围，不改。

## 涉及文件
- 修改 `macos/Sources/ModelBridge/UI/MainWindow.swift`
- 修改 `macos/Sources/ModelBridge/UI/SetupWindow.swift`
- 修改 `macos/Tests/ModelBridgeTests/MultiRelayViewTests.swift`（宽度断言由 620 改为 760）

## 函数清单
### macos/Sources/ModelBridge/UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| `MainWindow.body` | 根视图宽度改为 760 |
| `StatusFooter.body` | 版本号、各项、最右按钮都只排一行；有新版本时最右只放一个「新版本 X」按钮，带可访问名称，点了打开下载页 |
| `StatusFooter.item` | 让「标签 + 取值」这一项不折行 |
### macos/Sources/ModelBridge/UI/SetupWindow.swift
| 函数名 | 职责 |
|---|---|
| `SetupWindow.body` | 根视图宽度改为 760 |

## 协作关系
`AppWindows.makeMain` / `makeSetup` 用视图的 `fittingSize` 建窗口，窗口宽度跟着根视图走；`StatusFooter` 读 `AppModel.availableUpdate`（`UpdateInfo.version`、`url`），打开下载页用 `NSWorkspace.shared.open`。

## 验证方式
- 测试入口：`macos` 包的 XCTest，沿用 `ViewHarness.swift` 里的 `RenderedView` 渲染视图并量尺寸；现有 `MultiRelayViewTests` 里有主窗口宽度和 `StatusFooter` 的渲染用例可参考。
- 测试输入：用测试夹具构造 `AppModel`，分别设成：正常（版本 `0.1.0-local`、核心运行中、ChatGPT 已检测到、1 个配置 17 个模型、无新版本）；有新版本 0.2.0；文字最长时（版本 `0.10.0-local`、核心停止、ChatGPT 版本暂不支持、转发关闭、有新版本 0.11.0）。
- 预期结果：
- [ ] `MainWindow` 渲染宽 760（误差 1）；`SetupWindow` 渲染宽 760（误差 1）。
- [ ] 三种数据下，`StatusFooter` 放在 760 宽里渲染，高度等于单行高度（与「正常」状态同高，约 48），即没有任何一项折行。
- [ ] 有新版本时，状态区里能找到可访问名称为「发现新版本 0.2.0，打开下载页」、文字为「新版本 0.2.0」的按钮；找不到「发现新版本 0.2.0」文字和「打开下载页」按钮。
- [ ] 无新版本时最右是「检查更新」。
- [ ] `cd macos && swift test` 全部通过。
