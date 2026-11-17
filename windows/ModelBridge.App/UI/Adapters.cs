using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json;
using System.Threading.Tasks;
using System.Windows.Threading;
using ModelBridge.Services;

namespace ModelBridge.UI;

public sealed class CoreClient : ICoreClient
{
    readonly CoreProcess process;
    CoreInitParams init = new([], null);

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
        if (parameters == null) return result;

        if (method == "save_relay" && result.TryGetProperty("id", out var id) && id.GetString() is { } relayId)
        {
            var relay = new RelayInit(
                relayId, (string)parameters["name"]!, (string)parameters["base_url"]!, (string)parameters["api_key"]!);
            var relays = init.Relays.ToList();
            var index = relays.FindIndex(r => r.Id == relayId);
            if (index >= 0) relays[index] = relay; else relays.Add(relay);
            init = init with { Relays = relays };
        }
        else if (method == "delete_relay")
            init = init with { Relays = init.Relays.Where(r => r.Id != (string?)parameters["id"]).ToList() };
        else if (method == "init")
            init = init with { Relays = [], ChatGptDir = parameters["chatgpt_dir"] as string };
        return result;
    }
}

public sealed class VaultAdapter : ICredentialVault
{
    public void Save(string id, string key) => new CredentialStore(CredentialStore.TargetNameFor(id)).Save(key);
    public string? Read(string id) => new CredentialStore(CredentialStore.TargetNameFor(id)).Read();
    public void Delete(string id) => new CredentialStore(CredentialStore.TargetNameFor(id)).Delete();
    public void DeleteAll() => CredentialStore.DeleteByPrefix(CredentialStore.TargetPrefix);
    public bool Any() => CredentialStore.ExistsByPrefix(CredentialStore.TargetPrefix);
}

public sealed class SettingsAdapter(ShellSettings settings) : IShellSettingsStore
{
    public IReadOnlyList<RelayConfig> ReadRelays() => settings.ReadRelays();
    public void SaveRelays(IReadOnlyList<RelayConfig> relays) => settings.SaveRelays(relays);
    public void DeleteAll() => settings.DeleteAll();
}

public sealed class LoginAdapter(LoginItem item) : ILoginStart
{
    public bool IsEnabled() => item.IsEnabled();
    public void Enable() => item.Enable();
    public void Disable() => item.Disable();
}

public sealed class ChatGptAdapter(ChatGptApp app) : IChatGptControl
{
    public string? Locate() => app.Locate();
    public bool IsRunning() => app.IsRunning();
}
