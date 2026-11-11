using System.Collections;
using System.Text;
using System.Text.Json.Nodes;

var stateDir = ArgValue("--state-dir");
Directory.CreateDirectory(stateDir);
File.WriteAllLines(Path.Combine(stateDir, "args.txt"), args);
File.WriteAllLines(Path.Combine(stateDir, "env.txt"),
    Environment.GetEnvironmentVariables().Cast<DictionaryEntry>().Select(e => $"{e.Key}={e.Value}"));
AppendShared(Path.Combine(stateDir, "launches.log"), "launch\n");

var utf8 = new UTF8Encoding(false);
var stdout = Console.OpenStandardOutput();
var gate = new object();
string? lastKey = null;
string? lastBaseUrl = null;

void Send(string line)
{
    lock (gate)
    {
        var bytes = utf8.GetBytes(line + "\n");
        stdout.Write(bytes, 0, bytes.Length);
        stdout.Flush();
    }
}

void Reply(JsonNode? id, JsonNode result) =>
    Send(new JsonObject { ["id"] = id?.DeepClone(), ["result"] = result }.ToJsonString());

void Fail(JsonNode? id, string code) =>
    Send(new JsonObject
    {
        ["id"] = id?.DeepClone(),
        ["error"] = new JsonObject { ["code"] = code, ["keys"] = new JsonArray() },
    }.ToJsonString());

async Task Handle(JsonNode request)
{
    var id = request["id"];
    var method = request["method"]?.GetValue<string>();
    var p = request["params"];
    switch (method)
    {
        case "init":
            if (File.Exists(Path.Combine(stateDir, "init-fail")))
            {
                Fail(id, "internal_error");
                return;
            }
            lastBaseUrl = p?["base_url"]?.GetValue<string>();
            lastKey = p?["api_key"]?.GetValue<string>();
            AppendShared(Path.Combine(stateDir, "init.log"),
                $"{lastBaseUrl}|{p?["chatgpt_dir"]?.GetValue<string>()}|{lastKey != null}\n");
            Reply(id, new JsonObject { ["version"] = "1.0.0" });
            break;
        case "save_connection":
            lastBaseUrl = p?["base_url"]?.GetValue<string>();
            lastKey = p?["api_key"]?.GetValue<string>();
            Reply(id, new JsonObject { ["models_changed"] = false, ["relay_model_count"] = 0 });
            break;
        case "status":
            Reply(id, new JsonObject
            {
                ["version"] = "1.0.0",
                ["echo_api_key"] = lastKey,
                ["echo_base_url"] = lastBaseUrl,
            });
            break;
        case "echo":
            await Task.Delay(p?["delay_ms"]?.GetValue<int>() ?? 0);
            Reply(id, new JsonObject { ["tag"] = p?["tag"]?.GetValue<string>() });
            break;
        case "emit_status":
            Send(new JsonObject
            {
                ["event"] = "status",
                ["data"] = new JsonObject { ["version"] = "9.9.9" },
            }.ToJsonString());
            Reply(id, new JsonObject());
            break;
        case "raw_lines":
            Send("{\"id\":" + id + ",\"result\":{\"text\":\"a" + (char)0x2028 + "b\"},\r\"extra\":1}");
            break;
        case "bad_error":
            Send("{\"id\":" + id + ",\"error\":{\"keys\":\"not-an-array\"}}");
            break;
        case "crash":
            Environment.Exit(1);
            break;
        case "hang":
            break;
        default:
            Fail(id, "unknown_method");
            break;
    }
}

using var reader = new StreamReader(Console.OpenStandardInput(), utf8);
while (reader.ReadLine() is { } line)
{
    var request = JsonNode.Parse(line)!;
    _ = Task.Run(() => Handle(request));
}
Environment.Exit(0);

void AppendShared(string path, string text)
{
    using var stream = new FileStream(path, FileMode.Append, FileAccess.Write, FileShare.ReadWrite | FileShare.Delete);
    using var writer = new StreamWriter(stream);
    writer.Write(text);
}

string ArgValue(string name)
{
    var i = Array.IndexOf(args, name);
    return args[i + 1];
}
