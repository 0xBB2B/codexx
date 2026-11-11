using System.Collections.Generic;

namespace ModelBridge.Services;

public static class CoreErrorMessages
{
    public static readonly IReadOnlyDictionary<string, string> Messages = new Dictionary<string, string>
    {
        ["invalid_base_url"] = "Base URL 要以 https:// 开头。",
        ["relay_401"] = "中台服务返回 401：API Key 无效或已过期",
        ["relay_403"] = "中台服务返回 403：API Key 没有权限",
        ["relay_404"] = "中台服务返回 404：Base URL 不正确",
        ["relay_429"] = "中台服务返回 429：请求太频繁，请稍后重试",
        ["relay_5xx"] = "中台服务暂时不可用",
        ["relay_4xx"] = "请求未被中台接受",
        ["relay_unreachable"] = "连不上中台，请检查网络和 Base URL",
        ["chatgpt_not_found"] = "没有找到 ChatGPT",
        ["chatgpt_unsupported"] = "当前 ChatGPT 版本暂不支持",
        ["proxy_port_in_use"] = "本机转发服务没能启动：端口被占用",
        ["restore_blocked"] = "没能自动还原 Codex 配置",
        ["config_invalid"] = "Codex 配置文件格式有误，未做修改",
        ["config_changed"] = "Codex 配置文件刚被其他程序修改，请重试",
        ["update_unavailable"] = "暂时无法检查更新，请稍后再试",
        ["internal_error"] = "内部错误，请重试",
        ["unknown_method"] = "未知请求",
        ["bad_params"] = "请求参数有误",
    };
}
