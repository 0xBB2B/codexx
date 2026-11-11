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
MinVersion=10.0.17763
AppMutex=Local\ModelBridge.SingleInstance
CloseApplications=force
OutputBaseFilename=model-bridge-{#AppVersion}-windows-setup
OutputDir=..\..\dist
UninstallDisplayName=模型接入助手
UninstallDisplayIcon={app}\ModelBridge.exe
WizardStyle=modern
Compression=lzma2
SolidCompression=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

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
  if Exec(ExpandConstant('{app}\ModelBridge.exe'), '--uninstall', '', SW_SHOW, ewWaitUntilTerminated, ResultCode) then
    Result := (ResultCode = 0)
  else
    Result := MsgBox('找不到模型接入助手程序，无法自动还原 Codex 配置。仍要删除程序文件吗？', mbConfirmation, MB_YESNO) = IDYES;
end;
