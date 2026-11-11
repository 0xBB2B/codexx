using System.Runtime.InteropServices;
using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class CredentialStoreTests
{
    const uint CredTypeGeneric = 1;
    const uint CredPersistLocalMachine = 2;

    CredentialStore store = null!;
    string target = "";

    [TestInitialize]
    public void Setup()
    {
        target = "ModelBridge.Tests/" + Guid.NewGuid().ToString("N");
        store = new CredentialStore(target);
    }

    [TestCleanup]
    public void Cleanup() => store.Delete();

    [TestMethod]
    public void DefaultTargetName_IsRelayApiKey()
    {
        Assert.AreEqual("ModelBridge/relay-api-key", CredentialStore.DefaultTargetName);
    }

    [TestMethod]
    public void Read_NoEntry_ReturnsNull()
    {
        Assert.IsNull(store.Read());
    }

    [TestMethod]
    public void Save_ThenRead_ReturnsSameKey()
    {
        store.Save("sk-example-0000");

        Assert.AreEqual("sk-example-0000", store.Read());
    }

    [TestMethod]
    public void Save_NonAsciiValue_RoundTrips()
    {
        store.Save("sk-example-密钥-0000");

        Assert.AreEqual("sk-example-密钥-0000", store.Read());
    }

    [TestMethod]
    public void Save_EmptyString_ReadReturnsEmptyString()
    {
        store.Save("");

        Assert.AreEqual("", store.Read());
    }

    [TestMethod]
    public void Save_Twice_OverwritesWithLatest()
    {
        store.Save("sk-example-0000");
        store.Save("sk-example-1111");

        Assert.AreEqual("sk-example-1111", store.Read());
    }

    [TestMethod]
    public void Delete_ExistingEntry_ReadReturnsNull()
    {
        store.Save("sk-example-0000");

        store.Delete();

        Assert.IsNull(store.Read());
    }

    [TestMethod]
    public void Delete_NoEntry_DoesNotThrow()
    {
        store.Delete();
    }

    [TestMethod]
    public void Save_StoresGenericCredentialPersistedToLocalMachine()
    {
        store.Save("sk-example-0000");

        Assert.IsTrue(CredReadW(target, CredTypeGeneric, 0, out var ptr));
        try
        {
            var credential = Marshal.PtrToStructure<NativeCredential>(ptr);
            Assert.AreEqual(CredTypeGeneric, credential.Type);
            Assert.AreEqual(CredPersistLocalMachine, credential.Persist);
        }
        finally
        {
            CredFree(ptr);
        }
    }

    [StructLayout(LayoutKind.Sequential)]
    struct NativeCredential
    {
        public uint Flags;
        public uint Type;
        public IntPtr TargetName;
        public IntPtr Comment;
        public long LastWritten;
        public uint CredentialBlobSize;
        public IntPtr CredentialBlob;
        public uint Persist;
        public uint AttributeCount;
        public IntPtr Attributes;
        public IntPtr TargetAlias;
        public IntPtr UserName;
    }

    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern bool CredReadW(string target, uint type, uint flags, out IntPtr credential);

    [DllImport("advapi32.dll")]
    static extern void CredFree(IntPtr buffer);
}
