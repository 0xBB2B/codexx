using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class CredentialTargetTests
{
    [TestMethod]
    public void TargetNameFor_Id1_UsesOriginalTarget()
    {
        Assert.AreEqual("ModelBridge/relay-api-key", CredentialStore.TargetNameFor("1"));
    }

    [TestMethod]
    public void TargetNameFor_OtherIds_AppendIdSuffix()
    {
        Assert.AreEqual("ModelBridge/relay-api-key-2", CredentialStore.TargetNameFor("2"));
        Assert.AreEqual("ModelBridge/relay-api-key-10", CredentialStore.TargetNameFor("10"));
    }
}
