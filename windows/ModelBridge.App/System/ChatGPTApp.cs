using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
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

    public string? Locate() => new ChatGptLocator(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), RunningPaths).Find();

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
