---
name: 12-windows-installer
description: Inno Setup 安装脚本：按用户安装不要管理员、最低 Windows 10、卸载前先执行还原且可中止
---
# Windows 安装包

## 目标
生成可双击安装的 `model-bridge-<版本>-windows-setup.exe`，从"应用和功能"卸载时先还原 Codex 配置，遇到手改项就中止卸载。

## 业务规则（来源：PRD 05-uninstall C7、07-release C2～C6）
- 按当前用户安装到 `%LOCALAPPDATA%\Programs\ModelBridge`，全程不要求管理员权限。
- 低于 Windows 10 → 提示"需要 Windows 10 或更高版本"，不安装；只支持 64 位。
- 安装完成后启动 ModelBridge.exe（显示首次设置窗口）。
- 卸载开始前执行 `ModelBridge.exe --uninstall`：退出码 0 → 继续删除程序文件；其他 → 中止卸载（外壳已向用户说明原因）。
- 卸载不删 ChatGPT、聊天数据、`auth.json`、Codex 配置里接入项以外的内容、`config.toml.before-model-bridge`。
- 安装包无签名；脚本、文件名、说明里不出现真实地址、Key、中台品牌。

## 涉及文件
- windows/installer/model-bridge.iss（新建）

## 成品定义

`windows/installer/model-bridge.iss`（CI 传入 `/DAppVersion=<版本> /DSourceDir=<发布目录>`）：
```iss
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\windows-amd64"
#endif

[Setup]
AppId={{5C1F7A2E-7B3D-4E41-9C8A-2D7E6B1F0A11}
AppName=模型接入助手
AppVersion={#AppVersion}
AppPublisher=ModelBridge contributors
DefaultDirName={localappdata}\Programs\ModelBridge
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputBaseFilename=model-bridge-{#AppVersion}-windows-setup
OutputDir=..\..\dist
UninstallDisplayName=模型接入助手
UninstallDisplayIcon={app}\ModelBridge.exe
WizardStyle=modern
Compression=lzma2
SolidCompression=yes

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Messages]
WindowsVersionNotSupported=需要 Windows 10 或更高版本。

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: recursesubdirs ignoreversion

[Run]
Filename: "{app}\ModelBridge.exe"; Flags: nowait postinstall skipifsilent

[Code]
function InitializeUninstall(): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  if Exec(ExpandConstant('{app}\ModelBridge.exe'), '--uninstall', '', SW_SHOW, ewWaitUntilTerminated, ResultCode) then
    Result := (ResultCode = 0);
end;
```
说明：`ChineseSimplified.isl` 若构建机上的 Inno Setup 未自带，落盘时改为 `compiler:Default.isl` 并在 PROGRESS.md 记录；`[Messages]` 的自定义文字保留。

## 函数清单
### windows/installer/model-bridge.iss
| 函数名 | 职责 |
|---|---|
| `InitializeUninstall` | 卸载前运行 `ModelBridge.exe --uninstall`，退出码为 0 才继续 |

## 协作关系
CI（13）先发布 Windows 外壳和核心程序到 `dist\windows-amd64`，再用构建机预装的 Inno Setup 编译此脚本。卸载时调用外壳（11）的 `--uninstall` 流程。

## 验证方式
- 测试入口：CI 中 `iscc windows\installer\model-bridge.iss /DAppVersion=0.0.1 /DSourceDir=<目录>`；在用户的 Windows 电脑上实机安装、卸载
- 测试输入：普通用户账户；一份带无关设置的 Codex 配置
- 预期结果：
- [ ] 产出文件名为 `model-bridge-0.0.1-windows-setup.exe`
- [ ] 普通用户双击安装全程没有管理员权限请求，装到 `%LOCALAPPDATA%\Programs\ModelBridge`，结束后打开首次设置窗口
- [ ] "应用和功能"里能看到"模型接入助手"；卸载时先出现本工具的卸载说明；手改接入项后卸载 → 卸载被中止，程序文件还在
- [ ] 正常卸载后 Codex 配置还原、程序目录删除、ChatGPT 与 `config.toml.before-model-bridge` 都在
