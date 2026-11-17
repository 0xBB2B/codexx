using System.Text.Json;
using System.Windows.Threading;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;
using ModelBridge.UI;

namespace ModelBridge.Tests;

[TestClass]
public class CoreClientTests
{
    string stateDir = "";
    CoreClient? client;

    static string FakeCorePath => Path.Combine(AppContext.BaseDirectory, "ModelBridge.FakeCore.exe");

    [TestInitialize]
    public void Setup()
    {
        var root = Path.Combine(Path.GetTempPath(), "mb-client-" + Guid.NewGuid().ToString("N"));
        stateDir = Path.Combine(root, "state");
        Directory.CreateDirectory(stateDir);
    }

    [TestCleanup]
    public async Task Cleanup()
    {
        if (client != null) await client.StopAsync();
        try { Directory.Delete(Path.GetDirectoryName(stateDir)!, true); } catch (IOException) { }
    }

    async Task StartAsync(params RelayInit[] relays)
    {
        var options = new CoreProcessOptions(FakeCorePath, ["--data-dir", Path.Combine(stateDir, "data"), "--state-dir", stateDir])
        {
            RestartDelay = TimeSpan.FromMilliseconds(100),
        };
        client = new CoreClient(options, Dispatcher.CurrentDispatcher);
        await client.StartAsync(new CoreInitParams(relays, @"C:\Apps\ChatGPT"));
    }

    string[] InitRelays()
    {
        var path = Path.Combine(stateDir, "relays.log");
        if (!File.Exists(path)) return [];
        using var stream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd().Split('\n', StringSplitOptions.RemoveEmptyEntries);
    }

    async Task CrashAndWaitForReinitAsync()
    {
        var before = InitRelays().Length;
        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => client!.CallAsync("crash"));
        var deadline = DateTime.UtcNow.AddSeconds(10);
        while (InitRelays().Length == before)
        {
            if (DateTime.UtcNow > deadline) Assert.Fail("等待重启后重发 init 超时");
            await Task.Delay(20);
        }
    }

    static Dictionary<string, object?> Save(string? id, string name, string url, string key) =>
        new() { ["id"] = id, ["name"] = name, ["base_url"] = url, ["api_key"] = key };

    [TestMethod]
    public async Task Restart_AfterSaveRelayAdd_InitIncludesNewRelayWithKey()
    {
        await StartAsync(new RelayInit("1", "中转", "https://relay.example.com", "sk-example-0001"));
        await client!.CallAsync("save_relay", Save(null, "A家", "https://relay2.example.com", "sk-example-0002"));

        await CrashAndWaitForReinitAsync();

        Assert.AreEqual(
            "count=2;1|中转|https://relay.example.com|sk-example-0001;2|A家|https://relay2.example.com|sk-example-0002",
            InitRelays().Last());
    }

    [TestMethod]
    public async Task Restart_AfterSaveRelayEdit_InitHasLatestValuesInSamePosition()
    {
        await StartAsync(
            new RelayInit("1", "中转", "https://relay.example.com", "sk-example-0001"),
            new RelayInit("2", "A家", "https://relay2.example.com", "sk-example-0002"));
        await client!.CallAsync("save_relay", Save("1", "主线路", "https://new.example.com", "sk-example-9999"));

        await CrashAndWaitForReinitAsync();

        Assert.AreEqual(
            "count=2;1|主线路|https://new.example.com|sk-example-9999;2|A家|https://relay2.example.com|sk-example-0002",
            InitRelays().Last());
    }

    [TestMethod]
    public async Task Restart_AfterDeleteRelay_InitDropsIt()
    {
        await StartAsync(
            new RelayInit("1", "中转", "https://relay.example.com", "sk-example-0001"),
            new RelayInit("2", "A家", "https://relay2.example.com", "sk-example-0002"));
        await client!.CallAsync("delete_relay", new Dictionary<string, object?> { ["id"] = "2" });

        await CrashAndWaitForReinitAsync();

        Assert.AreEqual("count=1;1|中转|https://relay.example.com|sk-example-0001", InitRelays().Last());
    }

    [TestMethod]
    public async Task Restart_AfterInitCallWithEmptyRelays_InitHasNoRelays()
    {
        await StartAsync(new RelayInit("1", "中转", "https://relay.example.com", "sk-example-0001"));
        await client!.CallAsync("init", new Dictionary<string, object?>
        {
            ["relays"] = new List<object>(),
            ["chatgpt_dir"] = @"C:\Apps\ChatGPT",
        });

        await CrashAndWaitForReinitAsync();

        Assert.AreEqual("count=0;", InitRelays().Last());
    }
}
