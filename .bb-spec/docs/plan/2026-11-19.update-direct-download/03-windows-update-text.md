---
name: 03-windows-update-text
description: Windows 端按核心给的 installer 字段写更新项副标题和状态区可访问名称
---
# Windows 端更新提示文字

## 目标
Windows 托盘面板的更新项和主窗口状态区按钮，按核心返回的是安装包还是发布页显示对应文字；点击仍用默认浏览器打开核心给的地址。

## 业务规则（来源：PRD 01-direct-download）
- 核心的 check_update 返回和 status.update 带 `installer` 布尔字段：true 表示 `url` 是本平台安装包下载地址，false 表示是发布页。字段缺失按 false。
- installer 为 true：托盘面板更新项标题「发现新版本 <版本>」、副标题「下载安装包」；状态区按钮文字「新版本 <版本>」、可访问名称「发现新版本 <版本>，下载安装包」。
- installer 为 false：副标题「打开下载页」，可访问名称「发现新版本 <版本>，打开下载页」，按钮文字仍是「新版本 <版本>」。
- 点更新项或按钮：用默认浏览器打开 `url`（现有 ExternalOpen.Url，只放行 https，不变），托盘面板点完关闭（现有行为）。助手自己不下载。
- 自动检查（status）和手动检查（check_update）得到的提示一致；没有新版本时清空。
- 界面布局、字号、颜色不变，只换文字。

## 涉及文件
- windows/ModelBridge.App/UI/AppModel.cs（修改）
- windows/ModelBridge.App/UI/TrayPanelModel.cs（修改）
- windows/ModelBridge.App/UI/MainWindow.xaml.cs（修改）
- windows/ModelBridge.FakeCore/Program.cs（如假核心需返回 installer 字段时修改）
- windows/ModelBridge.Tests/AppModelTests.cs、TrayPanelModelTests.cs、MainWindowLayoutTests.cs（修改）

## 函数清单
### windows/ModelBridge.App/UI/AppModel.cs
| 函数名 | 职责 |
|---|---|
| `UpdateIsInstaller`（新增属性） | 当前新版本地址是不是安装包，变化时通知界面 |
| `CheckUpdateAsync` | 读 check_update 返回的 installer（缺失为 false）；没有新版本时清为 false |
| status 处理处（约 :1288-1291） | 从 status.update 读 installer |

### windows/ModelBridge.App/UI/TrayPanelModel.cs
| 函数名 | 职责 |
|---|---|
| 更新项构造处（约 :24-26） | 副标题按 UpdateIsInstaller 取「下载安装包」或「打开下载页」 |

### windows/ModelBridge.App/UI/MainWindow.xaml.cs
| 函数名 | 职责 |
|---|---|
| 更新按钮文字与可访问名称设置处（约 :77-78） | 可访问名称按 UpdateIsInstaller 取两种文字 |

## 协作关系
核心 status / check_update → AppModel（UpdateVersion、UpdateUrl、UpdateIsInstaller）→ TrayPanelModel、MainWindow 读取并显示；点击由 TrayMenu、MainWindow 调 ExternalOpen.Url 打开 UpdateUrl。

## 验证方式
- 测试入口：AppModel 的检查更新与 status 处理（现有假核心夹具）、TrayPanelModel 更新项、MainWindow 更新按钮可访问名称（现有 MainWindowLayoutTests 写法）。
- 测试输入：check_update 返回 `{"latest_version":"0.1.2","url":"https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-windows-setup.exe","installer":true,"is_newer":true}`；installer false、url 为发布页；缺 installer 字段；status.update 同样三种。
- 预期结果：
- [ ] installer true → 托盘更新项标题「发现新版本 0.1.2」、副标题「下载安装包」；按钮文字「新版本 0.1.2」、可访问名称「发现新版本 0.1.2，下载安装包」；UpdateUrl 等于 setup.exe 地址
- [ ] installer false 或缺失 → 副标题「打开下载页」，可访问名称「发现新版本 0.1.2，打开下载页」，按钮文字「新版本 0.1.2」
- [ ] 之后检查到已是最新 → UpdateIsInstaller 回到 false
- [ ] status.update 带 installer true 时结果与 check_update 一致
