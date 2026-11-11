using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class ChatGPTAppTests
{
    string localAppData = "";

    [TestInitialize]
    public void Setup()
    {
        localAppData = Path.Combine(Path.GetTempPath(), "mb-chatgpt-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(localAppData);
    }

    [TestCleanup]
    public void Cleanup()
    {
        if (Directory.Exists(localAppData)) Directory.Delete(localAppData, true);
    }

    string Touch(string relative)
    {
        var path = Path.Combine(localAppData, relative.Replace('/', Path.DirectorySeparatorChar));
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        File.WriteAllText(path, "");
        return path;
    }

    ChatGptLocator Locator(params string[] runningPaths) => new(localAppData, () => runningPaths);

    [TestMethod]
    [DataRow("Programs/ChatGPT/ChatGPT.exe")]
    [DataRow("Programs/OpenAI ChatGPT/ChatGPT.exe")]
    [DataRow("ChatGPT/ChatGPT.exe")]
    [DataRow("OpenAI/ChatGPT/ChatGPT.exe")]
    [DataRow("Programs/Codex/Codex.exe")]
    [DataRow("OpenAI Codex/Codex.exe")]
    [DataRow("Programs/ChatGPT/Codex.exe")]
    [DataRow("Programs/ChatGPT/app-1.2.3/ChatGPT.exe")]
    [DataRow("OpenAI Codex/app-2.0.0/Codex.exe")]
    public void Find_InstalledUnderKnownDirectory_ReturnsExePath(string relative)
    {
        var expected = Touch(relative);

        Assert.AreEqual(expected, Locator().Find());
    }

    [TestMethod]
    public void Find_NothingInstalled_ReturnsNull()
    {
        Assert.IsNull(Locator().Find());
    }

    [TestMethod]
    public void Find_UnknownDirectory_ReturnsNull()
    {
        Touch("Programs/Other/ChatGPT.exe");

        Assert.IsNull(Locator().Find());
    }

    [TestMethod]
    public void Find_MultipleDirectories_ReturnsFirstInDocumentedOrder()
    {
        Touch("OpenAI Codex/Codex.exe");
        Touch("Programs/Codex/Codex.exe");
        Touch("OpenAI/ChatGPT/ChatGPT.exe");
        Touch("ChatGPT/ChatGPT.exe");
        var second = Touch("Programs/OpenAI ChatGPT/ChatGPT.exe");
        var first = Touch("Programs/ChatGPT/ChatGPT.exe");

        Assert.AreEqual(first, Locator().Find());
        File.Delete(first);
        Assert.AreEqual(second, Locator().Find());
    }

    [TestMethod]
    public void Find_RunningProcessPath_TakesPrecedenceOverInstallDirectories()
    {
        Touch("Programs/ChatGPT/ChatGPT.exe");
        var running = Touch("Elsewhere/Codex.exe");

        Assert.AreEqual(running, Locator(running).Find());
    }

    sealed class FakeApp : IChatGptApp
    {
        public string? InstallPath = @"C:\Apps\ChatGPT\ChatGPT.exe";
        public bool Running = true;
        public bool CloseResult = true;
        public bool LaunchFails;
        public bool CloseBlocksUntilCancelled;
        public TimeSpan? CloseTimeout;
        public readonly List<string> Calls = [];

        public string? Locate() => InstallPath;

        public bool IsRunning() => Running;

        public async Task<bool> RequestCloseAndWaitAsync(TimeSpan timeout, CancellationToken ct)
        {
            Calls.Add("close");
            CloseTimeout = timeout;
            if (CloseBlocksUntilCancelled) await Task.Delay(Timeout.Infinite, ct);
            return CloseResult;
        }

        public void Launch(string exePath)
        {
            Calls.Add("launch:" + exePath);
            if (LaunchFails) throw new InvalidOperationException("launch failed");
        }
    }

    [TestMethod]
    public async Task Restart_Running_ClosesThenLaunchesFoundPath()
    {
        var app = new FakeApp();

        var result = await new ChatGptRestarter(app, TimeSpan.FromSeconds(7)).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.Restarted, result);
        CollectionAssert.AreEqual(new[] { "close", @"launch:C:\Apps\ChatGPT\ChatGPT.exe" }, app.Calls);
        Assert.AreEqual(TimeSpan.FromSeconds(7), app.CloseTimeout);
    }

    [TestMethod]
    public async Task Restart_DefaultTimeout_Is30Seconds()
    {
        var app = new FakeApp();

        await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(TimeSpan.FromSeconds(30), ChatGptRestarter.DefaultCloseTimeout);
        Assert.AreEqual(ChatGptRestarter.DefaultCloseTimeout, app.CloseTimeout);
    }

    [TestMethod]
    public async Task Restart_NotRunning_LaunchesWithoutClosing()
    {
        var app = new FakeApp { Running = false };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.Restarted, result);
        CollectionAssert.AreEqual(new[] { @"launch:C:\Apps\ChatGPT\ChatGPT.exe" }, app.Calls);
    }

    [TestMethod]
    public async Task Restart_NotFound_ReturnsNotFoundAndDoesNothing()
    {
        var app = new FakeApp { InstallPath = null };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.NotFound, result);
        Assert.AreEqual(0, app.Calls.Count);
    }

    [TestMethod]
    public async Task Restart_CloseTimesOut_ReturnsTimedOutWithoutLaunching()
    {
        var app = new FakeApp { CloseResult = false };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.CloseTimedOut, result);
        CollectionAssert.AreEqual(new[] { "close" }, app.Calls);
    }

    [TestMethod]
    public async Task Restart_LaunchFails_ReturnsOpenFailed()
    {
        var app = new FakeApp { LaunchFails = true };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.OpenFailed, result);
    }

    [TestMethod]
    public async Task Restart_CancelledWhileWaitingForClose_ReturnsCancelledImmediately()
    {
        var app = new FakeApp { CloseBlocksUntilCancelled = true };
        using var cts = new CancellationTokenSource();
        var restart = new ChatGptRestarter(app).RestartAsync(cts.Token);

        cts.CancelAfter(100);

        Assert.AreEqual(ChatGptRestartResult.Cancelled, await restart.WaitAsync(TimeSpan.FromSeconds(2)));
        CollectionAssert.AreEqual(new[] { "close" }, app.Calls);
    }

    [TestMethod]
    public async Task Restart_AlreadyCancelled_ReturnsCancelledWithoutLaunching()
    {
        var app = new FakeApp { Running = false };
        using var cts = new CancellationTokenSource();
        cts.Cancel();

        var result = await new ChatGptRestarter(app).RestartAsync(cts.Token);

        Assert.AreEqual(ChatGptRestartResult.Cancelled, result);
        Assert.AreEqual(0, app.Calls.Count);
    }

    [TestMethod]
    public async Task Restart_CloseThrowsWin32Exception_ReturnsCloseTimedOut()
    {
        var app = new ThrowingApp { CloseThrows = new System.ComponentModel.Win32Exception(5) };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.CloseTimedOut, result);
        Assert.IsFalse(app.Launched);
    }

    [TestMethod]
    public async Task Restart_CloseThrowsInvalidOperation_ReturnsCloseTimedOut()
    {
        var app = new ThrowingApp { CloseThrows = new InvalidOperationException() };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.CloseTimedOut, result);
        Assert.IsFalse(app.Launched);
    }

    [TestMethod]
    public async Task Restart_LaunchThrowsWin32Exception_ReturnsOpenFailed()
    {
        var app = new ThrowingApp { LaunchThrows = new System.ComponentModel.Win32Exception(2) };

        var result = await new ChatGptRestarter(app).RestartAsync(CancellationToken.None);

        Assert.AreEqual(ChatGptRestartResult.OpenFailed, result);
    }

    sealed class ThrowingApp : IChatGptApp
    {
        public Exception? CloseThrows;
        public Exception? LaunchThrows;
        public bool Launched;

        public string? Locate() => @"C:\Apps\ChatGPT\ChatGPT.exe";

        public bool IsRunning() => true;

        public Task<bool> RequestCloseAndWaitAsync(TimeSpan timeout, CancellationToken ct) =>
            CloseThrows != null ? throw CloseThrows : Task.FromResult(true);

        public void Launch(string exePath)
        {
            Launched = true;
            if (LaunchThrows != null) throw LaunchThrows;
        }
    }

    sealed class FakeProcess(int sessionId, string? path) : IChatGptProcess
    {
        public bool Disposed;
        public bool CloseRequested;
        public int SessionId => sessionId;
        public string? ExecutablePath => path;
        public void CloseMainWindow() => CloseRequested = true;
        public Task WaitForExitAsync(CancellationToken ct) => Task.CompletedTask;
        public void Dispose() => Disposed = true;
    }

    const int MySession = 1;
    const int OtherSession = 2;

    static ChatGptApp AppWith(params FakeProcess[] processes) => new(() => processes, MySession);

    [TestMethod]
    public void IsRunning_OnlyProcessInOtherSession_ReturnsFalseAndDisposesIt()
    {
        var other = new FakeProcess(OtherSession, null);

        Assert.IsFalse(AppWith(other).IsRunning());
        Assert.IsTrue(other.Disposed);
    }

    [TestMethod]
    public void IsRunning_ProcessInOwnSession_ReturnsTrue()
    {
        Assert.IsTrue(AppWith(new FakeProcess(OtherSession, null), new FakeProcess(MySession, null)).IsRunning());
    }

    [TestMethod]
    public async Task RequestClose_ClosesOnlyOwnSessionProcessesAndDisposesAll()
    {
        var mine = new FakeProcess(MySession, null);
        var other = new FakeProcess(OtherSession, null);

        var closed = await AppWith(mine, other).RequestCloseAndWaitAsync(TimeSpan.FromSeconds(1), CancellationToken.None);

        Assert.IsTrue(closed);
        Assert.IsTrue(mine.CloseRequested);
        Assert.IsFalse(other.CloseRequested);
        Assert.IsTrue(mine.Disposed);
        Assert.IsTrue(other.Disposed);
    }

    [TestMethod]
    public void Locate_RunningPathOnlyInOtherSession_IsIgnored()
    {
        var otherPath = Touch("Elsewhere/Codex.exe");
        var other = new FakeProcess(OtherSession, otherPath);

        Assert.AreNotEqual(otherPath, AppWith(other).Locate());
        Assert.IsTrue(other.Disposed);
    }

    [TestMethod]
    public void Locate_RunningPathInOwnSession_IsReturnedAndEveryProcessDisposed()
    {
        var ownPath = Touch("Elsewhere/Codex.exe");
        var first = new FakeProcess(MySession, ownPath);
        var second = new FakeProcess(MySession, ownPath);

        Assert.AreEqual(ownPath, AppWith(first, second).Locate());
        Assert.IsTrue(first.Disposed);
        Assert.IsTrue(second.Disposed);
    }
}
