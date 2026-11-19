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

    ChatGptLocator Locator(params string[] runningPaths) => new(localAppData, () => runningPaths, () => []);

    ChatGptLocator LocatorWithStore(Func<IEnumerable<string>> storePackageDirs, params string[] runningPaths) =>
        new(localAppData, () => runningPaths, storePackageDirs);

    const string StorePackage = "WindowsApps/OpenAI.Codex_26.1111.2228.0_x64__2p2nqsd0c76g0";

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

    [TestMethod]
    public void Find_StorePackageInstalled_ReturnsAppExePath()
    {
        var expected = Touch(StorePackage + "/app/ChatGPT.exe");
        var packageDir = Path.GetDirectoryName(Path.GetDirectoryName(expected))!;

        Assert.AreEqual(expected, LocatorWithStore(() => [packageDir]).Find());
    }

    [TestMethod]
    public void Find_StorePackageWithoutAppExe_ReturnsNull()
    {
        var packageDir = Path.Combine(localAppData, StorePackage.Replace('/', Path.DirectorySeparatorChar));
        Directory.CreateDirectory(packageDir);
        Touch(StorePackage + "/other/ChatGPT.exe");

        Assert.IsNull(LocatorWithStore(() => [packageDir]).Find());
    }

    [TestMethod]
    public void Find_StorePackageQueryThrows_ReturnsNull()
    {
        Assert.IsNull(LocatorWithStore(() => throw new InvalidOperationException("系统接口失败")).Find());
    }

    [TestMethod]
    public void Find_StorePackageQueryThrows_StillFindsKnownDirectory()
    {
        var expected = Touch("Programs/ChatGPT/ChatGPT.exe");

        Assert.AreEqual(expected, LocatorWithStore(() => throw new InvalidOperationException("系统接口失败")).Find());
    }

    [TestMethod]
    public void Find_RunningProcessPath_TakesPrecedenceOverStorePackage()
    {
        var running = Touch("Elsewhere/Codex.exe");
        var storeExe = Touch(StorePackage + "/app/ChatGPT.exe");
        var packageDir = Path.GetDirectoryName(Path.GetDirectoryName(storeExe))!;

        Assert.AreEqual(running, LocatorWithStore(() => [packageDir], running).Find());
    }

    [TestMethod]
    public void Find_KnownDirectory_TakesPrecedenceOverStorePackage()
    {
        var known = Touch("Programs/ChatGPT/ChatGPT.exe");
        var storeExe = Touch(StorePackage + "/app/ChatGPT.exe");
        var packageDir = Path.GetDirectoryName(Path.GetDirectoryName(storeExe))!;

        Assert.AreEqual(known, LocatorWithStore(() => [packageDir]).Find());
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
    public void Locate_RunningPathOnlyInOtherSession_IsIgnored()
    {
        var otherPath = Touch("Elsewhere/Codex.exe");
        var other = new FakeProcess(OtherSession, otherPath);

        Assert.AreNotEqual(Path.GetDirectoryName(otherPath), AppWith(other).Locate());
        Assert.IsTrue(other.Disposed);
    }

    [TestMethod]
    public void Locate_RunningPathInOwnSession_ReturnsItsFolderAndEveryProcessDisposed()
    {
        var ownPath = Touch("Elsewhere/Codex.exe");
        var first = new FakeProcess(MySession, ownPath);
        var second = new FakeProcess(MySession, ownPath);

        Assert.AreEqual(Path.GetDirectoryName(ownPath), AppWith(first, second).Locate());
        Assert.IsTrue(first.Disposed);
        Assert.IsTrue(second.Disposed);
    }

    sealed class SessionGoneProcess : IChatGptProcess
    {
        public bool Disposed;
        public int SessionId => throw new InvalidOperationException("进程已退出");
        public string? ExecutablePath => null;
        public void Dispose() => Disposed = true;
    }

    [TestMethod]
    public void IsRunning_ProcessExitsWhileReadingSessionId_SkipsItAndKeepsOthers()
    {
        var first = new FakeProcess(MySession, null);
        var gone = new SessionGoneProcess();
        var last = new FakeProcess(MySession, null);

        Assert.IsTrue(new ChatGptApp(() => new IChatGptProcess[] { first, gone, last }, MySession).IsRunning());
        Assert.IsTrue(first.Disposed);
        Assert.IsTrue(gone.Disposed);
        Assert.IsTrue(last.Disposed);
    }

    [TestMethod]
    public void IsRunning_OnlyProcessExitedWhileReadingSessionId_ReturnsFalse()
    {
        var gone = new SessionGoneProcess();

        Assert.IsFalse(new ChatGptApp(() => new IChatGptProcess[] { gone }, MySession).IsRunning());
        Assert.IsTrue(gone.Disposed);
    }

    [TestMethod]
    public void Locate_ProcessExitsWhileReadingSessionId_SkipsItAndReturnsOwnFolder()
    {
        var ownPath = Touch("Elsewhere/Codex.exe");
        var gone = new SessionGoneProcess();
        var own = new FakeProcess(MySession, ownPath);

        Assert.AreEqual(Path.GetDirectoryName(ownPath), new ChatGptApp(() => new IChatGptProcess[] { gone, own }, MySession).Locate());
        Assert.IsTrue(gone.Disposed);
        Assert.IsTrue(own.Disposed);
    }

    [TestMethod]
    public void Find_MultipleVersionDirs_PicksHighestByVersionNotByString()
    {
        Touch("Programs/ChatGPT/app-1.9.0/ChatGPT.exe");
        var newest = Touch("Programs/ChatGPT/app-1.10.0/ChatGPT.exe");

        Assert.AreEqual(newest, Locator().Find());
    }

    [TestMethod]
    public void Find_VersionDirsDifferInMajor_PicksHighestMajor()
    {
        Touch("OpenAI Codex/app-9.0.0/Codex.exe");
        var newest = Touch("OpenAI Codex/app-10.0.0/Codex.exe");

        Assert.AreEqual(newest, Locator().Find());
    }

    [TestMethod]
    public void Find_UnparsableVersionDirs_FallBackToDescendingStringOrder()
    {
        Touch("Programs/ChatGPT/app-alpha/ChatGPT.exe");
        var last = Touch("Programs/ChatGPT/app-zeta/ChatGPT.exe");

        Assert.AreEqual(last, Locator().Find());
    }
}
