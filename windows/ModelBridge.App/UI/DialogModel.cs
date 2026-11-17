using System.Collections.Generic;

namespace ModelBridge.UI;

public enum DialogButtonRole { Primary, Secondary }

public sealed record DialogButton(string Text, DialogButtonRole Role, bool IsDefault = false, bool IsEnabled = true, bool IsBusy = false);

public enum DialogAlertSeverity { Danger }

public sealed record DialogAlert(string Title, string Message, DialogAlertSeverity Severity);

public sealed record DialogModel(
    int Width,
    string Title,
    string Message,
    IReadOnlyList<DialogButton> Buttons,
    int EscapeButtonIndex,
    DialogAlert? Alert = null)
{
    public static DialogModel ForConfirm(ConfirmRequest request) => new(
        520,
        request.Title,
        request.Message,
        [
            new(request.CancelText, DialogButtonRole.Secondary, IsDefault: true),
            new(request.ConfirmText, DialogButtonRole.Primary),
        ],
        0);

    public static string DeleteRelayMessage(string name, int modelCount) =>
        $"删除后：\n· Codex 的模型列表里不再有「{name}」的 {modelCount} 个模型\n· 它的 API Key 会从系统凭据管理器里删除\n\n重启 ChatGPT 后生效。";

    public static DialogModel ForDeleteRelay(string name, int modelCount, bool isDeleting) => new(
        520,
        $"删除配置「{name}」？",
        DeleteRelayMessage(name, modelCount),
        [
            new("取消", DialogButtonRole.Secondary, IsDefault: true, IsEnabled: !isDeleting),
            new(isDeleting ? "正在删除…" : "删除", DialogButtonRole.Primary, IsEnabled: !isDeleting, IsBusy: isDeleting),
        ],
        0);

    public static DialogModel ForUninstallPause(UninstallPause pause) => new(
        520,
        "卸载暂停：有设置被手动改过",
        "",
        [
            new("取消卸载", DialogButtonRole.Secondary, IsDefault: true),
            new("打开所在文件夹", DialogButtonRole.Secondary),
            new("重新检查", DialogButtonRole.Primary),
        ],
        0,
        new DialogAlert(
            "没能自动还原 Codex 配置",
            $"安装后，有人手动改过本助手写入的设置项：{string.Join("、", pause.Keys)}。为了不覆盖这些修改，助手没有改动它，也还没有删除任何东西。\n\n请检查 {pause.ConfigPath}，处理后重新检查。",
            DialogAlertSeverity.Danger));

    public static DialogModel ForRestoreHelp(string code, IReadOnlyList<string> keys, string configPath)
    {
        var list = string.Join("、", keys);
        var (title, body) = code == "config_invalid"
            ? ("Codex 配置文件格式有误",
               "配置文件的格式不对，助手没有改动它。" + (keys.Count > 0 ? $"\n\n这些设置项的格式不对：\n{list}" : ""))
            : ("没能自动还原 Codex 配置",
               "为了不覆盖你的修改，助手没有还原本助手写入的设置。" + (keys.Count > 0 ? $"\n\n这些设置项被手动改过：\n{list}" : ""));
        var tail = code == "config_invalid" ? "请手动修正后点「重新检查」。" : "请手动处理后点「重新检查」。";
        return new(
            520,
            title,
            $"{body}\n\n配置文件：{configPath}\n\n{tail}",
            [
                new("关闭", DialogButtonRole.Secondary, IsDefault: true),
                new("打开所在文件夹", DialogButtonRole.Secondary),
                new("重新检查", DialogButtonRole.Primary),
            ],
            0);
    }

    public static DialogModel ForUninstallFinished(UninstallOutcome outcome)
    {
        var message = "已卸载模型接入助手。";
        if (outcome.Leftovers.Count > 0)
            message += "\n\n以下内容没能自动清理，请手动处理：\n· " + string.Join("\n· ", outcome.Leftovers);
        if (outcome.ChatGptRunning)
            message += "\n\n请重启 ChatGPT，Codex 的模型列表才会恢复成只有官方模型。";
        return new(
            520,
            "已卸载模型接入助手",
            message,
            [new("好", DialogButtonRole.Primary, IsDefault: true)],
            0);
    }

    public static DialogModel ForInfo(string title, string message) => new(
        520,
        title,
        message,
        [new("知道了", DialogButtonRole.Primary, IsDefault: true)],
        0);
}
