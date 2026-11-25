# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | 01-core-relay-only | done | 2026-11-25 |
| 02 | 02-macos-relay-only-switch | pending | — |
| 03 | 03-windows-relay-only-switch | pending | — |
## 当前
并行组：`02-macos-relay-only-switch.md`（Red）、`03-windows-relay-only-switch.md`（Red）。
## 阻塞
（无）
## 重试与自修（每次记一条）
- 01-core-relay-only 自修第 1 轮：SetRelayOnly 保存设置失败时不回滚；补 3 条测试缺口；PROTOCOL.md target 说明不准；测试文件并入 service_test.go —— 通过，复查无 ❌
## 无关发现（发现一条记一条）
- macOS 主窗口：转发关掉再打开后窗口整体变高，标题栏下和底部多出空白（用户 2026-11-25 截图报告，排在本需求之后用 /revise 修）
- core/internal/control/service.go 的 ApplyAccess、SetForwarding(true/false)、detect：保存 settings.json 失败时内存状态和 Codex 配置已改、不回滚（审查 2026-11-25 发现，老代码，未验证实际触发频率；不处理的后果：磁盘满或没权限时下次启动配置被悄悄改回；估计 1 小时）
- 换不同等级的 ChatGPT 账号（Go / Plus）后，合并模型目录的官方模型没跟着更新，选到用不了的模型显示「此模型不可用」（用户 2026-11-25 报告，排在窗口问题之后用 /revise 修）
