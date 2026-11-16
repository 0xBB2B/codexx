---
name: 06-windows-relay-model
description: Windows 外壳保存多个中转配置（设置 + 凭据管理器），升级迁移，界面模型里的配置列表逻辑和「中转」文案
---
# Windows：多配置的保存、迁移与界面模型

## 目标
Windows 外壳能保存、读取、迁移多个中转配置，`AppModel` 提供配置列表、添加、编辑、删除、重试的逻辑和所有界面文案（界面布局由另一个 plan 做）。

## 业务规则（来源：PRD 03-multi-relay-config、01-rename-relay、02-main-window）
- 每个配置：编号、名字、Base URL 存在 `shell-settings.json` 的配置列表里（按顺序）；API Key 存在 Windows 凭据管理器。编号 "1" 用原来的目标名 `ModelBridge/relay-api-key`，编号 N 用 `ModelBridge/relay-api-key-<N>`。普通文件、日志、错误信息里不出现 Key。
- 升级迁移：设置里只有旧的单个 `base_url` 时，变成一个配置：编号 "1"、名字「中转」、原 Base URL，Key 沿用原凭据，不需要重新填写；迁移后写回新格式。
- 启动：把所有配置（含 Key）通过 `init` 传给核心程序。没有任何配置时打开首次设置窗口。
- 本地校验（和核心同规则）：名字去首尾空格后 1~16 个字（超过时允许继续输入，保存时报「名字最多 16 个字，现在是 N 个。」）、不能含「·」（「名字里不能有「·」。」）、不能为空（「请填写名字。」）、不区分大小写不能和其他配置同名（「已有叫「X」的配置（不区分大小写），换一个名字。」）；Base URL 为空「请填写 Base URL。」、不是 https「Base URL 要以 https:// 开头。」；Key 为空「请填写 API Key。」错误显示在对应栏下，取代帮助文字。
- 添加/编辑：本地校验通过后调用 `save_relay`；成功后先存 Key、再存设置（任一步失败回滚并提示保存失败）；弹窗关闭，主窗口浮出成功提示：新增「已添加「名字」」「已获取 N 个模型。重启 ChatGPT 后生效。」，编辑「已保存「名字」」「重启 ChatGPT 后生效。」。核心返回失败：不保存，弹窗留着，弹窗上方浮出红色「没有保存」+ 原因（401 时「中转服务返回 401：API Key 无效或已过期。已填的内容都还在，改好后再保存。」）。保存中三栏和「取消」不可用，按钮显示「正在保存并获取模型…」。API Key 帮助文字「保存在系统凭据管理器中。」
- 删除：只剩一个时不可删（「至少保留一个配置。」）；确认弹窗标题「删除配置「名字」？」，正文「删除后：· Codex 的模型列表里不再有「名字」的 N 个模型 · 它的 API Key 会从系统凭据管理器里删除」，另起一行「重启 ChatGPT 后生效。」，按钮「取消」「删除」；确认后调 `delete_relay`，再删设置项和凭据；成功浮出「已删除「名字」」「重启 ChatGPT 后生效。」；写设置失败浮出红色「没有删除「名字」」+「无法写入设置文件。」，列表不变。正在删除时「取消」不可用，按钮显示「正在删除…」。
- 列表下说明：「已有 N 个，最多 10 个。」；10 个时「已有 10 个，达到上限。」且「添加配置」不可用。
- 每行状态文字：正常「N 个模型」；没更新「没更新，沿用上次的 N 个」+「重试」；从没成功「还没获取到模型」+「重试」；重试中「正在获取…」（该行编辑、删除不可用）；转发关着「转发关闭时不获取」。原来的两条全局提示去掉。
- 转发总览文字：标题「使用中转 · 已开启 / 已关闭」；说明（官方 + 中转）「官方模型在上，中转模型在下；选官方模型时仍访问官方服务。」；（只用中转）「不用登录 ChatGPT。只有中转模型，默认选中「<default_relay> · auto」。」，default_relay 为空时「不用登录 ChatGPT。只有中转模型，没有默认选中的模型。」；关着「已关闭：Codex 配置已还原为开启前的样子。」方式名「官方 + 中转」「只用中转」。底部「中转模型 N 个（M 个配置）」或「中转模型 转发关闭时不获取」。
- 首次设置：多「名字」栏默认「中转」；大标题「连接中转服务」；说明「填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。」；连接中「正在连接中转…」；失败标题「没能连上中转服务」；完成页写「任务栏通知区域」，其余文字里的「中台」换「中转」。
- 其余所有界面文案（托盘菜单「使用中转」、卸载、退出、提示）里的「中台」换「中转」；「转发」两字保留。

## 涉及文件
- 修改 `windows/ModelBridge.App/System/ShellSettings.cs`、`CredentialStore.cs`、`CoreProtocol.cs`
- 修改 `windows/ModelBridge.App/UI/AppModel.cs`、`DialogModel.cs`、`TrayPanelModel.cs`、`Adapters.cs`（如涉及）
- 修改对应测试：`ShellSettingsTests.cs`、`CredentialStoreTests.cs`、`CoreProtocolTests.cs`、`AppModelTests.cs`、`DialogModelTests.cs`、`TrayPanelModelTests.cs`、`AccessModeTests.cs`

## 函数清单
### System/ShellSettings.cs
| 函数名 | 职责 |
|---|---|
| `RelayEntry` | 编号、名字、Base URL |
| `Relays` / `SaveRelays` | 读写配置列表；读到旧格式时迁移 |
### System/CredentialStore.cs
| 函数名 | 职责 |
|---|---|
| `Save` / `Load` / `Delete` | 按配置编号区分目标名 |
### System/CoreProtocol.cs
| 函数名 | 职责 |
|---|---|
| `RelayEntryStatus`、`Status.Relays`、`Status.DefaultRelay` | 解码新状态 |
| `ErrorMessages` | 新错误码和「中转」文案 |
### UI/AppModel.cs
| 函数名 | 职责 |
|---|---|
| `RelayRows` | 列表行数据 |
| `BeginAdd` / `BeginEdit` / `SaveRelayFormAsync` / `ValidateRelayForm` | 表单流程 |
| `BeginDelete` / `ConfirmDeleteAsync` | 删除流程 |
| `RetryRelayAsync` | 单个重试 |
| `ForwardingTitle` / `AccessModeHelp` / `StatusLineText` | 总览和底部文字 |
### UI/DialogModel.cs
| 函数名 | 职责 |
|---|---|
| `ForDeleteRelay` | 删除确认内容 |

## 协作关系
`AppModel` → `CoreProcess`（`init`、`save_relay`、`delete_relay`、`refresh_models`）；`AppModel` → `ShellSettings`、`CredentialStore`。

## 验证方式
- 测试入口：只能在 CI（windows-2025）上跑：`dotnet test windows/ModelBridge.sln --filter <本 plan 相关测试类>`；本机只做 `dotnet build windows/ModelBridge.sln -p:EnableWindowsTargeting=true` 编译检查。推到临时分支 `ci/windows-wip` 前须征得用户同意，用完删除。
- 预期结果：同 macOS 版：旧设置迁移；新增后凭据管理器多一条 `ModelBridge/relay-api-key-<编号>`；6 种本地校验文字；核心 401 时弹窗不关；删除流程和失败提示；行状态五种；总览与底部文字；Windows 源码和测试里搜不到「中台」。
