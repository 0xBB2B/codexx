using System.Diagnostics;
using System.IO.Pipes;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class SingleInstanceTests
{
    string mutexName = "";
    string pipeName = "";

    [TestInitialize]
    public void Setup()
    {
        var id = Guid.NewGuid().ToString("N");
        mutexName = @"Local\ModelBridge.Tests." + id;
        pipeName = "ModelBridge.Tests." + id;
    }

    [TestMethod]
    public void Defaults_MutexNameAndSessionScopedPipeName()
    {
        using var current = Process.GetCurrentProcess();

        Assert.AreEqual(@"Local\ModelBridge.SingleInstance", SingleInstance.DefaultMutexName);
        Assert.AreEqual("ModelBridge.Activate." + current.SessionId, SingleInstance.DefaultPipeName);
    }

    [TestMethod]
    public void TryAcquire_FirstInstance_ReturnsAcquired()
    {
        using var first = new SingleInstance(mutexName, pipeName);

        Assert.AreEqual(SingleInstanceResult.Acquired, first.TryAcquire(() => { }));
    }

    [TestMethod]
    public void TryAcquire_SecondInstance_ReturnsActivatedOtherAndFirstGetsActivated()
    {
        using var activated = new ManualResetEventSlim();
        using var first = new SingleInstance(mutexName, pipeName);
        using var second = new SingleInstance(mutexName, pipeName);
        Assert.AreEqual(SingleInstanceResult.Acquired, first.TryAcquire(activated.Set));

        var result = second.TryAcquire(() => Assert.Fail("第二个实例不应收到激活"));

        Assert.AreEqual(SingleInstanceResult.ActivatedOther, result);
        Assert.IsTrue(activated.Wait(TimeSpan.FromSeconds(5)));
    }

    [TestMethod]
    public void TryAcquire_SecondInstanceTwice_FirstActivatedTwice()
    {
        var count = 0;
        using var twice = new CountdownEvent(2);
        using var first = new SingleInstance(mutexName, pipeName);
        using var second = new SingleInstance(mutexName, pipeName);
        using var third = new SingleInstance(mutexName, pipeName);
        first.TryAcquire(() => { Interlocked.Increment(ref count); twice.Signal(); });

        second.TryAcquire(() => { });
        third.TryAcquire(() => { });

        Assert.IsTrue(twice.Wait(TimeSpan.FromSeconds(5)));
        Assert.AreEqual(2, count);
    }

    [TestMethod]
    public void TryAcquire_AfterFirstDisposed_NewInstanceBecomesPrimary()
    {
        var first = new SingleInstance(mutexName, pipeName);
        Assert.AreEqual(SingleInstanceResult.Acquired, first.TryAcquire(() => { }));
        first.Dispose();

        using var next = new SingleInstance(mutexName, pipeName);

        Assert.AreEqual(SingleInstanceResult.Acquired, next.TryAcquire(() => { }));
    }

    [TestMethod]
    public void TryAcquire_MutexHeldButNoListener_ReturnsActivationFailed()
    {
        using var foreignOwner = new Mutex(true, mutexName);
        using var second = new SingleInstance(mutexName, pipeName);

        Assert.AreEqual(SingleInstanceResult.ActivationFailed, second.TryAcquire(() => { }));
    }

    [TestMethod]
    public void TryAcquire_ConnectionThatNeverSendsData_DoesNotBlockLaterActivation()
    {
        using var activated = new ManualResetEventSlim();
        using var first = new SingleInstance(mutexName, pipeName);
        using var second = new SingleInstance(mutexName, pipeName);
        first.TryAcquire(activated.Set);
        using var silent = new NamedPipeClientStream(".", pipeName, PipeDirection.Out);
        silent.Connect(2000);

        var result = second.TryAcquire(() => { });

        Assert.AreEqual(SingleInstanceResult.ActivatedOther, result);
        Assert.IsTrue(activated.Wait(TimeSpan.FromSeconds(5)));
    }

    [TestMethod]
    public void TryAcquire_CallbackThrows_ListeningContinues()
    {
        var count = 0;
        using var twice = new CountdownEvent(2);
        using var first = new SingleInstance(mutexName, pipeName);
        using var second = new SingleInstance(mutexName, pipeName);
        using var third = new SingleInstance(mutexName, pipeName);
        first.TryAcquire(() =>
        {
            Interlocked.Increment(ref count);
            twice.Signal();
            throw new InvalidOperationException("回调出错");
        });

        second.TryAcquire(() => { });
        third.TryAcquire(() => { });

        Assert.IsTrue(twice.Wait(TimeSpan.FromSeconds(5)));
        Assert.AreEqual(2, count);
    }
}
