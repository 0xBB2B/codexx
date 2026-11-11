---
name: 06-core-control
description: 核心程序与外壳之间的控制协议（标准输入输出逐行 JSON）和核心主流程
---
# 核心：控制协议与主流程

## 目标
外壳启动核心程序后，通过标准输入输出完成初始化、开关转发、保存设置、刷新模型、退出和卸载前还原；核心把状态变化推送给外壳。

## 业务规则（来源：PRD 01、02、03、04、05）
- 中台 Key 只经标准输入在 `init`、`save_connection` 里交给核心，只存内存；不出现在命令行参数、环境变量、日志、错误信息里（AGENTS.md）。
- 控制只走父进程持有的管道，本机其他程序无法控制核心。父进程管道关闭（外壳崩溃）→ 核心按 `shutdown` 处理但不还原配置（外壳下次启动会重新接管）。
- 启动（init）：恢复转发状态；转发开着时 → 启动转发服务 → 生成目录（02）→ 确保接入项已写入（05，入口已是本工具写入值时不重复接管）。转发关着时只检测 ChatGPT 是否存在。
- `save_connection`：先用新地址和新 Key 拉中台列表；失败 → 返回错误码，什么都不改；成功 → 替换内存里的地址和 Key，重新生成目录，返回中台模型集合是否变化。外壳负责把 Key 写进系统凭据库、把 Base URL 写进本机设置。
- `set_forwarding true`：找不到 codex 或官方清单获取失败 → 返回 chatgpt_not_found / chatgpt_unsupported，不写配置。成功 → 启动转发服务、生成目录、EnsureInitialBackup、Enable。
- `set_forwarding false`：Restore；有手改项 → 返回 restore_blocked 和键名，转发状态仍算关闭、转发服务停止。
- `prepare_uninstall`：RestoreAllOrNothing；成功后删除数据目录里本工具生成的文件（合并目录、状态、记录、日志）。
- `shutdown`：`restore: true` 时先 Restore 再退出；退出前停止转发服务。
- 转发状态持久化在数据目录 `settings.json`。

## 涉及文件
- core/PROTOCOL.md（新建，协议唯一权威说明）
- core/internal/control/protocol.go（新建）
- core/internal/control/service.go（新建）
- core/internal/control/settings.go（新建）
- core/internal/control/*_test.go（新建，与实现文件一一对应）
- core/cmd/model-bridge-core/main.go（修改：进入控制循环）

## 成品定义

`core/PROTOCOL.md` 正文（原样落盘）：
```markdown
# 核心程序控制协议

外壳以子进程启动 `model-bridge-core --data-dir <数据目录>`，通过标准输入输出通信。每行一个 JSON 对象，UTF-8，以 `\n` 结尾。标准错误输出不用于通信。

## 请求（外壳 → 核心）
{"id": <整数>, "method": "<方法名>", "params": { ... }}

## 响应（核心 → 外壳）
{"id": <同请求>, "result": { ... }}
{"id": <同请求>, "error": {"code": "<错误码>", "keys": ["<键名>", ...]}}
`keys` 只在 restore_blocked 时出现。

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
| relay_unreachable | 连不上中台，请检查网络和 Base URL |
| chatgpt_not_found | 没有找到 ChatGPT |
| chatgpt_unsupported | 当前 ChatGPT 版本暂不支持 |
| proxy_port_in_use | 本机转发服务没能启动：端口被占用 |
| restore_blocked | 没能自动还原 Codex 配置 |
| config_invalid | Codex 配置文件格式有误，未做修改 |
| update_unavailable | 暂时无法检查更新，请稍后再试 |
```

`settings.json`（数据目录，核心维护）：
```json
{ "forwarding": true }
```

## 函数清单
### core/internal/control/protocol.go
| 函数名 | 职责 |
|---|---|
| `ReadRequests` | 逐行读取并解析请求，管道关闭时结束 |
| `Writer` | 并发安全地逐行写响应和事件 |
### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `Service` | 持有转发服务、路由状态、目录、配置操作、设置 |
| `Service.Handle` | 按方法名分派，返回 result 或 error |
| `Service.Init` / `SaveConnection` / `SetForwarding` / `RefreshModels` / `PrepareUninstall` / `Shutdown` | 按「业务规则」执行各方法 |
| `Service.rebuildCatalog` | 获取官方清单和中台列表、生成目录、更新路由状态和 relay 状态 |
| `Service.emitStatus` | 状态变化时推送 status 事件 |
### core/internal/control/settings.go
| 函数名 | 职责 |
|---|---|
| `LoadSettings` / `SaveSettings` | 读写 settings.json |
### core/cmd/model-bridge-core/main.go
| 函数名 | 职责 |
|---|---|
| `main` | 解析 --data-dir，建 Service，进入请求循环，循环结束时按管道关闭处理 |

## 协作关系
外壳 ⇄ main（标准输入输出）→ Service → catalog（02）、proxy（03/04）、codexconfig（05）、update（07）。

## 验证方式
- 测试入口：`cd core && go test ./internal/control/...`，以及编译后的进程级测试（测试里启动 model-bridge-core 子进程，通过管道收发 JSON）
- 测试输入：临时数据目录、临时 CODEX_HOME、假 codex 脚本、假中台
- 预期结果：
- [ ] init（转发开）→ 返回 Status：proxy.running 为 true，配置里有接入项，合并目录存在
- [ ] save_connection 用会返回 401 的 Key → 错误 relay_401，内存里旧 Key 仍可用于转发
- [ ] save_connection 让中台列表多一个参数表内模型 → models_changed 为 true
- [ ] set_forwarding true 而 codex 不存在 → chatgpt_not_found，配置文件未变
- [ ] set_forwarding false 遇手改项 → restore_blocked 和键名
- [ ] prepare_uninstall 遇手改项 → restore_blocked，数据目录文件都还在；无手改 → 配置还原、数据目录里本工具文件被删除
- [ ] 关闭标准输入 → 进程在 5 秒内退出，配置未被还原
- [ ] 进程运行期间 `ps` 输出的命令行和环境变量里没有 Key；日志里也没有
