using System.Reflection;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace ModelBridge.Tests;

[TestClass]
public class SmokeTests
{
    [TestMethod]
    public void AppAssembly_CanBeLoaded()
    {
        var assembly = Assembly.Load("ModelBridge");

        Assert.AreEqual("ModelBridge", assembly.GetName().Name);
    }
}
