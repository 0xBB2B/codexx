using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Security.Principal;
using System.Threading;
using System.Threading.Tasks;

namespace ModelBridge.Services;

public enum SingleInstanceResult
{
    Acquired,
    ActivatedOther,
    ActivationFailed,
}

public sealed class SingleInstance(string? customMutexName = null, string? customPipeName = null) : IDisposable
{
    public const string DefaultMutexName = @"Local\ModelBridge.SingleInstance";
    public static readonly string DefaultPipeName = "ModelBridge.Activate." + CurrentSessionId();

    readonly string mutexName = customMutexName ?? DefaultMutexName;
    readonly string pipeName = customPipeName ?? DefaultPipeName;
    readonly CancellationTokenSource cts = new();
    Mutex? mutex;

    public SingleInstanceResult TryAcquire(Action onActivated)
    {
        mutex = new Mutex(true, mutexName, out var createdNew);
        if (createdNew)
        {
            _ = Task.Run(() => ListenAsync(onActivated, cts.Token));
            return SingleInstanceResult.Acquired;
        }

        mutex.Dispose();
        mutex = null;
        return SendActivation() ? SingleInstanceResult.ActivatedOther : SingleInstanceResult.ActivationFailed;
    }

    public void Dispose()
    {
        cts.Cancel();
        mutex?.Dispose();
        mutex = null;
    }

    static int CurrentSessionId()
    {
        using var current = Process.GetCurrentProcess();
        return current.SessionId;
    }

    async Task ListenAsync(Action onActivated, CancellationToken ct)
    {
        while (!ct.IsCancellationRequested)
        {
            NamedPipeServerStream server;
            try
            {
                server = new NamedPipeServerStream(pipeName, PipeDirection.In, -1, PipeTransmissionMode.Byte,
                    PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
            }
            catch (UnauthorizedAccessException)
            {
                return;
            }
            catch (IOException)
            {
                continue;
            }

            try
            {
                await server.WaitForConnectionAsync(ct);
            }
            catch (OperationCanceledException)
            {
                server.Dispose();
                return;
            }
            catch (IOException)
            {
                server.Dispose();
                continue;
            }
            _ = HandleConnectionAsync(server, onActivated, ct);
        }
    }

    static async Task HandleConnectionAsync(NamedPipeServerStream server, Action onActivated, CancellationToken ct)
    {
        using (server)
        {
            using var readTimeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
            readTimeout.CancelAfter(TimeSpan.FromSeconds(2));
            try
            {
                await server.ReadExactlyAsync(new byte[1], readTimeout.Token);
            }
            catch (Exception ex) when (ex is OperationCanceledException or IOException)
            {
                return;
            }
        }

        try { onActivated(); }
        catch (Exception) { }
    }

    bool SendActivation()
    {
        try
        {
            using var client = new NamedPipeClientStream(".", pipeName, PipeDirection.Out,
                PipeOptions.CurrentUserOnly, TokenImpersonationLevel.Identification);
            client.Connect(2000);
            client.WriteByte(1);
            client.Flush();
            return true;
        }
        catch (Exception ex) when (ex is TimeoutException or IOException or UnauthorizedAccessException)
        {
            return false;
        }
    }
}
