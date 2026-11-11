using System.Collections.Generic;
using System.IO;
using System.Text.Json;

namespace ModelBridge.Services;

public sealed class ShellSettings(string directory)
{
    public string FilePath { get; } = Path.Combine(directory, "shell-settings.json");

    public string? ReadBaseUrl()
    {
        if (!File.Exists(FilePath)) return null;
        try
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(FilePath));
            return doc.RootElement.TryGetProperty("base_url", out var value) && value.ValueKind == JsonValueKind.String
                ? value.GetString()
                : null;
        }
        catch (JsonException)
        {
            return null;
        }
    }

    public void SaveBaseUrl(string baseUrl)
    {
        Directory.CreateDirectory(directory);
        File.WriteAllText(FilePath, JsonSerializer.Serialize(new Dictionary<string, string> { ["base_url"] = baseUrl }));
    }

    public void DeleteAll()
    {
        if (File.Exists(FilePath)) File.Delete(FilePath);
    }
}
