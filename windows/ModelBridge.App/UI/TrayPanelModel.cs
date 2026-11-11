using System.Collections.Generic;

namespace ModelBridge.UI;

public enum TrayMenuCommand { ToggleForwarding, OpenConfig, ToggleLoginStart, CheckUpdate, Uninstall, Quit }

public sealed record TrayMenuItem(
    TrayMenuCommand Command,
    string Label,
    string? Detail = null,
    bool IsCheckable = false,
    bool IsChecked = false,
    bool IsEnabled = true);

public sealed record TrayPanelModel(
    int Width,
    string Title,
    string Subtitle,
    IReadOnlyList<IReadOnlyList<TrayMenuItem>> Groups)
{
    public static TrayPanelModel From(AppModel model)
    {
        var idle = !model.IsBusy;
        var update = model.UpdateVersion is { } version
            ? new TrayMenuItem(TrayMenuCommand.CheckUpdate, $"发现新版本 {version}", "打开下载页", IsEnabled: idle)
            : new TrayMenuItem(TrayMenuCommand.CheckUpdate, "检查更新", IsEnabled: idle);

        return new TrayPanelModel(
            286,
            "模型接入助手",
            model.TrayStatusText,
            [
                [
                    new(TrayMenuCommand.ToggleForwarding, "转发到中台", IsCheckable: true, IsChecked: model.ForwardingEnabled, IsEnabled: model.ToggleEnabled),
                    new(TrayMenuCommand.OpenConfig, "打开配置…"),
                ],
                [
                    new(TrayMenuCommand.ToggleLoginStart, "登录时启动", IsCheckable: true, IsChecked: model.LoginStartEnabled, IsEnabled: idle),
                    update,
                ],
                [
                    new(TrayMenuCommand.Uninstall, "卸载模型接入助手…", IsEnabled: idle),
                    new(TrayMenuCommand.Quit, "退出模型接入助手", IsEnabled: idle),
                ],
            ]);
    }
}
