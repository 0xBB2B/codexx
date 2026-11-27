---
name: macos-account-notice
description: macOS 收到 official_models_revision 变大时显示不自动消失的成功提示，关转发时移除
---
# macOS：官方模型更新提示
## 目标
macOS 主窗口在官方模型按账号更新后显示一条不自动消失的成功提示。
## 业务规则（来源：PRD 01-account-model-sync，已确认原型）
- core 推来的 status 里 `official_models_revision`（整数，从 0 开始）比 App 上一次收到的值大时，加一条成功提示：标题「官方模型已按当前账号更新」，正文「重启 ChatGPT 后生效。」，没有操作按钮（约束 6）。
- App 收到的第一份 status 只记下这个值，不弹提示（约束 9）；值变小（core 重启）时只更新记录，不弹。
- 这条提示不自动消失，点 ✕ 消失；同时最多一条，新的替换旧的、位置不变（按现有"标题和级别相同视为同一条"）；转发变成关着时一起移除（约束 7）。
- 其他成功、信息提示照旧 4 秒消失；和它叠放时按出现先后从上往下排、间距 8；「已保存「中转」」4 秒消失后它不动。
- 样子、位置沿用现有成功提示条：主窗口左上角，距标题栏下沿 12、左 24，宽 376，不盖按钮和开关。
## 涉及文件
- macos/Sources/ModelBridge/System/CoreProtocol.swift（修改）
- macos/Sources/ModelBridge/UI/AppModel.swift（修改）
## 函数清单
### macos/Sources/ModelBridge/System/CoreProtocol.swift
| 函数名 | 职责 |
|---|---|
| `CoreStatus.officialModelsRevision`（新字段） | 解码 `official_models_revision`，缺省为 0 |
### macos/Sources/ModelBridge/UI/AppModel.swift
| 函数名 | 职责 |
|---|---|
| `AppModel.apply`（修改） | 比较 revision，变大时加提示；转发变关时移除这条提示 |
| `AppModel.addNotice`（修改） | 支持不启动自动消失计时的成功提示 |
## 协作关系
CoreProcess status 事件 → AppModel.apply → addNotice / removeNotices(titled:) → NoticeStack 渲染。
## 验证方式
- 测试入口：AppModel（FakeCore 推 status / 直接设置 status 的现有测试方式）、NoticePositionTests 的窗口测量方式。
- 测试输入：依次推 revision 0、1、1、2 的 status；推 forwarding=false 的 status；期间保存一次中转配置。
- 预期结果：
- [ ] 第一份 status（无论 revision 多少）不出提示；0→1 出提示，标题、正文、无按钮符合规则
- [ ] noticeAutoDismissDelay 过后提示仍在；dismissNotice 后消失
- [ ] 1→2 时仍只有一条，位置不变
- [ ] forwarding 变 false 时这条提示被移除
- [ ] 与「已保存「中转」」叠放：它在上、间距 8；后者自动消失后它的位置不变；都不盖住主窗口按钮和开关
- [ ] status 不含该字段时按 0 解码
