---
name: 04-macos-relay-model
description: macOS 外壳保存多个中转配置（设置 + 钥匙串），升级迁移，界面模型里的配置列表逻辑和「中转」文案
---
# macOS：多配置的保存、迁移与界面模型

## 目标
macOS 外壳能保存、读取、迁移多个中转配置，`AppModel` 提供配置列表、添加、编辑、删除、重试的逻辑和所有界面文案（界面布局由另一个 plan 做）。

## 业务规则（来源：PRD 03-multi-relay-config、01-rename-relay、02-main-window）
- 每个配置：编号、名字、Base URL 存在 `shell-settings.json` 的配置列表里（按顺序）；API Key 存在钥匙串。编号 "1" 的 Key 用原来的条目（account `relay-api-key`），编号 N 用 `relay-api-key-<N>`。普通文件、日志、错误信息里不出现 Key。
- 升级迁移：设置里只有旧的单个 `base_url` 时，变成一个配置：编号 "1"、名字「中转」、原 Base URL，Key 沿用原钥匙串条目，不需要用户重新填写；迁移后写回新格式。
- 启动：把所有配置（含 Key）通过 `init` 传给核心程序。没有任何配置时打开首次设置窗口。
- 本地校验（和核心同规则）：名字去首尾空格后 1~16 个字（超过时允许继续输入，保存时报「名字最多 16 个字，现在是 N 个。」）、不能含「·」（「名字里不能有「·」。」）、不能为空（「请填写名字。」）、不区分大小写不能和其他配置同名（「已有叫「X」的配置（不区分大小写），换一个名字。」）；Base URL 为空「请填写 Base URL。」、不是 https「Base URL 要以 https:// 开头。」；Key 为空「请填写 API Key。」错误显示在对应栏下，取代帮助文字。
- 添加/编辑：本地校验通过后调用 `save_relay`；成功后先存 Key、再存设置（任一步失败要回滚并提示保存失败）；弹窗关闭，主窗口浮出成功提示：新增「已添加「名字」」「已获取 N 个模型。重启 ChatGPT 后生效。」，编辑「已保存「名字」」「重启 ChatGPT 后生效。」。核心返回失败：不保存，弹窗留着，弹窗上方浮出红色「没有保存」+ 原因（错误码说明，401 时为「中转服务返回 401：API Key 无效或已过期。已填的内容都还在，改好后再保存。」）。保存中三栏和「取消」不可用，按钮显示「正在保存并获取模型…」。
- 删除：只剩一个时不可删（说明「至少保留一个配置。」）；确认弹窗标题「删除配置「名字」？」，正文「删除后：· Codex 的模型列表里不再有「名字」的 N 个模型 · 它的 API Key 会从系统钥匙串里删除」，另起一行「重启 ChatGPT 后生效。」，按钮「取消」「删除」；确认后调 `delete_relay`，再删设置项和钥匙串条目；成功浮出「已删除「名字」」「重启 ChatGPT 后生效。」；写设置失败浮出红色「没有删除「名字」」+「无法写入设置文件。」，列表不变。正在删除时「取消」不可用，按钮显示「正在删除…」。
- 列表下说明：「已有 N 个，最多 10 个。」；10 个时「已有 10 个，达到上限。」且「添加配置」不可用。
- 每行状态文字：正常「N 个模型」；没更新「没更新，沿用上次的 N 个」+「重试」；从没成功「还没获取到模型」+「重试」；重试中「正在获取…」（该行编辑、删除不可用）；转发关着「转发关闭时不获取」。原来的两条全局提示（模型列表没更新、还没获取到模型）去掉。
- 转发总览文字：标题「使用中转 · 已开启 / 已关闭」；说明（官方 + 中转）「官方模型在上，中转模型在下；选官方模型时仍访问官方服务。」；（只用中转）「不用登录 ChatGPT。只有中转模型，默认选中「<default_relay> · auto」。」，default_relay 为空时「不用登录 ChatGPT。只有中转模型，没有默认选中的模型。」；关着「已关闭：Codex 配置已还原为开启前的样子。」方式名「官方 + 中转」「只用中转」。底部「中转模型 N 个（M 个配置）」或「中转模型 转发关闭时不获取」。
- 首次设置：多「名字」栏默认「中转」；大标题「连接中转服务」；说明「填写中转服务提供的 Base URL 和 API Key。设置完成后，Codex 的模型列表里会多出中转模型。」；连接中「正在连接中转…」；失败标题「没能连上中转服务」；完成页文字里的「中台」换「中转」。
- 其余所有界面文案（菜单「使用中转」、卸载、退出、提示）里的「中台」换「中转」；「转发」两字保留。

## 涉及文件
- 修改 `macos/Sources/ModelBridge/System/ShellSettings.swift`、`KeychainStore.swift`、`CoreProtocol.swift`
- 修改 `macos/Sources/ModelBridge/UI/AppModel.swift`、`DialogModels.swift`、`MenuPanelModel.swift`
- 修改对应测试：`ShellSettingsTests.swift`、`KeychainStoreTests.swift`、`CoreProtocolTests.swift`、`AppModelTests.swift`、`DialogModelTests.swift`、`MenuPanelModelTests.swift`

## 函数清单
### System/ShellSettings.swift
| 函数名 | 职责 |
|---|---|
| `RelayEntry` | 编号、名字、Base URL |
| `ShellSettings.relays` / `saveRelays` | 读写配置列表；读到旧格式时迁移成编号 "1"「中转」 |
### System/KeychainStore.swift
| 函数名 | 职责 |
|---|---|
| `save` / `load` / `delete` | 改为按配置编号区分条目（编号 "1" 用原条目名） |
### System/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `RelayEntryStatus`、`Status.relays`、`Status.defaultRelay` | 解码新状态 |
| `errorMessages` | 新错误码和「中转」文案 |
### UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `relayRows` | 列表每行的名字、地址、状态文字、可用按钮 |
| `beginAdd` / `beginEdit` / `saveRelayForm` | 弹窗表单的打开、校验、保存 |
| `validateRelayForm` | 本地校验规则 |
| `beginDelete` / `confirmDelete` | 删除流程 |
| `retryRelay` | 单个配置重试 |
| `forwardingTitle` / `accessModeHelp` / `statusLineText` | 转发总览和底部文字 |
| 首次设置相关 | 加名字栏、默认「中转」 |
### UI/DialogModels.swift
| 函数名 | 职责 |
|---|---|
| `forDeleteRelay` | 删除确认弹窗内容 |

## 协作关系
`AppModel` → `CoreProcess`（`init`、`save_relay`、`delete_relay`、`refresh_models`）；`AppModel` → `ShellSettings`、`KeychainStore`。界面（plan 05）只读 `AppModel` 的状态和调用它的动作。

## 验证方式
- 测试入口：`cd macos && swift test`（只跑本 plan 相关测试类）；包级入口 `AppModel`（用测试替身代替核心进程、钥匙串、设置文件，项目现有测试已有替身写法）。
- 预期结果：
  - [ ] 旧设置文件只有 `base_url` → 读出一个配置（"1"、「中转」、原地址），Key 从原条目读到；写回后是新格式。
  - [ ] 新增「A家」成功 → 钥匙串多一条 `relay-api-key-<新编号>`，设置里多一项，成功提示文字正确。
  - [ ] 本地校验的 6 种错误文字正确；核心返回 relay_401 → 弹窗不关、浮出规定红色提示、设置和钥匙串不变。
  - [ ] 删除：只剩一个时不可删；确认弹窗文字正确；成功后钥匙串条目和设置项都删掉；写设置失败时提示「没有删除「名字」」且列表不变。
  - [ ] 行状态文字五种情况正确；10 个时不能添加。
  - [ ] 转发总览、底部状态行在两种方式、default_relay 为空、转发关闭时的文字正确。
  - [ ] macOS 源码和测试里搜不到「中台」。
