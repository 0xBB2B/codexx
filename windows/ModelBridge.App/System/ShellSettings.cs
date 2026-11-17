using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace ModelBridge.Services;

public sealed record RelayConfig(string Id, string Name, string BaseUrl);

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
            if (root.ValueKind != JsonValueKind.Object) return [];

            if (root.TryGetProperty("relays", out var array) && array.ValueKind == JsonValueKind.Array)
                return array.EnumerateArray().Select(Parse).OfType<RelayConfig>().ToList();

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
        }
        catch (JsonException)
        {
        }
        return [];
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
        if (File.Exists(FilePath)) File.Delete(FilePath);
    }

    static RelayConfig? Parse(JsonElement e)
    {
        if (e.ValueKind != JsonValueKind.Object) return null;
        var id = Text(e, "id");
        var name = Text(e, "name");
        var url = Text(e, "base_url");
        return id == null || name == null || url == null ? null : new RelayConfig(id, name, url);
    }

    static string? Text(JsonElement e, string name) =>
        e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;
}
