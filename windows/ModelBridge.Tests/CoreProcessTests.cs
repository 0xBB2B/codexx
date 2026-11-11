using System.Reflection;
using System.Text.Json;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class CoreProcessTests
{
    const string Key = "sk-example-0000";
    const string BaseUrl = "https://relay.example.com";
    const string ChatGptDir = @"C:\Apps\ChatGPT";

    string stateDir = "";
    string dataDir = "";
    CoreProcess? core;

    static string FakeCorePath => Path.Combine(AppContext.BaseDirectory, "ModelBridge.FakeCore.exe");

    [TestInitialize]
    public void Setup()
    {
        var root = Path.Combine(Path.GetTempPath(), "mb-core-" + Guid.NewGuid().ToString("N"));
        stateDir = Path.Combine(root, "state");
        dataDir = Path.Combine(root, "data");
        Directory.CreateDirectory(stateDir);
    }

    [TestCleanup]
    public async Task Cleanup()
    {
        if (core != null) await core.StopAsync();
        try { Directory.Delete(Path.GetDirectoryName(stateDir)!, true); } catch (IOException) { }
    }

    CoreProcessOptions Options(TimeSpan? stableAfter = null) =>
        new(FakeCorePath, ["--data-dir", dataDir, "--state-dir", stateDir])
        {
            RestartDelay = TimeSpan.FromMilliseconds(100),
            StableAfter = stableAfter ?? TimeSpan.FromSeconds(30),
        };

    CoreProcess NewCore(CoreProcessOptions options) =>
        core = new CoreProcess(options, () => new CoreInitParams(BaseUrl, Key, ChatGptDir));

    int Launches() => ReadLines("launches.log").Length;

    string[] ReadLines(string name)
    {
        var path = Path.Combine(stateDir, name);
        if (!File.Exists(path)) return [];
        using var stream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete);
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd().Split('\n', StringSplitOptions.RemoveEmptyEntries);
    }

    static async Task WaitUntil(Func<bool> condition, int timeoutMs = 10000)
    {
        var deadline = DateTime.UtcNow.AddMilliseconds(timeoutMs);
        while (!condition())
        {
            if (DateTime.UtcNow > deadline) Assert.Fail("等待条件超时");
            await Task.Delay(20);
        }
    }

    [TestMethod]
    public void Options_Defaults_RestartDelay2s_StableAfter60s()
    {
        var options = new CoreProcessOptions("core.exe", ["--data-dir", "x"]);

        Assert.AreEqual(TimeSpan.FromSeconds(2), options.RestartDelay);
        Assert.AreEqual(TimeSpan.FromSeconds(60), options.StableAfter);
    }

    [TestMethod]
    public void Options_ForInstall_PointsAtCoreExeInInstallDirWithDataDirArg()
    {
        var options = CoreProcessOptions.ForInstall(@"C:\Apps\ModelBridge", @"C:\Users\u\AppData\Local");

        Assert.AreEqual(@"C:\Apps\ModelBridge\model-bridge-core.exe", options.ExecutablePath);
        CollectionAssert.AreEqual(new[] { "--data-dir", @"C:\Users\u\AppData\Local\ModelBridge" }, options.Arguments.ToArray());
    }

    [TestMethod]
    public void CreateStartInfo_NoWindow_RedirectsStdio_UsesArgumentList()
    {
        var info = CoreProcess.CreateStartInfo(Options());

        Assert.AreEqual(FakeCorePath, info.FileName);
        Assert.IsTrue(info.CreateNoWindow);
        Assert.IsFalse(info.UseShellExecute);
        Assert.IsTrue(info.RedirectStandardInput);
        Assert.IsTrue(info.RedirectStandardOutput);
        CollectionAssert.AreEqual(new[] { "--data-dir", dataDir, "--state-dir", stateDir }, info.ArgumentList.ToArray());
    }

    [TestMethod]
    public async Task Start_SendsInitWithBaseUrlKeyAndChatGptDir()
    {
        await NewCore(Options()).StartAsync();

        CollectionAssert.AreEqual(new[] { $"{BaseUrl}|{ChatGptDir}|True" }, ReadLines("init.log"));
    }

    [TestMethod]
    public async Task Start_InitReturnsError_ThrowsCoreCallExceptionWithCode()
    {
        File.WriteAllText(Path.Combine(stateDir, "init-fail"), "");

        var ex = await Assert.ThrowsExactlyAsync<CoreCallException>(() => NewCore(Options()).StartAsync());

        Assert.AreEqual("internal_error", ex.Code);
    }

    [TestMethod]
    public async Task Call_UnknownMethod_ThrowsCoreCallExceptionWithCode()
    {
        await NewCore(Options()).StartAsync();

        var ex = await Assert.ThrowsExactlyAsync<CoreCallException>(() => core!.CallAsync("no_such_method"));

        Assert.AreEqual("unknown_method", ex.Code);
    }

    [TestMethod]
    public async Task Call_ResponsesOutOfOrder_MatchedById()
    {
        await NewCore(Options()).StartAsync();

        var slow = core!.CallAsync("echo", new Dictionary<string, object?> { ["tag"] = "slow", ["delay_ms"] = 800 });
        var fast = core.CallAsync("echo", new Dictionary<string, object?> { ["tag"] = "fast", ["delay_ms"] = 0 });

        var fastResult = await fast;
        Assert.IsFalse(slow.IsCompleted);
        Assert.AreEqual("fast", fastResult.GetProperty("tag").GetString());
        Assert.AreEqual("slow", (await slow).GetProperty("tag").GetString());
    }

    [TestMethod]
    public async Task Call_ResponseLineContainingU2028AndCr_OnlySplitsOnLf()
    {
        await NewCore(Options()).StartAsync();

        var result = await core!.CallAsync("raw_lines");

        Assert.AreEqual("a\u2028b", result.GetProperty("text").GetString());
    }

    [TestMethod]
    public async Task StatusEvent_DeliveredToSubscriber()
    {
        var received = new TaskCompletionSource<string?>();
        NewCore(Options()).StatusReceived += data => received.TrySetResult(data.GetProperty("version").GetString());
        await core!.StartAsync();

        await core.CallAsync("emit_status");

        Assert.AreEqual("9.9.9", await received.Task.WaitAsync(TimeSpan.FromSeconds(5)));
    }

    [TestMethod]
    public async Task Key_NotInArgsNorEnvironment_ReachesCoreViaStdin()
    {
        await NewCore(Options()).StartAsync();

        var status = await core!.CallAsync("status");

        Assert.AreEqual(Key, status.GetProperty("echo_api_key").GetString());
        Assert.IsFalse(File.ReadAllText(Path.Combine(stateDir, "args.txt")).Contains(Key));
        Assert.IsFalse(File.ReadAllText(Path.Combine(stateDir, "env.txt")).Contains(Key));
    }

    [TestMethod]
    public async Task SaveConnection_KeySentViaStdin()
    {
        await NewCore(Options()).StartAsync();

        await core!.CallAsync("save_connection", new Dictionary<string, object?>
        {
            ["base_url"] = "https://relay2.example.com",
            ["api_key"] = "sk-example-1111",
        });
        var status = await core.CallAsync("status");

        Assert.AreEqual("sk-example-1111", status.GetProperty("echo_api_key").GetString());
        Assert.IsFalse(File.ReadAllText(Path.Combine(stateDir, "args.txt")).Contains("sk-example-1111"));
    }

    [TestMethod]
    public async Task Crash_PendingCallFails_RestartsOnceAndReinits()
    {
        await NewCore(Options()).StartAsync();

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core!.CallAsync("crash"));

        await WaitUntil(() => ReadLines("init.log").Length == 2);
        Assert.AreEqual(2, Launches());
        Assert.AreEqual(ReadLines("init.log")[0], ReadLines("init.log")[1]);
        Assert.IsFalse(core!.HasFailed);
        var status = await core.CallAsync("status");
        Assert.AreEqual(Key, status.GetProperty("echo_api_key").GetString());
    }

    [TestMethod]
    public async Task Crash_AfterStableRun_RestartsAgain()
    {
        await NewCore(Options(TimeSpan.FromMilliseconds(400))).StartAsync();
        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core!.CallAsync("crash"));
        await WaitUntil(() => ReadLines("init.log").Length == 2);
        await Task.Delay(1200);

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core!.CallAsync("crash"));

        await WaitUntil(() => ReadLines("init.log").Length == 3);
        Assert.AreEqual(3, Launches());
        Assert.IsFalse(core!.HasFailed);
    }

    [TestMethod]
    public async Task Crash_SoonAfterRestart_ReportsFailedAndStopsRetrying()
    {
        var failed = new TaskCompletionSource<string>();
        NewCore(Options()).Failed += reason => failed.TrySetResult(reason);
        await core!.StartAsync();
        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("crash"));
        await WaitUntil(() => ReadLines("init.log").Length == 2);
        await Task.Delay(200);

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("crash"));

        await failed.Task.WaitAsync(TimeSpan.FromSeconds(5));
        await Task.Delay(600);
        Assert.AreEqual(2, Launches());
        Assert.IsTrue(core.HasFailed);
        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("status"));
    }

    [TestMethod]
    public async Task Restart_InitReturnsError_ReportsFailedAndStopsRetrying()
    {
        var failed = new TaskCompletionSource<string>();
        NewCore(Options()).Failed += reason => failed.TrySetResult(reason);
        await core!.StartAsync();
        File.WriteAllText(Path.Combine(stateDir, "init-fail"), "");

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("crash"));

        await failed.Task.WaitAsync(TimeSpan.FromSeconds(5));
        await Task.Delay(600);
        Assert.AreEqual(2, Launches());
        Assert.IsTrue(core.HasFailed);
    }

    [TestMethod]
    public async Task Stop_PendingCallFailsWithExited_NoRestart()
    {
        await NewCore(Options()).StartAsync();
        var pending = core!.CallAsync("hang");

        await core.StopAsync();

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => pending);
        await Task.Delay(600);
        Assert.AreEqual(1, Launches());
        Assert.IsFalse(core.HasFailed);
    }

    [TestMethod]
    public async Task Call_AfterStop_ThrowsExited()
    {
        await NewCore(Options()).StartAsync();
        await core!.StopAsync();

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("status"));
    }

    [TestMethod]
    public async Task Stop_CalledTwice_DoesNotThrow()
    {
        await NewCore(Options()).StartAsync();

        await core!.StopAsync();
        await core.StopAsync();
    }

    [TestMethod]
    public async Task StatusEvent_SubscriberThrows_ReadLoopSurvivesAndCoreIsNotRestarted()
    {
        NewCore(Options()).StatusReceived += _ => throw new InvalidOperationException("订阅者出错");
        await core!.StartAsync();

        await core.CallAsync("emit_status").WaitAsync(TimeSpan.FromSeconds(5));
        var status = await core.CallAsync("status").WaitAsync(TimeSpan.FromSeconds(5));

        Assert.AreEqual("1.0.0", status.GetProperty("version").GetString());
        Assert.AreEqual(1, Launches());
        Assert.IsFalse(core.HasFailed);
    }

    [TestMethod]
    public async Task Call_MalformedErrorResponse_ThrowsBadResponseAndLaterCallsStillWork()
    {
        await NewCore(Options()).StartAsync();

        var ex = await Assert.ThrowsExactlyAsync<CoreCallException>(
            () => core!.CallAsync("bad_error").WaitAsync(TimeSpan.FromSeconds(5)));

        Assert.AreEqual("bad_response", ex.Code);
        var status = await core!.CallAsync("status").WaitAsync(TimeSpan.FromSeconds(5));
        Assert.AreEqual("1.0.0", status.GetProperty("version").GetString());
        Assert.AreEqual(1, Launches());
    }

    [TestMethod]
    public async Task Call_WriteFails_PendingAndNewCallsThrowExited()
    {
        await NewCore(Options()).StartAsync();
        var pending = core!.CallAsync("hang");
        var process = (System.Diagnostics.Process)typeof(CoreProcess)
            .GetField("process", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(core)!;
        process.StandardInput.Close();

        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => core.CallAsync("status"));
        await Assert.ThrowsExactlyAsync<CoreProcessExitedException>(() => pending.WaitAsync(TimeSpan.FromSeconds(5)));
    }
}
