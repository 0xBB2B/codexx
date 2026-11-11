using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.ComponentModel;
using System.Diagnostics;

namespace ModelBridge.Services;

public sealed class ChatGptLocator(string localAppData, Func<IEnumerable<string>> runningPaths)
{
    static readonly string[][] InstallDirs =
    [
        ["Programs", "ChatGPT"],
        ["Programs", "OpenAI ChatGPT"],
        ["ChatGPT"],
        ["OpenAI", "ChatGPT"],
        ["Programs", "Codex"],
        ["OpenAI Codex"],
    ];

    static readonly string[] ExeNames = ["ChatGPT.exe", "Codex.exe"];

    public string? Find()
    {
        foreach (var path in runningPaths())
            if (File.Exists(path)) return path;

        foreach (var parts in InstallDirs)
        {
            var dir = Path.Combine([localAppData, .. parts]);
            if (!Directory.Exists(dir)) continue;
            var searchDirs = new[] { dir }.Concat(
                Directory.EnumerateDirectories(dir, "app-*").OrderByDescending(d => d, StringComparer.Ordinal));
            foreach (var searchDir in searchDirs)
                foreach (var exe in ExeNames)
                {
                    var candidate = Path.Combine(searchDir, exe);
                    if (File.Exists(candidate)) return candidate;
                }
        }
        return null;
    }
}

public interface IChatGptApp
{
    string? Locate();
    bool IsRunning();
    Task<bool> RequestCloseAndWaitAsync(TimeSpan timeout, CancellationToken ct);
    void Launch(string exePath);
}

public interface IChatGptProcess : IDisposable
{
    int SessionId { get; }
    string? ExecutablePath { get; }
    void CloseMainWindow();
    Task WaitForExitAsync(CancellationToken ct);
}

sealed class SystemChatGptProcess(Process process) : IChatGptProcess
{
    public int SessionId => process.SessionId;

    public string? ExecutablePath
    {
        get
        {
            try { return process.MainModule?.FileName; }
            catch (Exception ex) when (ex is Win32Exception or InvalidOperationException) { return null; }
        }
    }

    public void CloseMainWindow() => process.CloseMainWindow();

    public Task WaitForExitAsync(CancellationToken ct) => process.WaitForExitAsync(ct);

    public void Dispose() => process.Dispose();
}

public sealed class ChatGptApp : IChatGptApp
{
    static readonly string[] ProcessNames = ["ChatGPT", "Codex"];

    readonly Func<IEnumerable<IChatGptProcess>> processSource;
    readonly int sessionId;

    public ChatGptApp() : this(SystemProcesses, CurrentSessionId())
    {
    }

    public ChatGptApp(Func<IEnumerable<IChatGptProcess>> processSource, int sessionId)
    {
        this.processSource = processSource;
        this.sessionId = sessionId;
    }

    public string? Locate() => new ChatGptLocator(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), RunningPaths).Find();

    public bool IsRunning()
    {
        var processes = FindProcesses();
        DisposeAll(processes);
        return processes.Count > 0;
    }

    public async Task<bool> RequestCloseAndWaitAsync(TimeSpan timeout, CancellationToken ct)
    {
        var processes = FindProcesses();
        try
        {
            foreach (var p in processes) p.CloseMainWindow();
            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            timeoutCts.CancelAfter(timeout);
            try
            {
                await Task.WhenAll(processes.Select(p => p.WaitForExitAsync(timeoutCts.Token)));
                return true;
            }
            catch (OperationCanceledException) when (!ct.IsCancellationRequested)
            {
                return false;
            }
        }
        finally
        {
            DisposeAll(processes);
        }
    }

    public void Launch(string exePath) =>
        Process.Start(new ProcessStartInfo(exePath) { UseShellExecute = true })?.Dispose();

    static int CurrentSessionId()
    {
        using var current = Process.GetCurrentProcess();
        return current.SessionId;
    }

    static IEnumerable<IChatGptProcess> SystemProcesses() =>
        ProcessNames.SelectMany(Process.GetProcessesByName).Select(p => new SystemChatGptProcess(p));

    List<IChatGptProcess> FindProcesses()
    {
        var mine = new List<IChatGptProcess>();
        foreach (var p in processSource())
        {
            if (p.SessionId == sessionId) mine.Add(p);
            else p.Dispose();
        }
        return mine;
    }

    List<string> RunningPaths()
    {
        var processes = FindProcesses();
        var paths = processes.Select(p => p.ExecutablePath).OfType<string>().ToList();
        DisposeAll(processes);
        return paths;
    }

    static void DisposeAll(List<IChatGptProcess> processes)
    {
        foreach (var p in processes) p.Dispose();
    }
}

public enum ChatGptRestartResult
{
    Restarted,
    NotFound,
    CloseTimedOut,
    OpenFailed,
    Cancelled,
}

public sealed class ChatGptRestarter(IChatGptApp app, TimeSpan closeTimeout)
{
    public static readonly TimeSpan DefaultCloseTimeout = TimeSpan.FromSeconds(30);

    public ChatGptRestarter(IChatGptApp app) : this(app, DefaultCloseTimeout)
    {
    }

    public async Task<ChatGptRestartResult> RestartAsync(CancellationToken ct)
    {
        if (ct.IsCancellationRequested) return ChatGptRestartResult.Cancelled;
        var path = app.Locate();
        if (path == null) return ChatGptRestartResult.NotFound;

        if (app.IsRunning())
        {
            bool closed;
            try { closed = await app.RequestCloseAndWaitAsync(closeTimeout, ct); }
            catch (OperationCanceledException) { return ChatGptRestartResult.Cancelled; }
            catch (Exception ex) when (ex is Win32Exception or InvalidOperationException) { closed = false; }
            if (!closed) return ChatGptRestartResult.CloseTimedOut;
        }

        try
        {
            app.Launch(path);
            return ChatGptRestartResult.Restarted;
        }
        catch (Exception ex) when (ex is Win32Exception or InvalidOperationException)
        {
            return ChatGptRestartResult.OpenFailed;
        }
    }
}
