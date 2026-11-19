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
    public bool DetailInDialog { get; init; }
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

public enum RelayRowState { Normal, Stale, NeverFetched, Fetching, ForwardingOff }

public sealed record RelayRow(
    string Id,
    string Name,
    string BaseUrl,
    RelayRowState State,
    string StatusText,
    bool CanRetry,
    bool CanEdit,
    bool CanDelete);

sealed record RelayStatus(int ModelCount, string? FetchedAt, bool Stale);

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
    void Save(string id, string key);
    string? Read(string id);
    void Delete(string id);
    void DeleteAll();
    bool Any();
}

public interface IShellSettingsStore
{
    IReadOnlyList<RelayConfig> ReadRelays();
    string? TryBackUp() => null;
    bool HasBackup() => false;
    void SaveRelays(IReadOnlyList<RelayConfig> relays);
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

public sealed class AppModel : IRelayFields
{
    public const string ActivationFailedMessage = "模型接入助手已在运行，但无法切换到它，请从任务栏通知区域打开";

    const string NoSave = "没有保存";
    public const string SettingsBackedUp = "原来的设置读不出来，已备份原文件";
    public const string SettingsUnreadable = "原来的设置读不出来";
    const string CredentialWriteFailed = "无法写入系统凭据管理器";
    const string SettingsWriteFailed = "无法写入设置文件";
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
    public const int MaxRelays = 10;
    public const int MaxNameLength = 16;
    const string QuitBase = "退出前会关闭转发，并把 Codex 配置还原成开启前的样子。";
    const string QuitRunningTail = "ChatGPT 正在运行，请在退出后重启 ChatGPT。";

    const string DefaultRelayName = "中转";

    readonly ICoreClient core;
    readonly ICredentialVault vault;
    readonly IShellSettingsStore settings;
    readonly ILoginStart login;
    readonly IChatGptControl chatGpt;
    readonly IConfirmer confirmer;
    RelayEditorModel? activeEditor;
    int forwardingToggles;

    string nameInput = DefaultRelayName;
    string baseUrlInput = "";
    string apiKeyInput = "";
    string? nameError;
    string? baseUrlError;
    string? apiKeyError;
    bool isConnecting;
    bool busy;
    AppPage page;
    IReadOnlyList<AppNotice> notices = [];
    readonly object noticeLock = new();
    readonly HashSet<string> dismissedBanners = [];
    AppNotice? taskError;
    string successTitle = "";
    string successText = "";
    bool forwardingEnabled;
    bool chatGptFound = true;
    bool chatGptUnsupported;
    bool coreForwarding;
    bool quitRestore;
    bool isRechecking;
    bool loginStartEnabled;
    bool coreRunning;
    string? accessMode;
    string? accessDetect;
    bool isDetectingAccess;
    bool startupAligned;
    IReadOnlyList<RelayConfig> configs = [];
    Dictionary<string, RelayStatus> relayStatus = [];
    readonly HashSet<string> fetching = [];
    string? defaultRelay;
    string? deletingRelayId;
    IReadOnlyList<RelayRow> relays = [];
    string version = "";
    string configPath = "";
    string? updateVersion;
    string? updateUrl;
    bool updateIsInstaller;
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

    public static string SetupHeading => "连接中转服务";
    public static string SetupDescription => "填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。";
    public static string ConnectingText => "正在连接中转…";

    public string NameInput
    {
        get => nameInput;
        set
        {
            if (Set(ref nameInput, value)) NameError = null;
        }
    }

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

    public string? NameError { get => nameError; private set => Set(ref nameError, value); }
    public string? BaseUrlError { get => baseUrlError; private set => Set(ref baseUrlError, value); }
    public string? ApiKeyError { get => apiKeyError; private set => Set(ref apiKeyError, value); }
    public bool IsConnecting { get => isConnecting; private set => Set(ref isConnecting, value); }
    public AppPage Page { get => page; private set => Set(ref page, value); }
    public IReadOnlyList<AppNotice> Notices { get => notices; private set => Set(ref notices, value); }
    public TimeSpan NoticeAutoDismissDelay { get; set; } = TimeSpan.FromSeconds(4);
    public AppNotice? TaskError { get => taskError; private set => Set(ref taskError, value); }
    public string SuccessTitle { get => successTitle; private set => Set(ref successTitle, value); }
    public string SuccessText { get => successText; private set => Set(ref successText, value); }
    public bool LoginStartEnabled { get => loginStartEnabled; private set => Set(ref loginStartEnabled, value); }
    public string? UpdateVersion { get => updateVersion; private set => Set(ref updateVersion, value); }
    public string? UpdateUrl { get => updateUrl; private set => Set(ref updateUrl, value); }
    public bool UpdateIsInstaller { get => updateIsInstaller; private set => Set(ref updateIsInstaller, value); }
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
            Raise(nameof(CoreStatusText));
            Raise(nameof(AccessModeName));
            Raise(nameof(OverviewTitle));
            Raise(nameof(OverviewText));
            Raise(nameof(ForwardingHelpText));
            RebuildRelays();
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

    public bool ChatGptUnsupported
    {
        get => chatGptUnsupported;
        private set
        {
            if (!Set(ref chatGptUnsupported, value)) return;
            Raise(nameof(ToggleEnabled));
            Raise(nameof(ChatGptText));
            Raise(nameof(ForwardingHelpText));
            Raise(nameof(OverviewText));
        }
    }

    public bool IsRechecking
    {
        get => isRechecking;
        private set => Set(ref isRechecking, value);
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
            RebuildRelays();
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

    public bool ToggleEnabled => !busy && chatGptFound && !chatGptUnsupported && coreRunning;

    public string? AccessModeName => forwardingEnabled ? ModeName(accessMode) : null;

    public string? AccessReasonText => accessDetect switch
    {
        "logged_in" => "· 已检测到 ChatGPT 登录",
        "not_logged_in" => "· 没有检测到 ChatGPT 登录",
        "failed" => "· 沿用上次的方式",
        _ => null,
    };

    public string OverviewTitle => "使用中转 · " + (forwardingEnabled ? "已开启" : "已关闭");

    public string? OverviewText => chatGptUnsupported
        ? UnsupportedHelp
        : !forwardingEnabled
        ? "已关闭：Codex 配置已还原为开启前的样子。"
        : accessMode switch
        {
            "both" => "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。",
            "relay" => defaultRelay is { } name
                ? $"不用登录 ChatGPT。只有中转模型，默认选中「{name} · auto」。"
                : "不用登录 ChatGPT。只有中转模型，没有默认选中的模型。",
            _ => null,
        };

    const string UnsupportedHelp = "当前 ChatGPT 版本暂不支持，暂时不能打开。";

    public string ForwardingHelpText => !chatGptFound
        ? "找到 ChatGPT 后才能打开。"
        : chatGptUnsupported
            ? UnsupportedHelp
        : forwardingEnabled
            ? ""
            : "已关闭：Codex 配置已还原为开启前的样子。";

    public string CoreStatusText => coreRunning ? "后台运行中" : "后台服务已停止";

    public string TrayStatusText => coreRunning ? (forwardingEnabled ? "转发已开启" : "转发已关闭") + " · " + CoreStatusText : CoreStatusText;

    public string ChatGptText => chatGptUnsupported ? "版本暂不支持" : chatGptFound ? "已检测到" : "未找到";

    public int ModelCountOf(string id) => relayStatus.GetValueOrDefault(id)?.ModelCount ?? 0;

    public string ApiKeyHelp => RelayEditorModel.ApiKeyHelpText;

    public bool FieldsEnabled => !isConnecting && !busy;

    public string? DeletingRelayId { get => deletingRelayId; private set => Set(ref deletingRelayId, value); }

    public IReadOnlyList<RelayRow> Relays { get => relays; private set => Set(ref relays, value); }

    public string RelayCountText => configs.Count >= MaxRelays
        ? $"已有 {configs.Count} 个，达到上限。"
        : $"已有 {configs.Count} 个，最多 {MaxRelays} 个。";

    public bool CanAddRelay => configs.Count < MaxRelays && !busy;

    public string? RelayMinimumText => configs.Count <= 1 ? "至少保留一个配置。" : null;

    public string RelayModelSummary => !forwardingEnabled
        ? "中转模型 转发关闭时不获取"
        : $"中转模型 {configs.Sum(c => relayStatus.GetValueOrDefault(c.Id)?.ModelCount ?? 0)} 个（{configs.Count} 个配置）";

    public void DismissNotice(Guid id) => RemoveNotice(id);

    public void DismissTaskError() => TaskError = null;

    public void DismissBanner(AppBanner banner)
    {
        dismissedBanners.Add(BannerKey(banner));
        RebuildBanners();
    }

    public async Task StartAsync()
    {
        configs = TryReadRelays();
        var inits = new List<RelayInit>();
        var keyUnreadable = false;
        foreach (var config in configs)
        {
            var key = "";
            try
            {
                key = vault.Read(config.Id) ?? "";
            }
            catch (Exception ex) when (IsSystemFault(ex))
            {
                keyUnreadable = true;
            }
            inits.Add(new RelayInit(config.Id, config.Name, config.BaseUrl, key));
        }

        RebuildRelays();
        Page = configs.Count == 0 ? AppPage.Setup : AppPage.Main;
        try { LoginStartEnabled = login.IsEnabled(); }
        catch (Exception ex) when (IsSystemFault(ex)) { LoginStartEnabled = false; }
        if (keyUnreadable)
        {
            AddNotice(new AppNotice(KeyUnreadable, "", AppSeverity.Warning));
            ShowMainWindowRequested?.Invoke();
        }

        try
        {
            await core.StartAsync(new CoreInitParams(inits, chatGpt.Locate()));
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
        if (!ValidateSetup()) return;

        IsConnecting = true;
        TaskError = null;
        try
        {
            var name = NameInput.Trim();
            var url = BaseUrlInput.Trim();
            var key = ApiKeyInput.Trim();
            JsonElement saved;
            try
            {
                saved = await core.CallAsync("save_relay", RelayParams(null, name, url, key));
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                TaskError = new AppNotice("没能连上中转服务", Describe(ex).TrimEnd('。') + "。");
                return;
            }

            var id = Str(saved, "id") ?? "1";
            var modelCount = Int(saved, "model_count");
            var config = new RelayConfig(id, Str(saved, "name") ?? name, url);
            var persistError = Persist(config, key, null, [config]);
            if (persistError != null)
            {
                var reverted = await RollbackAddAsync(id);
                AddNotice(new AppNotice(
                    NoSave,
                    persistError + (reverted ? "" : "\n已在本次运行中生效，但没能保存到本机，重启助手后会恢复成原来的样子。")));
                return;
            }

            configs = [config];
            RebuildRelays();
            RemoveNotices(NoSave, SettingsBackedUp, SettingsUnreadable);
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
            SuccessText = $"已获取 {modelCount} 个中转模型。{SetupAccessText()}\n请重启 ChatGPT 后使用。助手会在你登录电脑时自动启动，在任务栏通知区域里常驻。";
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
        if (busy || !chatGptFound || chatGptUnsupported) return;

        IsBusy = true;
        forwardingToggles++;
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
        var togglesBefore = forwardingToggles;
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
            var toggledMeanwhile = forwardingToggles != togglesBefore;
            var forwardingNow = forwardingEnabled;
            ApplyStatus(status);
            if (toggledMeanwhile) ForwardingEnabled = forwardingNow;
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
                    "relay" => "仍然没有检测到 ChatGPT 登录，继续只用中转模型。",
                    "both" => "仍然检测到 ChatGPT 登录，继续同时使用官方模型和中转模型。",
                    _ => null,
                };
                if (detail != null) AddNotice(new AppNotice(NoChangeTitle, detail, AppSeverity.Info));
                return;
            }

            if (toggledMeanwhile || !forwardingEnabled) return;
            await ReconcileAccessAsync(target);
        }
        finally
        {
            IsDetectingAccess = false;
        }
    }

    static bool ForwardingUsable(JsonElement s)
    {
        var chatgpt = Prop(s, "chatgpt");
        return Flag(s, "forwarding", false) && Flag(chatgpt, "found", true) && Str(chatgpt, "error") != "chatgpt_unsupported";
    }

    void OnStatusReceived(JsonElement s)
    {
        if (startupAligned || busy || !ForwardingUsable(s)) return;
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
        if (busy) return;
        IsBusy = true;
        try
        {
            await ApplyAccessAsync(target);
        }
        finally
        {
            IsBusy = false;
        }
    }

    async Task ApplyAccessAsync(string target)
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
            ? new AppNotice("已切换到「官方 + 中转」", "已检测到 ChatGPT 登录。重启 ChatGPT 后生效。", AppSeverity.Success)
            : new AppNotice("已切换到「只用中转」", "没有检测到 ChatGPT 登录。重启 ChatGPT 后生效。", AppSeverity.Success));
    }

    static AppNotice DetectFailedNotice() =>
        new(DetectFailedTitle, "沿用上次的方式。可以稍后点「重新检测」再试。", AppSeverity.Warning);

    static string? ModeName(string? mode) => mode switch
    {
        "relay" => "只用中转",
        "both" => "官方 + 中转",
        _ => null,
    };

    string SetupAccessText() => accessMode switch
    {
        "relay" => "没有检测到 ChatGPT 登录，接入方式为「只用中转」：Codex 不需要登录，模型列表里只有中转模型。",
        "both" => "已检测到 ChatGPT 登录，接入方式为「官方 + 中转」。",
        _ => "",
    };

    public RelayEditorModel? BeginAddRelay()
    {
        if (busy || activeEditor is { IsSaving: true }) return null;
        return activeEditor = new RelayEditorModel(this, null, "");
    }

    public RelayEditorModel? BeginEditRelay(string id)
    {
        if (busy || activeEditor is { IsSaving: true }) return null;
        var config = configs.First(c => c.Id == id);
        var key = "";
        try
        {
            key = vault.Read(id) ?? "";
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
        }
        return activeEditor = new RelayEditorModel(this, config, key);
    }

    internal async Task<bool> SaveRelayAsync(RelayEditorModel editor)
    {
        if (editor.IsSaving || busy) return false;

        var name = editor.NameInput.Trim();
        var url = editor.BaseUrlInput.Trim();
        var key = editor.ApiKeyInput.Trim();
        var (nameError, urlError, keyError) = ValidateRelay(name, url, key, configs.Where(c => c.Id != editor.Id));
        editor.NameError = nameError;
        editor.BaseUrlError = urlError;
        editor.ApiKeyError = keyError;
        if (nameError != null || urlError != null || keyError != null) return false;

        editor.SaveAlert = null;
        editor.IsSaving = true;
        IsBusy = true;
        try
        {
            var existing = editor.Id == null ? null : configs.FirstOrDefault(c => c.Id == editor.Id);
            string? oldKey = null;
            if (existing != null)
            {
                try { oldKey = vault.Read(existing.Id); }
                catch (Exception ex) when (IsSystemFault(ex)) { }
            }

            JsonElement saved;
            try
            {
                saved = await core.CallAsync("save_relay", RelayParams(editor.Id, name, url, key));
            }
            catch (CoreCallException ex) when (ex.Code is "name_empty" or "name_too_long" or "name_has_dot" or "name_duplicate")
            {
                editor.NameError = Reason(ex.Code, ex.Keys);
                return false;
            }
            catch (CoreCallException ex) when (ex.Code == "invalid_base_url")
            {
                editor.BaseUrlError = Reason(ex.Code);
                return false;
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                editor.SaveAlert = ex switch
                {
                    CoreCallException { Code: "relay_5xx" or "relay_4xx" or "relay_bad_response" } =>
                        new DialogAlert(NoSave, Describe(ex), DialogAlertSeverity.Danger),
                    CoreCallException => SaveFailedAlert(Describe(ex)),
                    _ => new DialogAlert(NoSave, CoreStopped + "。", DialogAlertSeverity.Danger),
                };
                return false;
            }

            var id = Str(saved, "id") ?? editor.Id ?? "";
            var config = new RelayConfig(id, Str(saved, "name") ?? name, url);
            var next = existing == null
                ? [.. configs, config]
                : configs.Select(c => c.Id == id ? config : c).ToList();
            var persistError = Persist(config, key, oldKey, next);
            if (persistError != null)
            {
                var reverted = existing == null
                    ? await RollbackAddAsync(id)
                    : await RollbackEditAsync(existing, oldKey);
                editor.SaveAlert = SaveFailedAlert(persistError, reverted);
                return false;
            }

            configs = next;
            RebuildRelays();
            AddNotice(existing == null
                ? new AppNotice($"已添加「{config.Name}」", $"已获取 {Int(saved, "model_count")} 个模型。{RestartHint}", AppSeverity.Success)
                : new AppNotice($"已保存「{config.Name}」", RestartHint, AppSeverity.Success));
            return true;
        }
        finally
        {
            editor.IsSaving = false;
            IsBusy = false;
        }
    }

    public async Task PerformDeleteRelayAsync(string id)
    {
        if (busy || configs.Count <= 1) return;
        var config = configs.FirstOrDefault(c => c.Id == id);
        if (config == null) return;

        IsBusy = true;
        try
        {
            await DeleteConfirmedAsync(config);
        }
        finally
        {
            IsBusy = false;
        }
    }

    async Task DeleteConfirmedAsync(RelayConfig config)
    {
        DeletingRelayId = config.Id;
        try
        {
            var failedTitle = $"没有删除「{config.Name}」";
            var next = configs.Where(c => c.Id != config.Id).ToList();
            try
            {
                settings.SaveRelays(next);
            }
            catch (Exception ex) when (IsSystemFault(ex))
            {
                AddNotice(new AppNotice(failedTitle, "无法写入设置文件。"));
                return;
            }

            try
            {
                ApplyStatus(await core.CallAsync("delete_relay", Params("id", config.Id)));
            }
            catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
            {
                var detail = Describe(ex);
                try { settings.SaveRelays(configs); }
                catch (Exception inner) when (IsSystemFault(inner))
                {
                    detail += "\n本机设置没能恢复，重启助手后这个配置会消失，需要重新添加。";
                }
                AddNotice(new AppNotice(failedTitle, detail));
                return;
            }

            configs = next;
            var keyDeleted = true;
            try { vault.Delete(config.Id); }
            catch (Exception ex) when (IsSystemFault(ex)) { keyDeleted = false; }
            RebuildRelays();
            AddNotice(keyDeleted
                ? new AppNotice($"已删除「{config.Name}」", RestartHint, AppSeverity.Success)
                : new AppNotice(
                    $"已删除「{config.Name}」",
                    RestartHint + "\n但没能从系统凭据管理器里删除它的 API Key。",
                    AppSeverity.Warning));
        }
        finally
        {
            DeletingRelayId = null;
        }
    }

    public async Task RetryRelayAsync(string id)
    {
        if (!fetching.Add(id)) return;
        RebuildRelays();
        try
        {
            ApplyStatus(await core.CallAsync("refresh_models", Params("id", id)));
            RemoveNotices(RefreshFailed);
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            AddNotice(new AppNotice(RefreshFailed, Describe(ex)));
        }
        finally
        {
            fetching.Remove(id);
            RebuildRelays();
        }
    }

    public async Task SetLoginStartAsync(bool enable)
    {
        if (!enable && forwardingEnabled)
        {
            var request = new ConfirmRequest(
                "关闭「登录时启动」？",
                "关闭后，下次登录电脑时助手不会自动运行。在你手动打开助手之前，Codex 里的官方模型和中转模型都无法使用，因为转发仍然开着。如果以后不打算常驻，建议先关闭转发。",
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

    public bool TryBeginQuit()
    {
        if (!TryHoldBusy()) return false;
        quitRestore = coreForwarding;
        LastError = null;
        return true;
    }

    public void ReleaseQuit() => IsBusy = false;

    public ConfirmRequest? QuitConfirmRequest() =>
        forwardingEnabled
            ? new ConfirmRequest("退出模型接入助手？", QuitBase + (chatGpt.IsRunning() ? QuitRunningTail : ""), "还原并退出")
            : null;

    public async Task QuitAsync()
    {
        if (!TryBeginQuit()) return;
        try
        {
            await RunHeldQuitAsync();
        }
        finally
        {
            ReleaseQuit();
        }
    }

    public async Task<bool> RunHeldQuitAsync()
    {
        try
        {
            await core.CallAsync("shutdown", Params("restore", quitRestore));
        }
        catch (CoreCallException ex) when (ex.Code == "restore_blocked")
        {
            LastError = new CoreErrorInfo(ex.Code, ex.Keys);
            await StopCoreAsync();
            ShowMainWindowRequested?.Invoke();
            return false;
        }
        catch (CoreCallException ex)
        {
            AddNotice(new AppNotice("没能退出", Describe(ex)));
            ShowMainWindowRequested?.Invoke();
            return false;
        }
        catch (CoreProcessExitedException)
        {
        }

        await StopCoreAsync();
        ExitRequested?.Invoke();
        return true;
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
            return settings.ReadRelays().Count == 0 && !settings.HasBackup() && !vault.Any() && !login.IsEnabled();
        }
        catch (Exception ex) when (IsSystemFault(ex) || ex is SettingsUnreadableException)
        {
            return false;
        }
    }

    public ConfirmRequest UninstallConfirmRequest()
    {
        var tail = chatGpt.IsRunning() ? "ChatGPT 正在运行，卸载后请重启 ChatGPT。" : "";
        return new ConfirmRequest(
            "卸载模型接入助手？",
            "卸载会：\n· 关闭转发，把 Codex 配置还原成安装前的样子\n· 从系统凭据管理器删除中转 API Key，删除本助手保存的 Base URL\n· 取消「登录时启动」，删除本助手\n\n不会删除 ChatGPT、聊天记录，以及你自己的其他 Codex 设置。"
            + (tail == "" ? "" : "\n" + tail),
            "卸载");
    }

    public const int UninstallCoreNotStarted = 3;

    bool TryHoldBusy()
    {
        if (RejectWhileConnecting() || busy) return false;
        IsBusy = true;
        return true;
    }

    public bool TryBeginUninstall() => TryHoldBusy();

    public void ReleaseUninstall() => IsBusy = false;

    bool RejectWhileConnecting()
    {
        if (!isConnecting) return false;
        ShowMainWindowRequested?.Invoke();
        return true;
    }

    public async Task<int> RunConfirmedUninstallAsync(bool startCore)
    {
        if (!TryBeginUninstall()) return 1;
        try { return await RunHeldUninstallAsync(startCore); }
        finally { ReleaseUninstall(); }
    }

    public async Task<int> RunHeldUninstallAsync(bool startCore)
    {
        if (startCore)
        {
            await StartAsync();
            if (!CoreRunning) return UninstallCoreNotStarted;
        }

        UninstallPause = null;
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
            ShowMainWindowRequested?.Invoke();
            return 1;
        }

        var leftovers = new List<string>();
        try { vault.DeleteAll(); }
        catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("系统凭据管理器里的 API Key 没能删除"); }
        try { settings.DeleteAll(); }
        catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("本助手保存的设置没能删除"); }
        try { login.Disable(); }
        catch (Exception ex) when (IsSystemFault(ex)) { leftovers.Add("登录时启动项没能取消"); }

        await StopCoreAsync();
        Uninstalled = new UninstallOutcome(leftovers, chatGpt.IsRunning());
        return 0;
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
                UpdateIsInstaller = Flag(result, "installer", false);
            }
            else
            {
                UpdateVersion = null;
                UpdateUrl = null;
                UpdateIsInstaller = false;
                AddNotice(new AppNotice("已是最新版本", "", AppSeverity.Success));
            }
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            AddNotice(new AppNotice(UpdateUnavailable, "", AppSeverity.Warning));
        }
    }

    public Task RetryAsync() => RefreshAsync("refresh_models", Params("id", null));

    public Task RecheckAsync()
    {
        LastError = null;
        return RecheckStatusAsync();
    }

    async Task RecheckStatusAsync()
    {
        if (busy) return;
        IsRechecking = true;
        try { await RefreshAsync("status", null); }
        finally { IsRechecking = false; }
    }

    async Task RefreshAsync(string method, IReadOnlyDictionary<string, object?>? parameters)
    {
        if (busy) return;
        IsBusy = true;
        JsonElement? result = null;
        try
        {
            result = await core.CallAsync(method, parameters);
            ApplyStatus(result.Value);
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
        if (result is { } status) OnStatusReceived(status);
    }

    string? Persist(RelayConfig config, string key, string? oldKey, IReadOnlyList<RelayConfig> next)
    {
        try
        {
            vault.Save(config.Id, key);
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            return CredentialWriteFailed;
        }

        try
        {
            settings.SaveRelays(next);
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            try
            {
                if (oldKey != null) vault.Save(config.Id, oldKey);
                else vault.Delete(config.Id);
            }
            catch (Exception inner) when (IsSystemFault(inner)) { }
            return SettingsWriteFailed;
        }

        return null;
    }

    async Task<bool> RollbackAddAsync(string id)
    {
        try
        {
            if (configs.Count > 0)
                await core.CallAsync("delete_relay", Params("id", id));
            else
                await core.CallAsync("init", new Dictionary<string, object?>
                {
                    ["relays"] = new List<Dictionary<string, object?>>(),
                    ["chatgpt_dir"] = chatGpt.Locate(),
                });
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            return false;
        }
        return true;
    }

    async Task<bool> RollbackEditAsync(RelayConfig old, string? oldKey)
    {
        if (oldKey == null) return false;
        try
        {
            await core.CallAsync("save_relay", RelayParams(old.Id, old.Name, old.BaseUrl, oldKey));
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException)
        {
            return false;
        }
        return true;
    }

    IReadOnlyList<RelayConfig> TryReadRelays()
    {
        try
        {
            return settings.ReadRelays();
        }
        catch (SettingsUnreadableException ex)
        {
            WarnSettingsUnreadable(ex.FilePath);
            return [];
        }
        catch (Exception ex) when (IsSystemFault(ex))
        {
            return [];
        }
    }

    void WarnSettingsUnreadable(string filePath)
    {
        const string reason = "保存中转名字和地址的设置文件读不出来，可能是文件坏了、正被别的程序占用，或者格式认不出来。";
        const string noKey = "这个文件里只有中转的名字和地址，没有 API Key。";
        var backup = settings.TryBackUp();
        AddNotice(backup != null
            ? new AppNotice(SettingsBackedUp, string.Join("\n\n", reason, "原文件已经改名备份到：", backup, noKey + "重新填写并完成设置后就能照常使用。"), AppSeverity.Warning) { DetailInDialog = true }
            : new AppNotice(SettingsUnreadable, string.Join("\n\n", reason, "没能备份这个文件，它还在原处：", filePath, "完成设置后，它会被新的设置覆盖。想留着原文件的话，请先自己复制一份。", noKey), AppSeverity.Warning) { DetailInDialog = true });
    }

    internal static (string? Name, string? BaseUrl, string? ApiKey) ValidateRelay(
        string name, string url, string key, IEnumerable<RelayConfig> others)
    {
        string? nameError = null;
        if (name.Length == 0) nameError = "请填写名字。";
        else if (name.EnumerateRunes().Count() > MaxNameLength)
            nameError = $"名字最多 {MaxNameLength} 个字，现在是 {name.EnumerateRunes().Count()} 个。";
        else if (name.Contains('·')) nameError = Reason("name_has_dot");
        else if (others.FirstOrDefault(c => string.Equals(c.Name, name, StringComparison.OrdinalIgnoreCase)) is { } same)
            nameError = Reason("name_duplicate", [same.Name]);

        var urlError = url.Length == 0
            ? "请填写 Base URL。"
            : url.StartsWith("https://", StringComparison.OrdinalIgnoreCase) ? null : Reason("invalid_base_url");
        return (nameError, urlError, key.Length == 0 ? "请填写 API Key。" : null);
    }

    static DialogAlert SaveFailedAlert(string reason, bool reverted = true) =>
        new(
            NoSave,
            reason.TrimEnd('。') + "。已填的内容都还在，改好后再保存。"
                + (reverted ? "" : "\n已在本次运行中生效，但没能保存到本机，重启助手后会恢复成原来的样子。"),
            DialogAlertSeverity.Danger);

    bool ValidateSetup()
    {
        (NameError, BaseUrlError, ApiKeyError) = ValidateRelay(NameInput.Trim(), BaseUrlInput.Trim(), ApiKeyInput.Trim(), configs);
        return NameError == null && BaseUrlError == null && ApiKeyError == null;
    }

    void OnCoreFailed(string reason)
    {
        CoreRunning = false;
        AddNotice(new AppNotice(CoreStartFailed, reason));
    }

    void ApplyStatus(JsonElement s)
    {
        if (s.ValueKind != JsonValueKind.Object) return;

        var proxy = Prop(s, "proxy");
        var chatgpt = Prop(s, "chatgpt");
        var update = Prop(s, "update");

        var found = Flag(chatgpt, "found", true);
        var unsupported = Str(chatgpt, "error") == "chatgpt_unsupported";
        coreForwarding = Flag(s, "forwarding", false);
        ForwardingEnabled = ForwardingUsable(s);
        ChatGptFound = found;
        ChatGptUnsupported = unsupported;
        Version = Str(s, "version") is { } v ? "v" + v : "";
        ConfigPath = Str(s, "config_path") ?? "";
        relayStatus = Prop(s, "relays").ValueKind == JsonValueKind.Array
            ? Prop(s, "relays").EnumerateArray()
                .Where(r => Str(r, "id") != null)
                .ToDictionary(r => Str(r, "id")!, r => new RelayStatus(Int(r, "model_count"), Str(r, "fetched_at"), Flag(r, "stale", false)))
            : [];
        defaultRelay = Str(s, "default_relay");
        var access = Prop(s, "access");
        accessMode = Str(access, "mode");
        accessDetect = Str(access, "detect");
        Raise(nameof(AccessModeName));
        Raise(nameof(AccessReasonText));
        Raise(nameof(OverviewTitle));
        Raise(nameof(OverviewText));

        if (Str(update, "latest_version") is { } latest)
        {
            UpdateVersion = latest;
            UpdateUrl = Str(update, "url");
            UpdateIsInstaller = Flag(update, "installer", false);
        }

        var list = new List<AppBanner>();
        if (Strings(s, "taken_over").Count > 0)
            list.Add(new AppBanner(
                "已接管 Codex 原有的接入设置",
                "开启转发前，Codex 配置里已有别的「官方服务地址」设置。原值已备份，关闭转发或卸载时会还原。",
                AppSeverity.Info,
                []));

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

        if (chatGptUnsupported)
            list.Add(new AppBanner(
                "当前 ChatGPT 版本暂不支持",
                "读不到 ChatGPT 自带的官方模型清单，转发保持关闭。请把 ChatGPT 更新到最新版后点「重新检测」；还是不行的话，请等待助手新版本。",
                AppSeverity.Danger,
                [BannerAction.Redetect]));

        statusBanners = list;
        RebuildBanners();
        RebuildRelays();
    }

    void RebuildRelays()
    {
        Relays = configs.Select(Row).ToList();
        Raise(nameof(RelayCountText));
        Raise(nameof(CanAddRelay));
        Raise(nameof(RelayMinimumText));
        Raise(nameof(RelayModelSummary));
    }

    RelayRow Row(RelayConfig config)
    {
        var status = relayStatus.GetValueOrDefault(config.Id);
        var state = fetching.Contains(config.Id) ? RelayRowState.Fetching
            : !forwardingEnabled ? RelayRowState.ForwardingOff
            : status?.FetchedAt == null ? RelayRowState.NeverFetched
            : status.Stale ? RelayRowState.Stale
            : RelayRowState.Normal;
        var count = status?.ModelCount ?? 0;
        var text = state switch
        {
            RelayRowState.Fetching => "正在获取…",
            RelayRowState.ForwardingOff => "转发关闭时不获取",
            RelayRowState.NeverFetched => "还没获取到模型",
            RelayRowState.Stale => $"没更新，沿用上次的 {count} 个",
            _ => $"{count} 个模型",
        };
        var idle = state != RelayRowState.Fetching && !busy;
        return new RelayRow(
            config.Id, config.Name, config.BaseUrl, state, text,
            state is RelayRowState.Stale or RelayRowState.NeverFetched,
            idle,
            idle && configs.Count > 1);
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

    static string Reason(string code, IReadOnlyList<string>? keys = null) =>
        (CoreErrorMessages.Messages.TryGetValue(code, out var message) ? message : CoreErrorMessages.Messages["internal_error"])
            .Replace("<keys[0]>", keys?.FirstOrDefault() ?? "");

    static string Describe(Exception ex) => ex is CoreCallException call ? Reason(call.Code, call.Keys) : CoreStopped;

    static bool IsSystemFault(Exception ex) =>
        ex is IOException or UnauthorizedAccessException or SecurityException or System.ComponentModel.Win32Exception;

    static Dictionary<string, object?> Params(string name, object? value) => new() { [name] = value };

    static Dictionary<string, object?> RelayParams(string? id, string name, string url, string key) =>
        new() { ["id"] = id, ["name"] = name, ["base_url"] = url, ["api_key"] = key };

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
