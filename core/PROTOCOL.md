# 核心程序控制协议

外壳以子进程启动 `model-bridge-core --data-dir <数据目录>`，通过标准输入输出通信。每行一个 JSON 对象，UTF-8，以 `\n` 结尾。标准错误输出不用于通信。

## 请求（外壳 → 核心）
{"id": <整数>, "method": "<方法名>", "params": { ... }}

## 响应（核心 → 外壳）
{"id": <同请求>, "result": { ... }}
{"id": <同请求>, "error": {"code": "<错误码>", "keys": ["<键名>", ...]}}
`keys` 在 restore_blocked 和 config_invalid 时出现，列出相关键名；在 name_duplicate 时放已有的那个配置名字。在 relay_5xx、relay_4xx、relay_bad_response 时，`keys` 放中转服务返回的状态码。

## 事件（核心 → 外壳，无 id）
{"event": "status", "data": <Status>}

## 方法
| 方法 | params | result |
|---|---|---|
| init | {"relays": [{"id": 字符串, "name": 字符串, "base_url": 字符串, "api_key": 字符串}], "chatgpt_dir": 字符串或 null} | Status |
| status | {} | Status（重新检测：转发关时重新查找 ChatGPT、重新检查配置能否还原；转发开但本机转发服务没在运行时重试启动，最长 30 秒） |
| save_relay | {"id": 字符串或 null, "name": 字符串, "base_url": 字符串, "api_key": 字符串} | {"id": 字符串, "name": 字符串, "model_count": 整数, "models_changed": 布尔}（id 为 null 表示新增；name 是去掉首尾空格后的名字） |
| delete_relay | {"id": 字符串} | Status |
| set_forwarding | {"enabled": 布尔} | Status |
| refresh_models | {"id": 字符串或 null} | Status（id 为 null 刷新全部配置；转发开但本机转发服务没在运行时先重试启动） |
| detect_access | {} | Status（只检测登录状态，更新 access.target 和 access.detect，不改 Codex 配置） |
| apply_access | {} | Status（转发开着且 access.target 与 access.mode 不同时，按 target 切换接入方式；否则不改动） |
| set_relay_only | {"enabled": 布尔} | Status（保存「只用中转」开关；转发开着时按新的目标方式切换接入方式，写 Codex 配置失败时开关和接入方式保持原样并返回错误） |
| check_update | {"manual": 布尔} | {"latest_version": 字符串或 null, "url": 字符串或 null, "installer": 布尔, "is_newer": 布尔} |
| prepare_uninstall | {} | {} |
| shutdown | {"restore": 布尔} | {}（随后进程退出） |

## Status
{
  "version": "1.2.0",
  "forwarding": true,
  "proxy": {"running": true, "error": null},
  "chatgpt": {"found": true, "error": null},
  "relays": [
    {"id": "1", "name": "中转", "model_count": 6, "fetched_at": "2026-11-11T00:12:00Z", "stale": false, "error": null},
    {"id": "2", "name": "A家", "model_count": 4, "fetched_at": "2026-11-10T08:00:00Z", "stale": true, "error": "relay_unreachable"}
  ],
  "default_relay": "中转",
  "taken_over": ["openai_base_url"],
  "restore_blocked": [],
  "config_path": "/Users/x/.codex/config.toml",
  "update": {"latest_version": null, "url": null, "installer": false},
  "access": {"mode": "both", "target": "relay", "detect": "not_logged_in", "relay_only": false, "logged_in": false}
}

`update.url` 在 `installer` 为 true 时是本平台安装包（macOS `model-bridge-<版本>-macos.dmg`，Windows `model-bridge-<版本>-windows-setup.exe`）的下载地址；为 false 时是这个版本的发布页。

`relays` 按配置顺序列出每个中转配置这次的模型情况：`model_count` 是这次 Codex 里可用的模型数（获取失败时为沿用的缓存数，从没成功过为 0），`fetched_at` 是最近一次成功获取的时间（从没成功过为 null），`stale` 为 true 表示这次没获取成功，`error` 是失败原因的错误码。`default_relay` 是「只用中转」方式下默认模型所在配置的名字，没有任何配置有 auto 时为 null。

`access` 是接入方式：`mode` 为 Codex 配置当前按哪种方式写入（`both` 官方 + 中转，`relay` 只用中转；转发关着为 null），`target` 为按「只用中转」开关和最近一次检测得出的目标方式（转发开着且检测过之后，开关开着时总是 `relay`；没检测过为 null），`detect` 为最近一次检测结果（`logged_in`、`not_logged_in`、`failed`，没检测过为 null），`relay_only` 为「只用中转」开关是否开着，`logged_in` 为最近一次检测成功时是否已登录（从没检测成功过为 false）。

## 错误码
| 码 | 界面说明 |
|---|---|
| invalid_base_url | Base URL 要以 https:// 开头。 |
| name_empty | 请填写名字。 |
| name_too_long | 名字最多 16 个字。 |
| name_has_dot | 名字里不能有「·」。 |
| name_duplicate | 已有同名配置「<keys[0]>」。 |
| relay_limit | 已有 10 个，达到上限。 |
| relay_last | 至少保留一个配置。 |
| relay_not_found | 这个配置不存在，请刷新后重试。 |
| relay_401 | 中转服务返回 401：API Key 无效或已过期 |
| relay_403 | 中转服务返回 403：API Key 没有权限 |
| relay_404 | 中转服务返回 404：Base URL 不正确 |
| relay_429 | 中转服务返回 429：请求太频繁，请稍后重试 |
| relay_5xx | 中转服务返回 <keys[0]>：中转服务暂时不可用 |
| relay_4xx | 中转服务返回 <keys[0]>：请求未被中转服务接受 |
| relay_bad_response | 中转服务返回 <keys[0]>：没有返回模型列表，请检查 Base URL |
| relay_unreachable | 连不上中转服务，请检查网络和 Base URL |
| chatgpt_not_found | 没有找到 ChatGPT |
| chatgpt_unsupported | 当前 ChatGPT 版本暂不支持 |
| proxy_port_in_use | 本机转发服务没能启动：端口被占用 |
| proxy_start_timeout | 启动转发超过 30 秒还没完成，请重试 |
| restore_blocked | 没能自动还原 Codex 配置 |
| config_invalid | Codex 配置文件格式有误，未做修改 |
| config_changed | Codex 配置文件刚被其他程序修改，请重试 |
| update_unavailable | 暂时无法检查更新，请稍后再试 |
| internal_error | 内部错误，请重试 |
| unknown_method | 未知请求 |
| bad_params | 请求参数有误 |
