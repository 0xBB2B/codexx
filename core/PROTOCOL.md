# 核心程序控制协议

外壳以子进程启动 `model-bridge-core --data-dir <数据目录>`，通过标准输入输出通信。每行一个 JSON 对象，UTF-8，以 `\n` 结尾。标准错误输出不用于通信。

## 请求（外壳 → 核心）
{"id": <整数>, "method": "<方法名>", "params": { ... }}

## 响应（核心 → 外壳）
{"id": <同请求>, "result": { ... }}
{"id": <同请求>, "error": {"code": "<错误码>", "keys": ["<键名>", ...]}}
`keys` 在 restore_blocked 和 config_invalid 时出现，列出相关键名。

## 事件（核心 → 外壳，无 id）
{"event": "status", "data": <Status>}

## 方法
| 方法 | params | result |
|---|---|---|
| init | {"base_url": 字符串或 null, "api_key": 字符串或 null, "chatgpt_dir": 字符串或 null} | Status |
| status | {} | Status |
| save_connection | {"base_url": 字符串, "api_key": 字符串} | {"models_changed": 布尔, "relay_model_count": 整数} |
| set_forwarding | {"enabled": 布尔} | Status |
| refresh_models | {} | Status |
| check_update | {"manual": 布尔} | {"latest_version": 字符串或 null, "url": 字符串或 null, "is_newer": 布尔} |
| prepare_uninstall | {} | {} |
| shutdown | {"restore": 布尔} | {}（随后进程退出） |

## Status
{
  "version": "1.2.0",
  "forwarding": true,
  "proxy": {"running": true, "error": null},
  "chatgpt": {"found": true, "error": null},
  "relay": {"model_count": 6, "fetched_at": "2026-11-11T00:12:00Z", "stale": false, "error": null},
  "taken_over": ["openai_base_url"],
  "restore_blocked": [],
  "config_path": "/Users/x/.codex/config.toml",
  "update": {"latest_version": null, "url": null}
}

## 错误码
| 码 | 界面说明 |
|---|---|
| invalid_base_url | Base URL 要以 https:// 开头。 |
| relay_401 | 中台服务返回 401：API Key 无效或已过期 |
| relay_403 | 中台服务返回 403：API Key 没有权限 |
| relay_404 | 中台服务返回 404：Base URL 不正确 |
| relay_429 | 中台服务返回 429：请求太频繁，请稍后重试 |
| relay_5xx | 中台服务暂时不可用 |
| relay_4xx | 请求未被中台接受 |
| relay_unreachable | 连不上中台，请检查网络和 Base URL |
| chatgpt_not_found | 没有找到 ChatGPT |
| chatgpt_unsupported | 当前 ChatGPT 版本暂不支持 |
| proxy_port_in_use | 本机转发服务没能启动：端口被占用 |
| restore_blocked | 没能自动还原 Codex 配置 |
| config_invalid | Codex 配置文件格式有误，未做修改 |
| config_changed | Codex 配置文件刚被其他程序修改，请重试 |
| update_unavailable | 暂时无法检查更新，请稍后再试 |
| internal_error | 内部错误，请重试 |
| unknown_method | 未知请求 |
| bad_params | 请求参数有误 |
