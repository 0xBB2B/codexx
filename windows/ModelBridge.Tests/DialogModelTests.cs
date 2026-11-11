using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class DialogModelTests
{
    static readonly ConfirmRequest Request = new("退出模型接入助手？", "退出后转发会停止。", "退出");

    [TestMethod]
    public void Confirm_Width_Is520()
    {
        Assert.AreEqual(520, DialogModel.ForConfirm(Request).Width);
    }

    [TestMethod]
    public void Confirm_TitleAndMessage_MatchRequest()
    {
        var dialog = DialogModel.ForConfirm(Request);

        Assert.AreEqual(Request.Title, dialog.Title);
        Assert.AreEqual(Request.Message, dialog.Message);
    }

    [TestMethod]
    public void Confirm_Buttons_CancelThenConfirm()
    {
        var dialog = DialogModel.ForConfirm(Request);

        CollectionAssert.AreEqual(new[] { "取消", "退出" }, dialog.Buttons.Select(b => b.Text).ToArray());
    }

    [TestMethod]
    public void Confirm_CustomCancelText_UsedForCancelButton()
    {
        var dialog = DialogModel.ForConfirm(Request with { CancelText = "先不退出" });

        Assert.AreEqual("先不退出", dialog.Buttons[0].Text);
        Assert.AreEqual("退出", dialog.Buttons[1].Text);
    }

    [TestMethod]
    public void Confirm_CancelButton_IsSecondaryAndDefault()
    {
        var cancel = DialogModel.ForConfirm(Request).Buttons[0];

        Assert.AreEqual(DialogButtonRole.Secondary, cancel.Role);
        Assert.IsTrue(cancel.IsDefault);
    }

    [TestMethod]
    public void Confirm_ConfirmButton_IsPrimaryAndNotDefault()
    {
        var confirm = DialogModel.ForConfirm(Request).Buttons[1];

        Assert.AreEqual(DialogButtonRole.Primary, confirm.Role);
        Assert.IsFalse(confirm.IsDefault);
    }

    [TestMethod]
    public void Confirm_OnlyOneDefaultButton()
    {
        Assert.AreEqual(1, DialogModel.ForConfirm(Request).Buttons.Count(b => b.IsDefault));
    }

    [TestMethod]
    public void Confirm_Escape_EqualsCancelButton()
    {
        var dialog = DialogModel.ForConfirm(Request);

        Assert.AreEqual(0, dialog.EscapeButtonIndex);
        Assert.AreEqual("取消", dialog.Buttons[dialog.EscapeButtonIndex].Text);
    }

    [TestMethod]
    public void UninstallPause_Buttons_CancelOpenFolderRecheck()
    {
        var pause = new UninstallPause(["model_provider"], "/home/example/.codex/config.toml");

        var dialog = DialogModel.ForUninstallPause(pause);

        CollectionAssert.AreEqual(
            new[] { "取消卸载", "打开所在文件夹", "重新检查" },
            dialog.Buttons.Select(b => b.Text).ToArray());
    }

    [TestMethod]
    public void UninstallPause_Width_Is520()
    {
        var pause = new UninstallPause(["model_provider"], "/home/example/.codex/config.toml");

        Assert.AreEqual(520, DialogModel.ForUninstallPause(pause).Width);
    }

    [TestMethod]
    public void UninstallPause_Alert_IsDangerWithTitle()
    {
        var pause = new UninstallPause(["model_provider"], "/home/example/.codex/config.toml");

        var alert = DialogModel.ForUninstallPause(pause).Alert;

        Assert.IsNotNull(alert);
        Assert.AreEqual(DialogAlertSeverity.Danger, alert.Severity);
        Assert.AreEqual("没能自动还原 Codex 配置", alert.Title);
    }

    [TestMethod]
    public void UninstallPause_AlertMessage_ListsKeysAndConfigPath()
    {
        var pause = new UninstallPause(["model_provider", "base_url"], "/home/example/.codex/config.toml");

        var alert = DialogModel.ForUninstallPause(pause).Alert;

        Assert.IsNotNull(alert);
        StringAssert.Contains(alert.Message, "model_provider");
        StringAssert.Contains(alert.Message, "base_url");
        StringAssert.Contains(alert.Message, "/home/example/.codex/config.toml");
    }

    [TestMethod]
    public void UninstallPause_Buttons_RolesAndDefaultFocus()
    {
        var pause = new UninstallPause(["model_provider"], "/home/example/.codex/config.toml");

        var buttons = DialogModel.ForUninstallPause(pause).Buttons;

        CollectionAssert.AreEqual(
            new[] { DialogButtonRole.Secondary, DialogButtonRole.Secondary, DialogButtonRole.Primary },
            buttons.Select(b => b.Role).ToArray());
        CollectionAssert.AreEqual(new[] { true, false, false }, buttons.Select(b => b.IsDefault).ToArray());
    }

    [TestMethod]
    public void Confirm_HasNoAlert()
    {
        Assert.IsNull(DialogModel.ForConfirm(Request).Alert);
    }

    [TestMethod]
    public void Info_SingleKnowItButton_PrimaryAndDefault()
    {
        var dialog = DialogModel.ForInfo("以下内容没能自动清理", "凭据、设置文件");

        Assert.AreEqual("以下内容没能自动清理", dialog.Title);
        Assert.AreEqual("凭据、设置文件", dialog.Message);
        Assert.AreEqual(1, dialog.Buttons.Count);
        Assert.AreEqual("知道了", dialog.Buttons[0].Text);
        Assert.AreEqual(DialogButtonRole.Primary, dialog.Buttons[0].Role);
        Assert.IsTrue(dialog.Buttons[0].IsDefault);
    }

    [TestMethod]
    public void Info_Escape_EqualsKnowItButton()
    {
        var dialog = DialogModel.ForInfo("以下内容没能自动清理", "凭据");

        Assert.AreEqual("知道了", dialog.Buttons[dialog.EscapeButtonIndex].Text);
    }

    [TestMethod]
    public void UninstallPause_Escape_EqualsCancelUninstall()
    {
        var pause = new UninstallPause(["model_provider"], "/home/example/.codex/config.toml");

        var dialog = DialogModel.ForUninstallPause(pause);

        Assert.AreEqual("取消卸载", dialog.Buttons[dialog.EscapeButtonIndex].Text);
    }
}
