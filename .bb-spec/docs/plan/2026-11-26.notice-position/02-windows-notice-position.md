---
name: windows-notice-position
description: Windows 主窗口、首次设置窗口提示条改左上角定宽，表单弹窗错误提示改插入式
---
# Windows 提示条位置
## 目标
Windows 三处窗口里的提示条不再盖住按钮、输入框、开关。
## 业务规则（来源：PRD 2026-11-26.notice-position / notice-position）
- 主窗口提示条浮在内容上面、不占布局，出现和消失时窗口高度不变；从左上角开始：距标题栏下沿 12px、距窗口左边 24px，宽 376px；多条从上往下排，间距 8px（C1）。
- 主窗口有一条或两条提示时（转发开着时含带「重试」按钮的错误提示；"没有找到 ChatGPT"时转发关着、没有接入方式行），提示条不与当时显示的转发开关、「只用中转」开关、「重新检测」、「编辑」、「删除」、「添加配置」、「检查更新」重叠（C2）。原因：提示正好盖住刚点的开关，用户 2026-11-26 选定"左上角收窄到 376px，右边让出开关"。
- 首次设置窗口提示条浮在内容上面、不占布局，窗口高度不变；左上角起始：距标题栏下沿 12px、距窗口左边 24px，宽 608px；多条从上往下排，间距 8px（C3）。
- 首次设置窗口只有一条提示时（含"没有保存"这条最长的错误提示），提示条不与三个输入框、显示 / 隐藏 API Key 按钮、「完成设置」重叠（C4）。
- 添加配置、编辑配置弹窗里不属于某个字段的错误提示插在正文最上面：标题下面 12px、第一个字段上面 20px，和字段同宽（左右各距弹窗边 24px），没有阴影；占布局，出现时下面的字段和按钮整体下移、弹窗变高，点 ✕ 消失后弹窗回到原高；不与任何控件重叠（C5）。原因：标题和「名字」之间只有 48px，浮着一定盖住输入框。
- 提示条本身的样子、文案、去留规则不变（4 秒自动消失、警告错误常驻、同标题同级别原位替换）（C6）。确认类弹窗（删除配置、卸载等）不改。
- macOS 和 Windows 位置、宽度、行为一致（C7）。
## 涉及文件
- windows/ModelBridge.App/UI/MainWindow.xaml、MainWindow.xaml.cs（修改）
- windows/ModelBridge.App/UI/SetupWindow.xaml、SetupWindow.xaml.cs（修改）
- windows/ModelBridge.App/UI/RelayFormWindow.xaml、RelayFormWindow.xaml.cs（修改）
- windows/ModelBridge.Tests/ 下新增或修改的测试
## 函数清单
### windows/ModelBridge.App/UI/MainWindow.xaml(.cs)
| 函数名 | 职责 |
|---|---|
| `AlertHost`（XAML） | 左上角、距标题栏下沿 12px、距左 24px |
| `OverlaySizeChanged` | 宽度固定 376px，不再按窗口宽减 48 |
### windows/ModelBridge.App/UI/SetupWindow.xaml(.cs)
| 函数名 | 职责 |
|---|---|
| `AlertHost`（XAML） | 左上角、距标题栏下沿 12px、距左 24px |
| `OverlaySizeChanged` | 宽度固定 608px |
### windows/ModelBridge.App/UI/RelayFormWindow.xaml(.cs)
| 函数名 | 职责 |
|---|---|
| `AlertHost`（XAML） | 从 Overlay Canvas 移进正文网格：标题行与表单之间，上 12px、下 20px、左右 24px，占布局，无阴影；窗口按内容变高 |
| `OverlaySizeChanged` / `Overlay` | 不再需要时删除 |
| `Refresh`（调用 AlertView.Fill 处） | 无提示时 AlertHost 不占空间 |
## 协作关系
各窗口经 AlertView.Fill 填充 AlertHost；AlertView 样式不变。
## 验证方式
- 测试入口：MSTest 窗口测试（沿用 MainWindowLayoutTests、RelayFormWindowTests、SetupWindow*Tests 与 UiTestHost 写法），读控件相对窗口的位置和尺寸。本机只能编译，测试在 CI 跑。
- 测试输入：同 macOS：主窗口三种状态；首次设置两种；编辑配置弹窗保存失败与写不进设置文件。
- 预期结果：
  - [ ] 主窗口提示条左上角距标题栏下沿 12、左 24、宽 376，两条间距 8；与开关和按钮不相交；有无提示窗口高度相同。
  - [ ] 首次设置窗口提示条距标题栏下沿 12、左 24、宽 608；与输入框和「完成设置」不相交；窗口高度不变。
  - [ ] 编辑配置弹窗错误提示在标题下 12、第一个字段上 20、与字段同宽；窗口变高；关闭后复原；不与控件相交。
  - [ ] 现有窗口测试不回归。
