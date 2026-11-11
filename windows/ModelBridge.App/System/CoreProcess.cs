using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.ComponentModel;
using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace ModelBridge.Services;

public sealed class CoreProcessOptions(string executablePath, IReadOnlyList<string> arguments)
{
    public string ExecutablePath { get; } = executablePath;
    public IReadOnlyList<string> Arguments { get; } = arguments;
    public TimeSpan RestartDelay { get; init; } = TimeSpan.FromSeconds(2);
    public TimeSpan StableAfter { get; init; } = TimeSpan.FromSeconds(60);

    public static CoreProcessOptions ForInstall(string installDir, string localAppData) =>
        new(Path.Combine(installDir, "model-bridge-core.exe"),
            ["--data-dir", Path.Combine(localAppData, "ModelBridge")]);
}

public sealed record CoreInitParams(string? BaseUrl, string? ApiKey, string? ChatGptDir);

public sealed class CoreCallException(string code, IReadOnlyList<string> keys) : Exception($"核心返回错误：{code}")
{
    public string Code { get; } = code;
    public IReadOnlyList<string> Keys { get; } = keys;
}

public sealed class CoreProcessExitedException() : Exception("核心进程已退出");

public sealed class CoreProcess(CoreProcessOptions options, Func<CoreInitParams> initParams)
{
    readonly object gate = new();
    readonly object writeGate = new();
    readonly Dictionary<long, TaskCompletionSource<JsonElement>> pending = [];
    Process? process;
    int generation;
    long nextId;
    bool stopped;
    bool hasRestarted;
    volatile bool failed;

    public event Action<JsonElement>? StatusReceived;
    public event Action<string>? Failed;

    public bool HasFailed => failed;

    public static ProcessStartInfo CreateStartInfo(CoreProcessOptions options)
    {
        var info = new ProcessStartInfo(options.ExecutablePath)
        {
            CreateNoWindow = true,
            UseShellExecute = false,
            RedirectStandardInput = true,
            RedirectStandardOutput = true,
            StandardInputEncoding = new UTF8Encoding(false),
            StandardOutputEncoding = new UTF8Encoding(false),
        };
        foreach (var argument in options.Arguments) info.ArgumentList.Add(argument);
        return info;
    }

    public async Task StartAsync()
    {
        Launch(null);
        await InitAsync();
    }

    public Task StopAsync() => StopCoreAsync(false, "");

    public async Task<JsonElement> CallAsync(string method, IReadOnlyDictionary<string, object?>? parameters = null)
    {
        var tcs = new TaskCompletionSource<JsonElement>(TaskCreationOptions.RunContinuationsAsynchronously);
        Process p;
        long id;
        lock (gate)
        {
            if (stopped || process == null) throw new CoreProcessExitedException();
            p = process;
            id = ++nextId;
            pending[id] = tcs;
        }

        var line = JsonSerializer.Serialize(new Dictionary<string, object?>
        {
            ["id"] = id,
            ["method"] = method,
            ["params"] = parameters ?? new Dictionary<string, object?>(),
        }) + "\n";
        try
        {
            lock (writeGate) p.StandardInput.Write(line);
        }
        catch (Exception ex) when (ex is IOException or ObjectDisposedException or InvalidOperationException)
        {
            lock (gate) FailPending();
            throw new CoreProcessExitedException();
        }
        return await tcs.Task;
    }

    int Launch(int? expectedGeneration)
    {
        var p = new Process { StartInfo = CreateStartInfo(options) };
        int gen;
        lock (gate)
        {
            if (stopped || (expectedGeneration is { } e && generation != e)) return 0;
            p.Start();
            p.StandardInput.AutoFlush = true;
            process = p;
            gen = ++generation;
        }
        _ = Task.Run(() => ReadLoopAsync(p, gen));
        return gen;
    }

    Task<JsonElement> InitAsync()
    {
        var init = initParams();
        return CallAsync("init", new Dictionary<string, object?>
        {
            ["base_url"] = init.BaseUrl,
            ["api_key"] = init.ApiKey,
            ["chatgpt_dir"] = init.ChatGptDir,
        });
    }

    async Task ReadLoopAsync(Process p, int gen)
    {
        var buffer = new char[4096];
        var line = new StringBuilder();
        try
        {
            int count;
            while ((count = await p.StandardOutput.ReadAsync(buffer, 0, buffer.Length)) > 0)
            {
                for (var i = 0; i < count; i++)
                {
                    if (buffer[i] == '\n')
                    {
                        DispatchSafely(line.ToString());
                        line.Clear();
                    }
                    else line.Append(buffer[i]);
                }
            }
        }
        catch (Exception ex) when (ex is IOException or ObjectDisposedException or InvalidOperationException)
        {
        }
        await OnExitedAsync(p, gen);
    }

    void DispatchSafely(string line)
    {
        try { Dispatch(line); }
        catch (Exception) { }
    }

    void Dispatch(string line)
    {
        JsonDocument doc;
        try { doc = JsonDocument.Parse(line); }
        catch (JsonException) { return; }

        using (doc)
        {
            var root = doc.RootElement;
            if (root.TryGetProperty("event", out var ev))
            {
                if (ev.GetString() == "status" && root.TryGetProperty("data", out var data))
                    StatusReceived?.Invoke(data.Clone());
                return;
            }
            if (!root.TryGetProperty("id", out var idElement) || !idElement.TryGetInt64(out var id)) return;

            TaskCompletionSource<JsonElement>? tcs;
            lock (gate)
            {
                if (!pending.Remove(id, out tcs)) return;
            }
            if (root.TryGetProperty("error", out var error))
                tcs.TrySetException(ParseError(error));
            else
                tcs.TrySetResult(root.TryGetProperty("result", out var result) ? result.Clone() : default);
        }
    }

    static CoreCallException ParseError(JsonElement error)
    {
        try
        {
            var keys = error.TryGetProperty("keys", out var keyArray)
                ? keyArray.EnumerateArray().Select(k => k.GetString() ?? "").ToList()
                : [];
            return new CoreCallException(error.GetProperty("code").GetString() ?? "", keys);
        }
        catch (Exception ex) when (ex is KeyNotFoundException or InvalidOperationException)
        {
            return new CoreCallException("bad_response", []);
        }
    }

    async Task OnExitedAsync(Process p, int gen)
    {
        bool giveUp;
        lock (gate)
        {
            if (gen != generation || stopped) return;
            process = null;
            FailPending();
            giveUp = hasRestarted;
            hasRestarted = true;
        }
        KillAndDispose(p);

        if (giveUp) await StopCoreAsync(true, "核心进程反复崩溃");
        else await RestartAsync(gen);
    }

    static void KillAndDispose(Process p)
    {
        try
        {
            if (!p.HasExited) p.Kill(true);
        }
        catch (Exception ex) when (ex is InvalidOperationException or Win32Exception)
        {
        }
        finally
        {
            p.Dispose();
        }
    }

    async Task RestartAsync(int crashedGeneration)
    {
        await Task.Delay(options.RestartDelay);
        var newGeneration = 0;
        try
        {
            newGeneration = Launch(crashedGeneration);
            if (newGeneration == 0) return;
            await InitAsync();
        }
        catch (Exception ex) when (ex is CoreCallException or CoreProcessExitedException or Win32Exception)
        {
            lock (gate)
            {
                if (generation != (newGeneration != 0 ? newGeneration : crashedGeneration)) return;
            }
            await StopCoreAsync(true, ex.Message);
            return;
        }
        _ = ResetRestartedAfterStableAsync(newGeneration);
    }

    async Task ResetRestartedAfterStableAsync(int gen)
    {
        await Task.Delay(options.StableAfter);
        lock (gate)
        {
            if (generation == gen && !stopped) hasRestarted = false;
        }
    }

    async Task StopCoreAsync(bool asFailure, string reason)
    {
        Process? p;
        lock (gate)
        {
            if (stopped) return;
            stopped = true;
            generation++;
            p = process;
            process = null;
            FailPending();
            if (asFailure) failed = true;
        }
        await CloseAsync(p);
        if (asFailure) Failed?.Invoke(reason);
    }

    static async Task CloseAsync(Process? p)
    {
        if (p == null) return;
        try
        {
            p.StandardInput.Close();
            using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(3));
            try { await p.WaitForExitAsync(cts.Token); }
            catch (OperationCanceledException) { p.Kill(true); }
        }
        catch (Exception ex) when (ex is IOException or InvalidOperationException)
        {
        }
        finally
        {
            p.Dispose();
        }
    }

    void FailPending()
    {
        foreach (var tcs in pending.Values) tcs.TrySetException(new CoreProcessExitedException());
        pending.Clear();
    }
}
