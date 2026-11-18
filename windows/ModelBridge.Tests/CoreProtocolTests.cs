using Microsoft.VisualStudio.TestTools.UnitTesting;
using ModelBridge.Services;

namespace ModelBridge.Tests;

[TestClass]
public class CoreProtocolTests
{
    static string ProtocolPath()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir != null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "core", "PROTOCOL.md");
            if (File.Exists(candidate)) return candidate;
        }
        throw new FileNotFoundException("从测试程序集目录向上找不到 core/PROTOCOL.md");
    }

    static Dictionary<string, string> ReadProtocolErrorTable()
    {
        var table = new Dictionary<string, string>();
        var inSection = false;
        foreach (var line in File.ReadLines(ProtocolPath()))
        {
            if (line.StartsWith("## ")) inSection = line.Trim() == "## 错误码";
            else if (inSection && line.StartsWith("|"))
            {
                var cells = line.Trim().Trim('|').Split('|').Select(c => c.Trim()).ToArray();
                if (cells.Length == 2 && cells[0] != "码" && !cells[0].StartsWith("---"))
                    table[cells[0]] = cells[1];
            }
        }
        return table;
    }

    [TestMethod]
    public void ErrorMessages_CoverEveryCodeInProtocol()
    {
        var protocol = ReadProtocolErrorTable();

        Assert.IsTrue(protocol.Count > 0);
        var missing = protocol.Keys.Except(CoreErrorMessages.Messages.Keys).ToArray();
        CollectionAssert.AreEqual(Array.Empty<string>(), missing);
    }

    [TestMethod]
    public void ErrorMessages_TextMatchesProtocolVerbatim()
    {
        foreach (var (code, text) in ReadProtocolErrorTable())
            Assert.AreEqual(text, CoreErrorMessages.Messages[code], $"错误码 {code} 的文字与 PROTOCOL.md 不一致");
    }

    [TestMethod]
    public void ErrorMessages_NonListResponseCode_IsInProtocolAndNotRelay404()
    {
        const string text = "没有返回模型列表，请检查 Base URL";
        var entry = ReadProtocolErrorTable().Where(e => e.Value.Contains(text)).ToArray();

        Assert.AreEqual(1, entry.Length, "PROTOCOL.md 里应有且只有一个错误码对应「" + text + "」");
        Assert.AreEqual("relay_bad_response", entry[0].Key);
        Assert.AreEqual("中转服务返回 <keys[0]>：" + text, CoreErrorMessages.Messages[entry[0].Key]);
    }

    [TestMethod]
    public void ErrorMessages_Relay5xxAnd4xx_CarryStatusPlaceholder()
    {
        Assert.AreEqual("中转服务返回 <keys[0]>：中转服务暂时不可用", CoreErrorMessages.Messages["relay_5xx"]);
        Assert.AreEqual("中转服务返回 <keys[0]>：请求未被中转服务接受", CoreErrorMessages.Messages["relay_4xx"]);
    }

    [TestMethod]
    public void ErrorMessages_IncludeMultiRelayCodes()
    {
        string[] codes = ["name_empty", "name_too_long", "name_has_dot", "name_duplicate", "relay_limit", "relay_last", "relay_not_found"];
        var protocol = ReadProtocolErrorTable();

        foreach (var code in codes)
        {
            Assert.IsTrue(protocol.ContainsKey(code), $"PROTOCOL.md 缺 {code}");
            Assert.IsTrue(CoreErrorMessages.Messages.ContainsKey(code), $"Messages 缺 {code}");
        }
    }

    [TestMethod]
    public void ErrorMessages_RelayCodesSayZhongzhuan_NotLegacyWord()
    {
        var legacy = "中" + "台";
        foreach (var (code, text) in CoreErrorMessages.Messages.Where(m => m.Key.StartsWith("relay_")))
        {
            Assert.IsFalse(text.Contains(legacy), code);
        }
        Assert.IsTrue(CoreErrorMessages.Messages["relay_401"].StartsWith("中转服务返回 401"));
        Assert.IsTrue(CoreErrorMessages.Messages["relay_unreachable"].Contains("中转"));
    }

    [TestMethod]
    public void ErrorMessages_NoCodeOutsideProtocol()
    {
        var extra = CoreErrorMessages.Messages.Keys.Except(ReadProtocolErrorTable().Keys).ToArray();

        CollectionAssert.AreEqual(Array.Empty<string>(), extra);
    }

    [TestMethod]
    public void ErrorMessages_ProxyStartTimeout_HasConfirmedCopy()
    {
        Assert.AreEqual("启动转发超过 30 秒还没完成，请重试", CoreErrorMessages.Messages["proxy_start_timeout"]);
    }
}
