using System.Collections.Generic;
using System.IO;
using System.Linq;
using Microsoft.Win32;

namespace ModelBridge.Services;

public sealed class LoginItem(
    string installDir,
    string runKeyPath = LoginItem.DefaultRunKeyPath,
    string valueName = LoginItem.DefaultValueName)
{
    public const string DefaultRunKeyPath = @"Software\Microsoft\Windows\CurrentVersion\Run";
    public const string DefaultValueName = "ModelBridge";
    const string LoginArg = "--login";

    public static bool WasLaunchedAtLogin(IEnumerable<string> args) => args.Contains(LoginArg);

    public bool IsEnabled()
    {
        using var key = Registry.CurrentUser.OpenSubKey(runKeyPath);
        return key?.GetValue(valueName) != null;
    }

    public void Enable()
    {
        using var key = Registry.CurrentUser.CreateSubKey(runKeyPath);
        key.SetValue(valueName, $"\"{Path.Combine(installDir, "ModelBridge.exe")}\" {LoginArg}");
    }

    public void Disable()
    {
        using var key = Registry.CurrentUser.OpenSubKey(runKeyPath, true);
        key?.DeleteValue(valueName, false);
    }
}
