using System;
using System.Collections.Generic;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Threading;
using ModelBridge.Services;

namespace ModelBridge.UI;

public sealed class CoreClient : ICoreClient
{
    readonly CoreProcess process;
    CoreInitParams init = new(null, null, null);

    public CoreClient(CoreProcessOptions options, Dispatcher ui)
    {
        process = new CoreProcess(options, () => init);
        process.StatusReceived += status => ui.InvokeAsync(() => StatusReceived?.Invoke(status));
        process.Failed += reason => ui.InvokeAsync(() => Failed?.Invoke(reason));
    }

    public event Action<JsonElement>? StatusReceived;
    public event Action<string>? Failed;

    public Task StartAsync(CoreInitParams initParams)
    {
        init = initParams;
        return process.StartAsync();
    }

    public Task StopAsync() => process.StopAsync();

    public async Task<JsonElement> CallAsync(string method, IReadOnlyDictionary<string, object?>? parameters = null)
    {
        var result = await process.CallAsync(method, parameters);
        if (method == "save_connection" && parameters != null)
            init = init with { BaseUrl = parameters["base_url"] as string, ApiKey = parameters["api_key"] as string };
        else if (method == "init" && parameters != null)
            init = new CoreInitParams(
                parameters["base_url"] as string, parameters["api_key"] as string, parameters["chatgpt_dir"] as string);
        return result;
    }
}

public sealed class VaultAdapter(CredentialStore store) : ICredentialVault
{
    public void Save(string key) => store.Save(key);
    public string? Read() => store.Read();
    public void Delete() => store.Delete();
}

public sealed class SettingsAdapter(ShellSettings settings) : IShellSettingsStore
{
    public string? ReadBaseUrl() => settings.ReadBaseUrl();
    public void SaveBaseUrl(string baseUrl) => settings.SaveBaseUrl(baseUrl);
    public void DeleteAll() => settings.DeleteAll();
}

public sealed class LoginAdapter(LoginItem item) : ILoginStart
{
    public bool IsEnabled() => item.IsEnabled();
    public void Enable() => item.Enable();
    public void Disable() => item.Disable();
}

public sealed class ChatGptAdapter(ChatGptApp app, ChatGptRestarter restarter) : IChatGptControl
{
    public string? Locate() => app.Locate();
    public bool IsRunning() => app.IsRunning();
    public Task<ChatGptRestartResult> RestartAsync(CancellationToken ct) => restarter.RestartAsync(ct);
}
