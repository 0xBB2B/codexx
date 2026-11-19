using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.ComponentModel;
using System.Diagnostics;

namespace ModelBridge.Services;

public sealed class ChatGptLocator(
    string localAppData, Func<IEnumerable<string>> runningPaths, Func<IEnumerable<string>> storePackageDirs)
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

    static Version? VersionOf(string dir) =>
        Version.TryParse(Path.GetFileName(dir)["app-".Length..], out var version) ? version : null;

    public string? Find()
    {
        foreach (var path in runningPaths())
            if (File.Exists(path)) return path;

        foreach (var parts in InstallDirs)
        {
            var dir = Path.Combine([localAppData, .. parts]);
            if (!Directory.Exists(dir)) continue;
            var searchDirs = new[] { dir }.Concat(
                Directory.EnumerateDirectories(dir, "app-*")
                    .OrderByDescending(VersionOf)
                    .ThenByDescending(d => d, StringComparer.Ordinal));
            foreach (var searchDir in searchDirs)
                foreach (var exe in ExeNames)
                {
                    var candidate = Path.Combine(searchDir, exe);
                    if (File.Exists(candidate)) return candidate;
                }
        }
        return FindInStorePackages();
    }

    string? FindInStorePackages()
    {
        try
        {
            foreach (var packageDir in storePackageDirs())
            {
                var candidate = Path.Combine(packageDir, "app", "ChatGPT.exe");
                if (File.Exists(candidate)) return candidate;
            }
        }
        catch (Exception)
        {
            // 商店包查询依赖系统接口，失败时当作没装商店版，不影响其他安装位置的结果
        }
        return null;
    }
}

public interface IChatGptProcess : IDisposable
{
    int SessionId { get; }
    string? ExecutablePath { get; }
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

    public void Dispose() => process.Dispose();
}

public sealed class ChatGptApp
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

    public string? Locate()
    {
        var exe = new ChatGptLocator(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), RunningPaths, StorePackageDirs).Find();
        return exe is null ? null : Path.GetDirectoryName(exe);
    }

    static IEnumerable<string> StorePackageDirs() =>
        new Windows.Management.Deployment.PackageManager()
            .FindPackagesForUser(string.Empty, "OpenAI.Codex_2p2nqsd0c76g0")
            .Select(p => p.InstalledLocation.Path);

    public bool IsRunning()
    {
        var processes = FindProcesses();
        DisposeAll(processes);
        return processes.Count > 0;
    }

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
            if (IsInSession(p)) mine.Add(p);
            else p.Dispose();
        }
        return mine;
    }

    bool IsInSession(IChatGptProcess p)
    {
        try { return p.SessionId == sessionId; }
        catch (InvalidOperationException) { return false; }
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
