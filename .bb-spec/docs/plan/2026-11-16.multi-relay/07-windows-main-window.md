---
name: 07-windows-main-window
description: Windows 主窗口改成转发总览 + 中转配置卡片 + 底部状态行，加配置弹窗、删除确认，首次设置加名字栏
---
# Windows：主窗口新布局与配置弹窗

## 目标
Windows 主窗口、配置弹窗、删除确认、首次设置窗口按确认过的原型画出来，文字和状态都取自 `AppModel`。

## 业务规则（来源：PRD 02-main-window、03-multi-relay-config）
- 主窗口宽 620px，从上到下：标题栏（高 32px，标题「模型接入助手」靠左，前面 16px 应用图标，右边系统三个按钮，窗口圆角 8px）、转发总览、「中转配置」卡片、底部状态行；没有「运行状态」卡片。3 个配置、转发开着时高约 633px（±12px）。
- 转发总览：开着时浅绿底 #e7f2f0 + 主色边框；左边 48px 应用图标，中间标题和一句说明，右边开关；第二行灰色「接入方式」、深色加粗方式名、灰色检测结果，最右「重新检测」按钮（检测中显示「正在检测…」并不可点）。关着时白底灰边、没有第二行。
- 「中转配置」卡片：每行高 64px（名字加粗；下面状态和 Base URL，地址放不下用省略号；右边「重试」（仅失败时）「编辑」「删除」）；最多显示 3 行，更多在列表内滚动；下面「添加配置」和说明一句。
- 底部状态行：版本号、绿点「后台运行中」、对勾「ChatGPT 已检测到」、对勾「中转模型 N 个（M 个配置）」，最右「检查更新」；标签灰色，取值深色加粗。
- 添加/编辑弹窗：宽 520px，头部 24px 应用图标 + 标题；名字、Base URL（等宽字）、API Key（默认遮住，可显示）三栏都标「必填」；帮助文字「1~16 个字，不能重名，不能含「·」。」「以 https:// 开头的完整地址。」「保存在系统凭据管理器中。」；错误显示在帮助文字位置，弹窗高度不变；底部「取消」「保存」。
- 提示浮在窗口上方，不进入 SizeToContent，不改变窗口高度（沿用现有 Canvas 浮层写法）。
- 样式取值以 DESIGN.md 为准。

## 涉及文件
- 修改 `windows/ModelBridge.App/UI/MainWindow.xaml`(.cs)、`SetupWindow.xaml`(.cs)、`ConnectionForm.xaml`(.cs)、`Theme.xaml`、`DialogWindow.xaml`(.cs)
- 新建 `windows/ModelBridge.App/UI/RelayFormWindow.xaml`(.cs)
- 修改对应测试（如 `SmokeTests.cs`）

## 成品定义
布局和各状态以确认过的原型 `.bb-spec/.cache/ui-preview/2026-11-16.multi-relay.html` 里 Windows 的五页为准；和 DESIGN.md 有出入时以 DESIGN.md 为准。窗口里的应用图标按 DESIGN.md「应用图标」的几何参数用矢量绘制（WPF Path）。

## 函数清单
### UI/MainWindow.xaml(.cs)
| 函数名 | 职责 |
|---|---|
| 转发总览区域 | 绑定 `AppModel` 的标题、说明、方式、检测结果 |
| 配置列表区域 | `ItemsControl` 绑定 `RelayRows`，最多 3 行高度 |
| 底部状态行 | 绑定状态文字 |
### UI/RelayFormWindow.xaml(.cs)
| 函数名 | 职责 |
|---|---|
| `RelayFormWindow` | 添加/编辑弹窗 |
### UI/Theme.xaml
| 函数名 | 职责 |
|---|---|
| `AppIconGeometry` 等资源 | 应用图标几何、标签 + 取值样式 |

## 协作关系
视图只读 `AppModel`（plan 06）的状态、调用它的动作。

## 验证方式
- 测试入口：本机 `dotnet build windows/ModelBridge.sln -p:EnableWindowsTargeting=true` 编译通过；CI 上 `dotnet test`（推临时分支前征得用户同意）；界面需在 Windows 真机对照原型（记入待用户验证）。
- 预期结果：
  - [ ] 编译通过、CI 测试通过。
  - [ ] XAML 里没有「运行状态」「中转连接」卡片，没有「中台」。
  - [ ] 弹窗宽 520px，帮助文字与规定一致。
