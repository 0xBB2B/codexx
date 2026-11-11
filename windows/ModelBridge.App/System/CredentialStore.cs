using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;

namespace ModelBridge.Services;

public sealed class CredentialStore(string targetName = CredentialStore.DefaultTargetName)
{
    public const string DefaultTargetName = "ModelBridge/relay-api-key";

    const uint CredTypeGeneric = 1;
    const uint CredPersistLocalMachine = 2;
    const int ErrorNotFound = 1168;

    public void Save(string key)
    {
        var blob = Encoding.Unicode.GetBytes(key);
        var blobPtr = blob.Length == 0 ? IntPtr.Zero : Marshal.AllocHGlobal(blob.Length);
        var targetPtr = Marshal.StringToHGlobalUni(targetName);
        try
        {
            if (blob.Length > 0) Marshal.Copy(blob, 0, blobPtr, blob.Length);
            var credential = new NativeCredential
            {
                Type = CredTypeGeneric,
                TargetName = targetPtr,
                CredentialBlobSize = (uint)blob.Length,
                CredentialBlob = blobPtr,
                Persist = CredPersistLocalMachine,
            };
            if (!CredWriteW(ref credential, 0)) throw new Win32Exception(Marshal.GetLastWin32Error());
        }
        finally
        {
            if (blobPtr != IntPtr.Zero) Marshal.FreeHGlobal(blobPtr);
            Marshal.FreeHGlobal(targetPtr);
        }
    }

    public string? Read()
    {
        if (!CredReadW(targetName, CredTypeGeneric, 0, out var ptr))
        {
            var error = Marshal.GetLastWin32Error();
            if (error == ErrorNotFound) return null;
            throw new Win32Exception(error);
        }
        try
        {
            var credential = Marshal.PtrToStructure<NativeCredential>(ptr);
            if (credential.CredentialBlob == IntPtr.Zero) return "";
            return Marshal.PtrToStringUni(credential.CredentialBlob, (int)credential.CredentialBlobSize / 2);
        }
        finally
        {
            CredFree(ptr);
        }
    }

    public void Delete()
    {
        if (CredDeleteW(targetName, CredTypeGeneric, 0)) return;
        var error = Marshal.GetLastWin32Error();
        if (error != ErrorNotFound) throw new Win32Exception(error);
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
    static extern bool CredWriteW(ref NativeCredential credential, uint flags);

    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern bool CredReadW(string target, uint type, uint flags, out IntPtr credential);

    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern bool CredDeleteW(string target, uint type, uint flags);

    [DllImport("advapi32.dll")]
    static extern void CredFree(IntPtr buffer);
}
