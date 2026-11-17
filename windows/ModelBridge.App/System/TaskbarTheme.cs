using System;
using Microsoft.Win32;

namespace ModelBridge.Services;

public static class TaskbarTheme
{
    public static bool IsLight(Func<int?> readValue) => readValue() == 1;

    public static bool IsLight() => IsLight(ReadRegistry);

    public static string IconFor(bool isLight) => isLight ? "TrayIconLight.ico" : "TrayIconDark.ico";

    public static int IconSizeFor(double dpi) => dpi > 96 ? 32 : 16;

    static int? ReadRegistry()
    {
        using var key = Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");
        return key?.GetValue("SystemUsesLightTheme") as int?;
    }
}
