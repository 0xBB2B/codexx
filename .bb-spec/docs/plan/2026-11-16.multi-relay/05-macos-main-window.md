---
name: 05-macos-main-window
description: macOS 主窗口改成转发总览 + 中转配置卡片 + 底部状态行，加配置弹窗、删除确认，首次设置加名字栏
---
# macOS：主窗口新布局与配置弹窗

## 目标
macOS 主窗口、配置弹窗、删除确认、首次设置窗口按确认过的原型画出来，文字和状态都取自 `AppModel`。

## 业务规则（来源：PRD 02-main-window、03-multi-relay-config）
- 主窗口宽 620px，从上到下：标题栏「模型接入助手」、转发总览、「中转配置」卡片、底部状态行；没有「运行状态」卡片。3 个配置、转发开着时高约 649px（±12px）。
- 转发总览：开着时浅绿底 #e7f2f0 + 主色边框；左边 48px 应用图标，中间标题和一句说明，右边开关；第二行灰色「接入方式」、深色加粗方式名、灰色检测结果，最右「重新检测」按钮（检测中显示转圈「正在检测…」并不可点）。关着时白底灰边、没有第二行，窗口约 593px。
- 「中转配置」卡片：配置列表每行高 64px（名字加粗；下面状态和 Base URL，地址放不下用省略号；右边「重试」（仅失败时）「编辑」「删除」）；最多显示 3 行，更多在列表内滚动；下面「添加配置」按钮和说明一句。
- 底部状态行：版本号、绿点「后台运行中」、对勾「ChatGPT 已检测到」、对勾「中转模型 N 个（M 个配置）」，最右「检查更新」；标签灰色，取值深色加粗。
- 添加/编辑弹窗：独立窗口，宽 520px，头部 24px 应用图标 + 标题；名字、Base URL（等宽字，占位 `https://relay.example.com/v1`）、API Key（默认遮住，眼睛按钮显示/隐藏）三栏，都标「必填」，帮助文字分别是「1~16 个字，不能重名，不能含「·」。」「以 https:// 开头的完整地址。」「保存在系统钥匙串中。」；错误显示在帮助文字位置，弹窗高度不变；底部「取消」「保存」。
- 删除确认用现有弹窗样式。
- 首次设置窗口：名字栏放在最前、默认「中转」，其余布局不变。
- 所有提示浮在窗口上方，不改变窗口高度（沿用现有提示组件）。
- 样式取值以项目根 DESIGN.md 为准（「转发总览」「标签 + 取值」「配置列表」「表单弹窗」「应用图标」各节）。

## 涉及文件
- 修改 `macos/Sources/ModelBridge/UI/MainWindow.swift`、`Components.swift`、`SetupWindow.swift`、`Dialogs.swift`
- 新建 `macos/Sources/ModelBridge/UI/RelayFormWindow.swift`（配置弹窗）
- 修改对应测试（如界面快照/文案测试在 `AppModelTests.swift`、`SmokeTests.swift`）

## 成品定义
界面的布局、各状态的样子以确认过的原型为准：`.bb-spec/.cache/ui-preview/2026-11-16.multi-relay.html` 里 macOS 的「主窗口 · 配置的各种状态」「主窗口 · 转发的各种状态」「添加 / 编辑配置」「删除确认」「首次设置」五页。和 DESIGN.md 有出入时以 DESIGN.md 为准。

## 函数清单
### UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| `ForwardingOverview` | 转发总览 |
| `RelayConfigCard` / `RelayRow` | 中转配置卡片和每一行 |
| `StatusFooter` | 底部状态行 |
### UI/RelayFormWindow.swift
| 函数名 | 职责 |
|---|---|
| `RelayFormView` | 添加/编辑弹窗内容 |
| `RelayFormWindowController` | 打开、关闭弹窗窗口 |
### UI/Components.swift
| 函数名 | 职责 |
|---|---|
| `LabelValue` | 灰色标签 + 深色加粗取值的写法 |
| `AppIconView` | 按 DESIGN.md「应用图标」的几何参数（圆、两条箭头路径、按尺寸的线宽、主色底板）用矢量绘制窗口里的应用图标 |
### UI/SetupWindow.swift
| 函数名 | 职责 |
|---|---|
| 表单 | 加名字栏 |

## 协作关系
视图只读 `AppModel`（plan 04）的状态、调用它的动作。

## 验证方式
- 测试入口：`cd macos && swift test`；可观察的入口是 `AppModel` 状态驱动的视图层测试（项目现有 `SmokeTests` 写法），以及打包后手动打开主窗口对照原型。
- 预期结果：
  - [ ] 主窗口没有「运行状态」「中转连接」卡片；区块顺序正确。
  - [ ] 3 个配置、转发开着时窗口高 637~661px；关掉转发后变矮约 56px。
  - [ ] 弹窗宽 520px，三栏帮助文字正确，错误时高度不变。
  - [ ] 首次设置窗口名字栏默认「中转」。
  - [ ] 浏览器打开原型、和真实窗口逐项对比，布局、文字、状态一致（收尾时的界面对比）。
