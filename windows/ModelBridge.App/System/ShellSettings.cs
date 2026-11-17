using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Runtime.ExceptionServices;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace ModelBridge.Services;

public sealed record RelayConfig(string Id, string Name, string BaseUrl);

public sealed class SettingsUnreadableException(string filePath, Exception inner)
    : Exception($"设置文件读不出来：{filePath}", inner)
{
    public string FilePath { get; } = filePath;
}

public sealed class ShellSettings(string directory)
{
    public string FilePath { get; } = Path.Combine(directory, "shell-settings.json");

    public IReadOnlyList<RelayConfig> ReadRelays()
    {
        if (!File.Exists(FilePath)) return [];
        try
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(FilePath));
            var root = doc.RootElement;
            if (root.ValueKind != JsonValueKind.Object) throw new JsonException("根节点不是对象");

            if (root.TryGetProperty("relays", out var array))
            {
                if (array.ValueKind != JsonValueKind.Array) throw new JsonException("relays 不是数组");
                return array.EnumerateArray().Select(Parse).ToList();
            }

            if (root.TryGetProperty("base_url", out var legacy) && legacy.ValueKind == JsonValueKind.String && legacy.GetString() is { } url)
            {
                IReadOnlyList<RelayConfig> migrated = [new RelayConfig("1", "中转", url)];
                try
                {
                    SaveRelays(migrated);
                }
                catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
                {
                }
                return migrated;
            }
            throw new JsonException("缺少 relays 和 base_url");
        }
        catch (Exception ex) when (ex is JsonException or IOException or UnauthorizedAccessException)
        {
            throw new SettingsUnreadableException(FilePath, ex);
        }
    }

    string BackupPath => FilePath + ".bak";

    public string? TryBackUp()
    {
        try
        {
            File.Move(FilePath, BackupPath, overwrite: true);
            return BackupPath;
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
        {
            return null;
        }
    }

    public void SaveRelays(IReadOnlyList<RelayConfig> relays)
    {
        Directory.CreateDirectory(directory);
        var array = new JsonArray();
        foreach (var r in relays)
            array.Add(new JsonObject { ["id"] = r.Id, ["name"] = r.Name, ["base_url"] = r.BaseUrl });
        var temp = Path.Combine(directory, "shell-settings." + Guid.NewGuid().ToString("N") + ".tmp");
        try
        {
            File.WriteAllText(temp, new JsonObject { ["relays"] = array }.ToJsonString());
            if (File.Exists(FilePath)) File.Replace(temp, FilePath, null);
            else File.Move(temp, FilePath);
        }
        finally
        {
            if (File.Exists(temp)) File.Delete(temp);
        }
    }

    public void DeleteAll()
    {
        Exception? first = null;
        foreach (var path in new[] { FilePath, BackupPath })
        {
            try
            {
                if (File.Exists(path)) File.Delete(path);
            }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
            {
                first ??= ex;
            }
        }
        if (first != null) ExceptionDispatchInfo.Capture(first).Throw();
    }

    public bool HasBackup() => File.Exists(BackupPath);

    static RelayConfig Parse(JsonElement e)
    {
        if (e.ValueKind != JsonValueKind.Object) throw new JsonException("条目不是对象");
        return new RelayConfig(
            Text(e, "id") ?? throw new JsonException("缺少 id"),
            Text(e, "name") ?? throw new JsonException("缺少 name"),
            Text(e, "base_url") ?? throw new JsonException("缺少 base_url"));
    }

    static string? Text(JsonElement e, string name) =>
        e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;
}
