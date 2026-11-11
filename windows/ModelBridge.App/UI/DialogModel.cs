using System.Collections.Generic;

namespace ModelBridge.UI;

public enum DialogButtonRole { Primary, Secondary }

public sealed record DialogButton(string Text, DialogButtonRole Role, bool IsDefault = false);

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

    public static DialogModel ForInfo(string title, string message) => new(
        520,
        title,
        message,
        [new("知道了", DialogButtonRole.Primary, IsDefault: true)],
        0);
}
