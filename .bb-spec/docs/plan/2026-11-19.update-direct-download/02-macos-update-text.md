---
name: 02-macos-update-text
description: macOS 端按核心给的 installer 字段写更新项副标题和状态区可访问名称
---
# macOS 端更新提示文字

## 目标
macOS 菜单栏面板的更新项和主窗口状态区按钮，按核心返回的是安装包还是发布页显示对应文字；点击仍用默认浏览器打开核心给的地址。

## 业务规则（来源：PRD 01-direct-download）
- 核心的 check_update 返回和 status.update 带 `installer` 布尔字段：true 表示 `url` 是本平台安装包下载地址，false 表示是发布页。字段缺失按 false。
- installer 为 true：菜单面板更新项标题「发现新版本 <版本>」、副标题「下载安装包」；状态区按钮文字「新版本 <版本>」、可访问名称「发现新版本 <版本>，下载安装包」。
- installer 为 false：副标题「打开下载页」，可访问名称「发现新版本 <版本>，打开下载页」，按钮文字仍是「新版本 <版本>」。
- 点更新项或按钮：用默认浏览器打开 `url`（现有 NSWorkspace 打开方式不变），菜单面板点完关闭（现有行为）。助手自己不下载。
- 自动检查（status）和手动检查（check_update）得到的提示一致。
- 界面布局、字号、颜色不变，只换文字。

## 涉及文件
- macos/Sources/ModelBridge/UI/AppModel.swift（修改）
- macos/Sources/ModelBridge/UI/CoreProtocol.swift（修改）
- macos/Sources/ModelBridge/UI/MenuPanelModel.swift（修改）
- macos/Sources/ModelBridge/UI/MainWindow.swift（修改）
- macos/Tests/ModelBridgeTests/AppModelTests.swift、MenuPanelModelTests.swift、MultiRelayViewTests.swift（修改）

## 函数清单
### macos/Sources/ModelBridge/UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `UpdateInfo`（加 `installer` 字段） | 记录地址是不是安装包 |
| `UpdateResult`（私有，加 `installer` 解码，缺失为 false） | 解码 check_update 返回 |
| `checkUpdate` | 发现新版时把 installer 写进 availableUpdate |
| status 处理处（约 :944） | 从 status.update 读 installer 写进 availableUpdate |

### macos/Sources/ModelBridge/UI/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `Update`（加 `installer` 解码，缺失为 false） | 解码 status.update |

### macos/Sources/ModelBridge/UI/MenuPanelModel.swift
| 函数名 | 职责 |
|---|---|
| 更新项构造处（约 :24） | 副标题按 installer 取「下载安装包」或「打开下载页」 |

### macos/Sources/ModelBridge/UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| 状态区更新按钮（约 :211-218） | 可访问名称按 installer 取两种文字 |

## 协作关系
核心 status / check_update → AppModel.availableUpdate → MenuPanelModel、MainWindow 读取并显示；点击由 StatusMenu、MainWindow 用 NSWorkspace 打开 url。

## 验证方式
- 测试入口：AppModel 的检查更新与 status 处理（现有假核心夹具）、MenuPanelModel 更新项、状态区按钮的可访问名称（现有 StatusFooterUpdateTests 写法）。
- 测试输入：check_update 返回 `{"latest_version":"0.1.2","url":"https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-macos.dmg","installer":true,"is_newer":true}`；以及 installer false、url 为发布页的版本；以及缺 installer 字段的版本；status.update 同样三种。
- 预期结果：
- [ ] installer true → 菜单更新项标题「发现新版本 0.1.2」、副标题「下载安装包」；按钮文字「新版本 0.1.2」、可访问名称「发现新版本 0.1.2，下载安装包」；availableUpdate.url 等于 dmg 地址
- [ ] installer false 或缺失 → 副标题「打开下载页」，可访问名称「发现新版本 0.1.2，打开下载页」，按钮文字「新版本 0.1.2」
- [ ] status.update 带 installer true 时结果与 check_update 一致
