---
name: macos-notice-position
description: macOS 主窗口、首次设置窗口提示条改左上角定宽，表单弹窗错误提示改插入式
---
# macOS 提示条位置
## 目标
macOS 三处窗口里的提示条不再盖住按钮、输入框、开关。
## 业务规则（来源：PRD 2026-11-26.notice-position / notice-position）
- 主窗口提示条浮在内容上面、不占布局，出现和消失时窗口高度不变；从左上角开始：距标题栏下沿 12px、距窗口左边 24px，宽 376px；多条从上往下排，间距 8px（C1）。
- 主窗口有一条或两条提示时（转发开着时含带「重试」按钮的错误提示；"没有找到 ChatGPT"时转发关着、没有接入方式行），提示条不与当时显示的转发开关、「只用中转」开关、「重新检测」、「编辑」、「删除」、「添加配置」、「检查更新」重叠（C2）。原因：提示正好盖住刚点的开关，用户 2026-11-26 选定"左上角收窄到 376px，右边让出开关"。
- 首次设置窗口提示条浮在内容上面、不占布局，窗口高度不变；左上角起始：距标题栏下沿 12px、距窗口左边 24px，宽 608px；多条从上往下排，间距 8px（C3）。
- 首次设置窗口只有一条提示时（含"没有保存"这条最长的错误提示），提示条不与三个输入框、显示 / 隐藏 API Key 按钮、「完成设置」重叠（C4）。
- 添加配置、编辑配置弹窗里不属于某个字段的错误提示插在正文最上面：标题下面 12px、第一个字段上面 20px，和字段同宽（左右各距弹窗边 24px），没有阴影；占布局，出现时下面的字段和按钮整体下移、弹窗变高，点 ✕ 消失后弹窗回到原高；不与任何控件重叠（C5）。原因：标题和「名字」之间只有 48px，浮着一定盖住输入框。
- 提示条本身的样子、文案、去留规则不变（4 秒自动消失、警告错误常驻、同标题同级别原位替换）（C6）。确认类弹窗（删除配置、卸载等）不改。
- macOS 和 Windows 位置、宽度、行为一致（C7）。
## 涉及文件
- macos/Sources/ModelBridge/UI/Components.swift（修改）
- macos/Sources/ModelBridge/UI/MainWindow.swift（修改）
- macos/Sources/ModelBridge/UI/SetupWindow.swift（修改）
- macos/Sources/ModelBridge/UI/RelayFormWindow.swift（修改）
- macos/Tests/ModelBridgeTests/ 下新增或修改的测试
## 函数清单
### macos/Sources/ModelBridge/UI/Components.swift
| 函数名 | 职责 |
|---|---|
| `AlertOverlay` | 提示条浮层容器：改为左上角对齐、按调用方给定宽度排列，不再左右各 24px 通栏 |
### macos/Sources/ModelBridge/UI/MainWindow.swift
| 函数名 | 职责 |
|---|---|
| `MainWindow.body` | 浮层宽 376px、左上角对齐 |
### macos/Sources/ModelBridge/UI/SetupWindow.swift
| 函数名 | 职责 |
|---|---|
| `SetupWindow.body` | 浮层宽 608px、左上角对齐 |
### macos/Sources/ModelBridge/UI/RelayFormWindow.swift
| 函数名 | 职责 |
|---|---|
| `RelayFormWindow.body` | relaySaveAlert 从 overlay 改为插进正文最上面（标题下 12px、字段上 20px、同字段宽、无阴影），弹窗随之变高 |
## 协作关系
MainWindow、SetupWindow 调 AlertOverlay；RelayFormWindow 直接放 AlertBar。AlertBar 样子不变。
## 验证方式
- 测试入口：用 ViewHarness 离屏渲染 MainWindow、SetupWindow、RelayFormWindow，读控件的 frame。
- 测试输入：主窗口"刚切换成功""一条错误 + 一条成功（错误带重试）""没有找到 ChatGPT"三种状态；首次设置"连接失败""写不进设置文件"两种；编辑配置弹窗保存失败（401）与写不进设置文件。
- 预期结果：
  - [ ] 主窗口提示条 x=24、距标题栏下沿 12、宽 376，两条间距 8；与列出的开关和按钮 frame 不相交；有无提示时窗口高度相同。
  - [ ] 首次设置窗口提示条 x=24、距标题栏下沿 12、宽 608；与三个输入框、显示按钮、「完成设置」不相交；窗口高度不变。
  - [ ] 编辑配置弹窗错误提示在标题下 12、「名字」上 20、宽与字段相同、无阴影；弹窗高度增加；点 ✕ 后高度复原；不与控件相交。
  - [ ] 提示条样式与改前一致，NoticeReplaceTests、现有布局测试不回归。
