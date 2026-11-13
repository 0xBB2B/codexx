using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Linq;
using System.Runtime.CompilerServices;
using System.Security;
using System.Text.Json;
using System.Threading.Tasks;
using ModelBridge.Services;

namespace ModelBridge.UI;

public enum AppPage { Setup, Success, Main }

public enum AppSeverity { Info, Success, Warning, Danger }

public enum BannerAction { Retry, ShowDetail, Redetect }

public sealed record AppNotice(string Title, string Detail = "", AppSeverity Severity = AppSeverity.Danger)
{
    public Guid Id { get; } = Guid.NewGuid();
}

public sealed record AppBanner(string Title, string Detail, AppSeverity Severity, IReadOnlyList<BannerAction> Actions)
{
    public string Code { get; init; } = "";
    public IReadOnlyList<string> Keys { get; init; } = [];
    public string ConfigPath { get; init; } = "";
}

public sealed record ConfirmRequest(string Title, string Message, string ConfirmText, string CancelText = "取消");

public sealed record CoreErrorInfo(string Code, IReadOnlyList<string> Keys);

public sealed record UninstallPause(IReadOnlyList<string> Keys, string ConfigPath);

public sealed record UninstallOutcome(IReadOnlyList<string> Leftovers, bool ChatGptRunning);

public interface ICoreClient
{
    event Action<JsonElement>? StatusReceived;
    event Action<string>? Failed;
    Task StartAsync(CoreInitParams init);
    Task StopAsync();
    Task<JsonElement> CallAsync(string method, IReadOnlyDictionary<string, object?>? parameters = null);
}

public interface ICredentialVault
{
    void Save(string key);
    string? Read();
    void Delete();
}

public interface IShellSettingsStore
{
    string? ReadBaseUrl();
    void SaveBaseUrl(string baseUrl);
    void DeleteAll();
}

public interface ILoginStart
{
    bool IsEnabled();
    void Enable();
    void Disable();
}

public interface IChatGptControl
{
    string? Locate();
    bool IsRunning();
}

public interface IConfirmer
{
    Task<bool> ConfirmAsync(ConfirmRequest request);
}

public sealed class AppModel : INotifyPropertyChanged
{
    public const string ActivationFailedMessage = "模型接入助手已在运行，但无法切换到它，请从任务栏通知区域打开";

    const string Required = "必填";
    const string NoSave = "没有保存";
    const string CredentialWriteFailed = "无法写入系统凭据管理器";
    const string SettingsWriteFailed = "无法保存设置";
    const string CoreStartFailed = "后台服务没能启动，请退出后重新打开";
    const string CoreStopped = "后台服务已停止，请退出后重新打开";
    const string RestoreBlockedTitle = "没能自动还原 Codex 配置";
    const string RefreshFailed = "没能刷新";
    const string SwitchFailed = "没能切换转发";
    const string DetectFailedTitle = "没能检测 ChatGPT 登录状态";
    const string NoChangeTitle = "接入方式没有变化";
    const string RecheckFailedTitle = "没能重新检测";
    const string AccessSwitchFailed = "没能切换接入方式";
    const string KeyUnreadable = "无法读取已保存的 API Key，请重新输入";
    const string RestartHint = "重启 ChatGPT 后生效。";
    const string SavedNoRestart = "已保存，不需要重启 ChatGPT";
    const string QuitBase = "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。";
    const string QuitRunningTail = "ChatGPT 正在运行，请在退出后重启 ChatGPT。";

    readonly ICoreClient core;
    readonly ICredentialVault vault;
    readonly IShellSettingsStore settings;
    readonly ILoginStart login;
    readonly IChatGptControl chatGpt;
    readonly IConfirmer confirmer;

    string baseUrlInput = "";
    string apiKeyInput = "";
    string? baseUrlError;
    string? apiKeyError;
    bool isConnecting;
    bool busy;
    AppPage page;
    IReadOnlyList<AppNotice> notices = [];
    readonly object noticeLock = new();
    readonly HashSet<string> dismissedBanners = [];
    AppNotice? taskError;
    string? progressText;
    string successTitle = "";
    string successText = "";
    bool forwardingEnabled;
    bool chatGptFound = true;
    bool loginStartEnabled;
    bool coreRunning;
    string? accessMode;
    string? accessDetect;
    bool isDetectingAccess;
    bool startupAligned;
    int modelCount;
    string? fetchedAt;
    string version = "";
    string configPath = "";
    string? updateVersion;
    string? updateUrl;
    CoreErrorInfo? lastError;
    UninstallPause? uninstallPause;
    UninstallOutcome? uninstalled;
    IReadOnlyList<AppBanner> banners = [];
    IReadOnlyList<AppBanner> statusBanners = [];

    public AppModel(
        ICoreClient core,
        ICredentialVault vault,
        IShellSettingsStore settings,
        ILoginStart login,
        IChatGptControl chatGpt,
        IConfirmer confirmer)
    {
        this.core = core;
        this.vault = vault;
        this.settings = settings;
        this.login = login;
        this.chatGpt = chatGpt;
        this.confirmer = confirmer;
        core.StatusReceived += ApplyStatus;
        core.StatusReceived += OnStatusReceived;
        core.Failed += OnCoreFailed;
    }

    public event PropertyChangedEventHandler? PropertyChanged;
    public event Action? ShowMainWindowRequested;
    public event Action? ExitRequested;

    public string BaseUrlInput
    {
        get => baseUrlInput;
        set
        {
            if (Set(ref baseUrlInput, value)) BaseUrlError = null;
        }
    }

    public string ApiKeyInput
    {
        get => apiKeyInput;
        set
        {
            if (Set(ref apiKeyInput, value)) ApiKeyError = null;
        }
    }

    public string? BaseUrlError { get => baseUrlError; private set => Set(ref baseUrlError, value); }
    public string? ApiKeyError { get => apiKeyError; private set => Set(ref apiKeyError, value); }
    public bool IsConnecting { get => isConnecting; private set => Set(ref isConnecting, value); }
    public AppPage Page { get => page; private set => Set(ref page, value); }
    public IReadOnlyList<AppNotice> Notices { get => notices; private set => Set(ref notices, value); }
    public TimeSpan NoticeAutoDismissDelay { get; set; } = TimeSpan.FromSeconds(4);
    public AppNotice? TaskError { get => taskError; private set => Set(ref taskError, value); }
    public string? ProgressText { get => progressText; private set => Set(ref progressText, value); }
    public string SuccessTitle { get => successTitle; private set => Set(ref successTitle, value); }
    public string SuccessText { get => successText; private set => Set(ref successText, value); }
    public bool LoginStartEnabled { get => loginStartEnabled; private set => Set(ref loginStartEnabled, value); }
    public string? UpdateVersion { get => updateVersion; private set => Set(ref updateVersion, value); }
    public string? UpdateUrl { get => updateUrl; private set => Set(ref updateUrl, value); }
    public CoreErrorInfo? LastError
    {
        get => lastError;
        private set
        {
            if (Set(ref lastError, value)) RebuildBanners();
        }
    }
    public UninstallPause? UninstallPause { get => uninstallPause; private set => Set(ref uninstallPause, value); }
    public UninstallOutcome? Uninstalled { get => uninstalled; private set => Set(ref uninstalled, value); }
    public IReadOnlyList<AppBanner> Banners { get => banners; private set => Set(ref banners, value); }
    public string Version { get => version; private set => Set(ref version, value); }
    public string ConfigPath { get => configPath; private set => Set(ref configPath, value); }

    public bool ForwardingEnabled
    {
        get => forwardingEnabled;
        private set
        {
            if (!Set(ref forwardingEnabled, value)) return;
            Raise(nameof(RelayStatusText));
            Raise(nameof(CoreStatusText));
            Raise(nameof(AccessModeName));
            Raise(nameof(AccessHelpText));
            Raise(nameof(ForwardingHelpText));
        }
    }

    public bool ChatGptFound
    {
        get => chatGptFound;
        private set
        {
            if (!Set(ref chatGptFound, value)) return;
            Raise(nameof(ToggleEnabled));
            Raise(nameof(ChatGptText));
            Raise(nameof(ForwardingHelpText));
        }
    }

    public bool CoreRunning
    {
        get => coreRunning;
        private set
        {
            if (!Set(ref coreRunning, value)) return;
            Raise(nameof(CoreStatusText));
            Raise(nameof(ToggleEnabled));
        }
    }

    public bool IsBusy
    {
        get => busy;
        private set
        {
            if (!Set(ref busy, value)) return;
            Raise(nameof(ToggleEnabled));
        }
    }

    public bool IsDetectingAccess
    {
        get => isDetectingAccess;
        private set
        {
            if (!Set(ref isDetectingAccess, value)) return;
            Raise(nameof(ToggleEnabled));
        }
    }

    public bool ToggleEnabled => !busy && !isDetectingAccess && chatGptFound && coreRunning;

    public string? AccessModeName => forwardingEnabled ? ModeName(accessMode) : null;

    public string? AccessReasonText => accessDetect switch
    {
        "logged_in" => "· 已检测到 ChatGPT 登录",
        "not_logged_in" => "· 没有检测到 ChatGPT 登录",
        "failed" => "· 沿用上次的方式",
        _ => null,
    };

    public string? AccessHelpText => !forwardingEnabled ? null : accessMode switch
    {
        "relay" => "不需要登录 ChatGPT。模型列表里只有中台模型，默认选中「中台 · auto」。登录 ChatGPT 后点「重新检测」，就能同时用官方模型。",
        "both" => "模型列表里官方模型在上，中台模型在下；选官方模型时仍然访问官方服务。",
        _ => null,
    };

    public string ForwardingHelpText => !chatGptFound
        ? "找到 ChatGPT 后才能打开。"
        : forwardingEnabled
            ? "开启时，本助手按下方的接入方式修改 Codex 配置。"
            : "已关闭：Codex 配置已还原为开启前的样子。";

    public string CoreStatusText => coreRunning ? "后台运行中" : "后台服务已停止";

    public string TrayStatusText => coreRunning ? (forwardingEnabled ? "转发已开启" : "转发已关闭") + " · " + CoreStatusText : CoreStatusText;

    public string ChatGptText => chatGptFound ? "已检测到" : "未找到";

    public string ModelCountText => $"已获取 {modelCount} 个";

    public string RelayStatusText
    {
        get
        {
            if (!forwardingEnabled) return "转发关闭时不获取";
            if (modelCount == 0) return "暂无，Codex 里只显示官方模型";
            var time = FormatTime(fetchedAt);
            return time == null ? ModelCountText : $"{ModelCountText}，{time} 更新";
        }
    }

    public void DismissNotice(Guid id) => RemoveNotice(id);

    public void DismissTaskError() => TaskError = null;

    public void DismissBanner(AppBanner banner)
    {
        dismissedBanners.Add(BannerKey(banner));
        RebuildBanners();
    }

    public async Task StartAsync()
    {
        var url = TryReadBaseUrl();
        string? key = null;
        var keyUnreadable = false;
        try
        {
            key = vault.Read();
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            keyUnreadable = true;
        }

        if (url != null) BaseUrlInput = url;
        if (key != null) ApiKeyInput = key;
        Page = url == null ? AppPage.Setup : AppPage.Main;
        LoginStartEnabled = login.IsEnabled();
        if (keyUnreadable)
        {
            AddNotice(new AppNotice(KeyUnreadable, "", AppSeverity.Warning));
            ShowMainWindowRequested?.Invoke();
        }

        try
        {
            await core.StartAsync(new CoreInitParams(url, key, chatGpt.Locate()));
            CoreRunning = true;
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException or IOException or System.ComponentModel.Win32Exception)
        {
            CoreRunning = false;
            AddNotice(new AppNotice(CoreStartFailed, Describe(ex)));
            return;
        }

        try
        {
            ApplyStatus(await core.CallAsync("status"));
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
        }
    }

    public async Task CompleteSetupAsync()
    {
        if (IsConnecting) return;
        if (!Validate()) return;

        IsConnecting = true;
        TaskError = null;
        try
        {
            var url = BaseUrlInput.Trim();
            var key = ApiKeyInput.Trim();
            if (!await SaveToCoreAsync(url, key)) return;

            var persistError = await PersistAsync(url, key, null, null);
            if (persistError != null)
            {
                AddNotice(new AppNotice(NoSave, persistError));
                return;
            }

            RemoveNotices(NoSave);
            var problems = new List<string>();
            try
            {
                ApplyStatus(await core.CallAsync("set_forwarding", Params("enabled", true)));
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                problems.Add("没能打开转发：" + Describe(ex));
            }

            try
            {
                login.Enable();
                LoginStartEnabled = true;
            }
            catch (Exception ex) when (IsSystemFault(ex))
            {
                problems.Add("没能开启登录时启动");
            }

            SuccessTitle = forwardingEnabled ? "设置完成，转发已开启" : "设置已保存";
            SuccessText = $"已获取 {modelCount} 个中台模型。{SetupAccessText()}\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。";
            if (problems.Count > 0)
                AddNotice(new AppNotice("设置已保存，但有一项没完成", string.Join("\n", problems), AppSeverity.Warning));
            Page = AppPage.Success;
        }
        finally
        {
            IsConnecting = false;
        }
    }

    public void FinishSetup() => Page = AppPage.Main;

    public async Task ToggleForwardingAsync()
    {
        if (busy || !chatGptFound) return;

        IsBusy = true;
        LastError = null;
        try
        {
            var target = !forwardingEnabled;
            try
            {
                ApplyStatus(await core.CallAsync("set_forwarding", Params("enabled", target)));
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                if (ex is CoreCallException call) LastError = new CoreErrorInfo(call.Code, call.Keys);
                if (!HasErrorBanner(lastError))
                    AddNotice(new AppNotice(SwitchFailed, Describe(ex)));
                ShowMainWindowRequested?.Invoke();
                return;
            }

            RemoveNotices(SwitchFailed);
            if (chatGpt.IsRunning())
                AddNotice(new AppNotice(target ? "已打开转发" : "已关闭转发", RestartHint, AppSeverity.Success));
        }
        finally
        {
            IsBusy = false;
        }
    }

    public async Task RecheckAccessAsync()
    {
        RemoveNotices(DetectFailedTitle);
        IsDetectingAccess = true;
        try
        {
            JsonElement status;
            try
            {
                status = await core.CallAsync("detect_access");
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                AddNotice(new AppNotice(RecheckFailedTitle, Describe(ex)));
                return;
            }

            RemoveNotices(RecheckFailedTitle);
            ApplyStatus(status);
            var access = Prop(status, "access");
            var mode = Str(access, "mode");
            var target = Str(access, "target");
            if (Str(access, "detect") == "failed")
            {
                AddNotice(DetectFailedNotice());
                return;
            }

            if (target == null) return;
            if (target == mode)
            {
                var detail = mode switch
                {
                    "relay" => "仍然没有检测到 ChatGPT 登录，继续只用中台模型。",
                    "both" => "仍然检测到 ChatGPT 登录，继续同时使用官方模型和中台模型。",
                    _ => null,
                };
                if (detail != null) AddNotice(new AppNotice(NoChangeTitle, detail, AppSeverity.Info));
                return;
            }

            await ReconcileAccessAsync(target);
        }
        finally
        {
            IsDetectingAccess = false;
        }
    }

    void OnStatusReceived(JsonElement s)
    {
        if (startupAligned || !Flag(s, "forwarding", false)) return;
        startupAligned = true;

        var access = Prop(s, "access");
        var target = Str(access, "target");
        if (target == null) return;
        if (Str(access, "detect") == "failed")
        {
            AddNotice(DetectFailedNotice());
            return;
        }

        if (target != Str(access, "mode")) _ = ReconcileAccessAsync(target);
    }

    async Task ReconcileAccessAsync(string target)
    {
        LastError = null;
        try
        {
            ApplyStatus(await core.CallAsync("apply_access"));
            RemoveNotices(AccessSwitchFailed);
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            if (ex is CoreCallException call) LastError = new CoreErrorInfo(call.Code, call.Keys);
            if (!HasErrorBanner(lastError))
                AddNotice(new AppNotice(AccessSwitchFailed, Describe(ex)));
            ShowMainWindowRequested?.Invoke();
            return;
        }

        if (!chatGpt.IsRunning()) return;
        AddNotice(target == "both"
            ? new AppNotice("已切换到「官方 + 中台」", "已检测到 ChatGPT 登录。重启 ChatGPT 后生效。", AppSeverity.Success)
            : new AppNotice("已切换到「只用中台」", "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。", AppSeverity.Success));
    }

    static AppNotice DetectFailedNotice() =>
        new(DetectFailedTitle, "沿用上次的方式。可以稍后点「重新检测」再试。", AppSeverity.Warning);

    static string? ModeName(string? mode) => mode switch
    {
        "relay" => "只用中台",
        "both" => "官方 + 中台",
        _ => null,
    };

    string SetupAccessText() => accessMode switch
    {
        "relay" => "没有检测到 ChatGPT 登录，接入方式为「只用中台」：Codex 不需要登录，模型列表里只有中台模型。",
        "both" => "已检测到 ChatGPT 登录，接入方式为「官方 + 中台」。",
        _ => "",
    };

    public async Task SaveConnectionAsync()
    {
        if (busy || !Validate()) return;

        IsBusy = true;
        ProgressText = "正在保存并重新获取模型…";
        try
        {
            var url = BaseUrlInput.Trim();
            var key = ApiKeyInput.Trim();
            var oldUrl = TryReadBaseUrl();
            string? oldKey = null;
            try { oldKey = vault.Read(); }
            catch (Exception ex) when (IsSystemFault(ex)) { }

            JsonElement result;
            try
            {
                result = await core.CallAsync("save_connection", ConnectionParams(url, key));
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                AddNotice(new AppNotice(NoSave, Describe(ex)));
                return;
            }

            var persistError = await PersistAsync(url, key, oldUrl, oldKey);
            if (persistError != null)
            {
                AddNotice(new AppNotice(NoSave, persistError));
                return;
            }

            modelCount = Int(result, "relay_model_count");
            Raise(nameof(ModelCountText));
            Raise(nameof(RelayStatusText));

            RemoveNotices(NoSave, KeyUnreadable);
            if (Flag(result, "models_changed", false) && chatGpt.IsRunning())
                AddNotice(new AppNotice(
                    "已保存",
                    $"现在可用 {modelCount} 个中台模型。重启 ChatGPT 后，Codex 里会显示新的列表。",
                    AppSeverity.Success));
            else
                AddNotice(new AppNotice(SavedNoRestart, "已重新获取中台模型，不需要重启 ChatGPT。", AppSeverity.Success));
        }
        finally
        {
            ProgressText = null;
            IsBusy = false;
        }
    }

    public async Task SetLoginStartAsync(bool enable)
    {
        if (!enable && forwardingEnabled)
        {
            var request = new ConfirmRequest(
                "关闭「登录时启动」？",
                "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中台模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
                "仍然关闭");
            if (!await confirmer.ConfirmAsync(request))
            {
                Raise(nameof(LoginStartEnabled));
                return;
            }
        }

        try
        {
            if (enable) login.Enable(); else login.Disable();
            LoginStartEnabled = enable;
            RemoveNotices(enable ? "没能开启登录时启动" : "没能关闭登录时启动");
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            AddNotice(new AppNotice(enable ? "没能开启登录时启动" : "没能关闭登录时启动"));
            Raise(nameof(LoginStartEnabled));
        }
    }

    public async Task QuitAsync()
    {
        if (busy) return;

        IsBusy = true;
        try
        {
            var restore = forwardingEnabled;
            LastError = null;
            if (restore)
            {
                var tail = chatGpt.IsRunning() ? QuitRunningTail : "";
                if (!await confirmer.ConfirmAsync(new ConfirmRequest("退出模型接入助手？", QuitBase + tail, "还原并退出")))
                    return;
            }

            try
            {
                await core.CallAsync("shutdown", Params("restore", restore));
            }
            catch (CoreCallException ex) when (ex.Code == "restore_blocked")
            {
                LastError = new CoreErrorInfo(ex.Code, ex.Keys);
                await StopCoreAsync();
                ShowMainWindowRequested?.Invoke();
                return;
            }
            catch (CoreCallException ex)
            {
                AddNotice(new AppNotice("没能退出", Describe(ex)));
                ShowMainWindowRequested?.Invoke();
                return;
            }
            catch (CoreProcessExitedException)
            {
            }

            await StopCoreAsync();
            ExitRequested?.Invoke();
        }
        finally
        {
            IsBusy = false;
        }
    }

    public async Task StopCoreAsync()
    {
        await core.StopAsync();
        CoreRunning = false;
    }

    public bool IsAlreadyCleaned()
    {
        try
        {
            return settings.ReadBaseUrl() == null && vault.Read() == null && !login.IsEnabled();
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            return false;
        }
    }

    public Task<int> UninstallAsync() => RunUninstallAsync(true);

    public Task<int> RecheckUninstallAsync() => RunUninstallAsync(false);

    async Task<int> RunUninstallAsync(bool askFirst)
    {
        if (busy) return 1;

        IsBusy = true;
        try
        {
            if (askFirst)
            {
                var tail = chatGpt.IsRunning() ? "ChatGPT 正在运行，卸载后请重启 ChatGPT。" : "";
                var request = new ConfirmRequest(
                    "卸载模型接入助手？",
                    "卸载会：\n· 关闭转发，把 Codex 配置还原成安装前的样子\n· 从系统凭据管理器删除中台 API Key，删除本助手保存的 Base URL\n· 取消「登录时启动」，删除本助手\n\n不会删除 ChatGPT、聊天记录，以及你自己的其他 Codex 设置。"
                    + (tail == "" ? "" : "\n" + tail),
                    "卸载");
                if (!await confirmer.ConfirmAsync(request)) return 1;
            }

            UninstallPause = null;
            ProgressText = "正在卸载";
            try
            {
                await core.CallAsync("prepare_uninstall");
            }
            catch (CoreCallException ex) when (ex.Code == "restore_blocked")
            {
                UninstallPause = new UninstallPause(ex.Keys, configPath);
                return 2;
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                AddNotice(new AppNotice("没能卸载", Describe(ex)));
                return 1;
            }

            var leftovers = new List<string>();
            try { vault.Delete(); }
            catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("系统凭据管理器里的 API Key 没能删除"); }
            try { settings.DeleteAll(); }
            catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("本助手保存的设置没能删除"); }
            try { login.Disable(); }
            catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("登录时启动项没能取消"); }

            await StopCoreAsync();
            Uninstalled = new UninstallOutcome(leftovers, chatGpt.IsRunning());
            return 0;
        }
        finally
        {
            ProgressText = null;
            IsBusy = false;
        }
    }

    public async Task CheckUpdateAsync()
    {
        try
        {
            var result = await core.CallAsync("check_update", Params("manual", true));
            RemoveNotices(UpdateUnavailable);
            var latest = Str(result, "latest_version");
            var url = Str(result, "url");
            if (Flag(result, "is_newer", false) && latest != null)
            {
                UpdateVersion = latest;
                UpdateUrl = url;
            }
            else
            {
                UpdateVersion = null;
                UpdateUrl = null;
                AddNotice(new AppNotice("已是最新版本", "", AppSeverity.Success));
            }
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            AddNotice(new AppNotice(UpdateUnavailable, "", AppSeverity.Warning));
        }
    }

    public Task RetryAsync() => RefreshAsync("refresh_models");

    public Task RecheckAsync()
    {
        LastError = null;
        return RefreshAsync("status");
    }

    async Task RefreshAsync(string method)
    {
        if (busy) return;
        IsBusy = true;
        try
        {
            ApplyStatus(await core.CallAsync(method));
            RemoveNotices(RefreshFailed);
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            AddNotice(new AppNotice(RefreshFailed, Describe(ex)));
        }
        finally
        {
            IsBusy = false;
        }
    }

    async Task<bool> SaveToCoreAsync(string url, string key)
    {
        try
        {
            var result = await core.CallAsync("save_connection", ConnectionParams(url, key));
            modelCount = Int(result, "relay_model_count");
            Raise(nameof(ModelCountText));
            return true;
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            TaskError = new AppNotice("没能连上中台", Describe(ex));
            return false;
        }
    }

    async Task<string?> PersistAsync(string url, string key, string? oldUrl, string? oldKey)
    {
        string? error = null;
        try
        {
            vault.Save(key);
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            error = CredentialWriteFailed;
        }

        if (error == null)
        {
            try
            {
                settings.SaveBaseUrl(url);
            }
            catch (Exception ex) when (IsSystemFault(ex))
            {
                error = SettingsWriteFailed;
                try
                {
                    if (oldKey != null) vault.Save(oldKey);
                    else vault.Delete();
                }
                catch (Exception inner) when (IsSystemFault(inner)) { }
            }
        }

        if (error != null) await RollbackCoreAsync(oldUrl, oldKey);
        return error;
    }

    async Task RollbackCoreAsync(string? oldUrl, string? oldKey)
    {
        try
        {
            if (oldUrl != null && oldKey != null)
                await core.CallAsync("save_connection", ConnectionParams(oldUrl, oldKey));
            else
                await core.CallAsync("init", new Dictionary<string, object?>
                {
                    ["base_url"] = null,
                    ["api_key"] = null,
                    ["chatgpt_dir"] = chatGpt.Locate(),
                });
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException) { }
    }

    string? TryReadBaseUrl()
    {
        try
        {
            return settings.ReadBaseUrl();
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            return null;
        }
    }

    bool Validate()
    {
        BaseUrlError = string.IsNullOrWhiteSpace(baseUrlInput)
            ? Required
            : baseUrlInput.Trim().StartsWith("https://", StringComparison.OrdinalIgnoreCase)
                ? null
                : Reason("invalid_base_url");
        ApiKeyError = string.IsNullOrWhiteSpace(apiKeyInput) ? Required : null;
        return BaseUrlError == null && ApiKeyError == null;
    }

    void OnCoreFailed(string reason)
    {
        CoreRunning = false;
        AddNotice(new AppNotice(CoreStartFailed, reason));
    }

    void ApplyStatus(JsonElement s)
    {
        if (s.ValueKind != JsonValueKind.Object) return;

        var relay = Prop(s, "relay");
        var proxy = Prop(s, "proxy");
        var chatgpt = Prop(s, "chatgpt");
        var update = Prop(s, "update");

        ForwardingEnabled = Flag(s, "forwarding", false);
        ChatGptFound = Flag(chatgpt, "found", true);
        Version = Str(s, "version") is { } v ? "v" + v : "";
        ConfigPath = Str(s, "config_path") ?? "";
        modelCount = Int(relay, "model_count");
        fetchedAt = Str(relay, "fetched_at");
        var access = Prop(s, "access");
        accessMode = Str(access, "mode");
        accessDetect = Str(access, "detect");
        Raise(nameof(AccessModeName));
        Raise(nameof(AccessReasonText));
        Raise(nameof(AccessHelpText));
        Raise(nameof(ModelCountText));
        Raise(nameof(RelayStatusText));

        if (Str(update, "latest_version") is { } latest)
        {
            UpdateVersion = latest;
            UpdateUrl = Str(update, "url");
        }

        var list = new List<AppBanner>();
        if (Strings(s, "taken_over").Count > 0)
            list.Add(new AppBanner(
                "已接管 Codex 原有的接入设置",
                "开启转发前，Codex 配置里已有别的「官方服务地址」设置。原值已备份，关闭转发或卸载时会还原。",
                AppSeverity.Info,
                []));

        var relayError = Str(relay, "error");
        if (Flag(relay, "stale", false))
        {
            var time = FormatTime(fetchedAt);
            var lines = new List<string>();
            if (relayError != null) lines.Add(Reason(relayError));
            lines.Add("Codex 里继续使用上次获取的列表" + (time == null ? "。" : $"（{time}）。"));
            list.Add(new AppBanner("中台模型列表本次没有更新", string.Join("\n", lines), AppSeverity.Warning, [BannerAction.Retry]));
        }

        if (modelCount == 0 && relayError != null)
            list.Add(new AppBanner(
                "还没有获取到中台模型",
                Reason(relayError) + "\n请检查下方的 API Key，保存后会重新获取。",
                AppSeverity.Danger,
                [BannerAction.Retry]));

        var blocked = Strings(s, "restore_blocked");
        if (blocked.Count > 0)
            list.Add(RestoreBlockedBanner(blocked));

        if (Str(proxy, "error") is { } proxyError)
            list.Add(new AppBanner("本机转发服务没能启动", Reason(proxyError), AppSeverity.Danger, [BannerAction.Retry]));

        if (!chatGptFound)
            list.Add(new AppBanner(
                "没有找到 ChatGPT",
                "请先安装官方 ChatGPT 桌面版，再回来打开转发。如果已经安装，可能是当前 ChatGPT 版本暂不支持，请等待助手新版本。",
                AppSeverity.Danger,
                [BannerAction.Redetect]));

        statusBanners = list;
        RebuildBanners();
    }

    void RebuildBanners()
    {
        var list = new List<AppBanner>(statusBanners);
        if (lastError is { Code: "restore_blocked" } blocked)
        {
            if (list.All(b => b.Title != RestoreBlockedTitle)) list.Add(RestoreBlockedBanner(blocked.Keys));
        }
        else if (lastError is { Code: "config_invalid", Keys.Count: > 0 } invalid)
            list.Add(new AppBanner(Reason(invalid.Code), "", AppSeverity.Danger, [BannerAction.ShowDetail])
            {
                Code = invalid.Code,
                Keys = invalid.Keys,
                ConfigPath = configPath,
            });

        dismissedBanners.IntersectWith(list.Select(BannerKey));
        Banners = list.Where(b => !dismissedBanners.Contains(BannerKey(b))).ToList();
    }

    static string UpdateUnavailable => CoreErrorMessages.Messages["update_unavailable"];

    static bool HasErrorBanner(CoreErrorInfo? error) =>
        error is { Code: "restore_blocked" } or { Code: "config_invalid", Keys.Count: > 0 };

    static string BannerKey(AppBanner b) =>
        string.Join("\n", b.Title, b.Detail, string.Join(",", b.Keys), b.ConfigPath);

    AppBanner RestoreBlockedBanner(IReadOnlyList<string> keys) => new(
        RestoreBlockedTitle, "有设置项被手动改过。", AppSeverity.Danger, [BannerAction.ShowDetail])
    {
        Code = "restore_blocked",
        Keys = keys,
        ConfigPath = configPath,
    };

    void AddNotice(AppNotice notice)
    {
        lock (noticeLock)
        {
            if (notices.Any(n => n.Title == notice.Title && n.Severity == notice.Severity)) return;
            Notices = [.. notices, notice];
        }

        if (notice.Severity is AppSeverity.Success or AppSeverity.Info)
            _ = DismissAfterDelayAsync(notice.Id, NoticeAutoDismissDelay);
    }

    async Task DismissAfterDelayAsync(Guid id, TimeSpan delay)
    {
        await Task.Delay(delay);
        RemoveNotice(id);
    }

    void RemoveNotice(Guid id)
    {
        lock (noticeLock)
        {
            if (notices.All(n => n.Id != id)) return;
            Notices = notices.Where(n => n.Id != id).ToList();
        }
    }

    void RemoveNotices(params string[] titles)
    {
        lock (noticeLock)
        {
            if (!notices.Any(n => titles.Contains(n.Title))) return;
            Notices = notices.Where(n => !titles.Contains(n.Title)).ToList();
        }
    }

    static string Reason(string code) =>
        CoreErrorMessages.Messages.TryGetValue(code, out var message) ? message : CoreErrorMessages.Messages["internal_error"];

    static string Describe(Exception ex) => ex is CoreCallException call ? Reason(call.Code) : CoreStopped;

    static bool IsSystemFault(Exception ex) =>
        ex is IOException or UnauthorizedAccessException or SecurityException or System.ComponentModel.Win32Exception;

    static string? FormatTime(string? iso) =>
        DateTimeOffset.TryParse(iso, out var time) ? time.ToLocalTime().ToString("M月d日 HH:mm") : null;

    static Dictionary<string, object?> Params(string name, object? value) => new() { [name] = value };

    static Dictionary<string, object?> ConnectionParams(string url, string key) =>
        new() { ["base_url"] = url, ["api_key"] = key };

    static JsonElement Prop(JsonElement e, string name) =>
        e.ValueKind == JsonValueKind.Object && e.TryGetProperty(name, out var v) ? v : default;

    static string? Str(JsonElement e, string name)
    {
        var v = Prop(e, name);
        return v.ValueKind == JsonValueKind.String ? v.GetString() : null;
    }

    static int Int(JsonElement e, string name)
    {
        var v = Prop(e, name);
        return v.ValueKind == JsonValueKind.Number && v.TryGetInt32(out var n) ? n : 0;
    }

    static bool Flag(JsonElement e, string name, bool fallback) => Prop(e, name).ValueKind switch
    {
        JsonValueKind.True => true,
        JsonValueKind.False => false,
        _ => fallback,
    };

    static List<string> Strings(JsonElement e, string name)
    {
        var v = Prop(e, name);
        return v.ValueKind == JsonValueKind.Array ? v.EnumerateArray().Select(x => x.GetString() ?? "").ToList() : [];
    }

    bool Set<T>(ref T field, T value, [CallerMemberName] string? name = null)
    {
        if (EqualityComparer<T>.Default.Equals(field, value)) return false;
        field = value;
        Raise(name);
        return true;
    }

    void Raise(string? name) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
