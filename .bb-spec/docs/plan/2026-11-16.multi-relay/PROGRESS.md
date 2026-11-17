# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | 01-core-relay-catalog | pending | — |
| 08 | 08-app-icon-assets | done | 2026-11-16 |
| 10 | 10-site-readme-rename | done | 2026-11-16 |
| 02 | 02-core-proxy-routing | done | 2026-11-16 |
| 03 | 03-core-control-relays | done | 2026-11-16 |
| 04 | 04-macos-relay-model | done | 2026-11-16 |
| 06 | 06-windows-relay-model | done | 2026-11-17 |
| 05 | 05-macos-main-window | done | 2026-11-17 |
| 07 | 07-windows-main-window | done | 2026-11-17 |
| 09 | 09-menubar-tray-icons | pending | — |
## 当前
2026-11-16 用户要求暂停。05、06、07 的代码都在工作区里（未提交），状态：
- 06（Windows 外壳模型）：实现与测试完成，复查 14/14 合规；最后一处「首次设置重置核心失败时提示」已改，随 07 一起等 CI 通过后提交。
- 07（Windows 主窗口）：最后一次 CI（run 43835731297）windows 396 个里 1 个失败：MainWindowLayoutTests.StatusRow_ShowsVersionCoreChatGptRelayModelsAndUpdate（MainWindowLayoutTests.cs:207 起的某条 Contains）。待查是测试还是实现。
- 05（macOS 主窗口）：本机全量 314 通过、复查 15/15 合规；但 CI 的 macOS 上 MultiRelayViewTests 大量失败：测试辅助 ViewHarness 用 KVC 读 accessibilityTitle，CI 的 macOS 版本不支持（NSUnknownKeyException）；MainWindowShellTests 真实窗口高 675（CI 系统标题栏高度与本机不同，交通灯重摆公式依赖标题栏高度）。需要：改测试辅助只读各系统都有的属性；窗口高度/交通灯位置改为不依赖系统标题栏高度的做法。
- 05 复查遗留（不阻塞，待做）：顶栏 32~48px 段拖动改用 performDrag；macOS 13~15 交通灯位置未验证；删除进行中点「退出/卸载/关闭登录启动」无反馈（建议带出主窗口）；sheet 无遮罩与 DESIGN.md「弹窗」要求不符，需用户决定补遮罩还是在 DESIGN.md 注明 macOS 用 sheet 代替。
- 之后还剩 09（菜单栏与托盘图标），以及收尾（全量测试、界面对比、doc-update、删 plan 目录）。
## 阻塞
（无）
## 重试与自修（每次记一条）
- 05-macos-main-window 自修第 1 轮：真实窗口标题栏多出 32px 且交通灯不在自绘顶栏、顶栏不能拖动；弹窗改为主窗口 sheet（居中+遮罩+拦住主窗口）；保存/删除进行中禁止再打开编辑或顶掉确认；删除确认焦点落「取消」；间距 2→4；「（N 个配置）」用补充说明样式；补真实窗口高度与弹窗测试 → 结果：通过，复查关闭全部 ❌（遮罩改为 DESIGN.md 例外，用户 2026-11-17 定）
- 04/06 外壳模型自修第 1 轮（两端同根因一起修）：首次设置撤回、撤回失败如实提示、删 Key 失败提示、卸载按前缀清 Key、迁移写回失败仍用迁移结果、Windows 删除进行中状态、设置原子写、PROTOCOL name_duplicate 说明移出文案 → 结果：
- 03-core-control-relays 自修第 1 轮：save/delete/refresh 在写 Codex 配置失败后磁盘目录与缓存未回滚；models_changed 对从没获取成功的配置恒为 false；init 未限 10 个；补 4 条测试 → 结果：已修，复查 12/12，新报备份还原不安全
- 03-core-control-relays 自修第 2 轮：备份时读失败的文件被误删、还原写盘非原子且错误被忽略 → 结果：已修（只记录读成功或确定不存在的文件、原子还原、还原失败可见并在成功后清除、Sync），复查 12/12 无 ❌
- 01-core-relay-catalog 自修第 1 轮：「何时联网」改为每个配置显式模式（联网/只读缓存/用传入列表）、多个配置并发获取、relay_test.go 残留「中台」、补测试（缓存不含 Key/URL、空列表、单配置刷新失败） → 结果：
- 10-site-readme-rename 自修第 1 轮：底部状态行缺标签+取值、版本号字体、添加配置按钮样式、菜单栏图标 18px；收紧 3 条区分度弱的断言 → 结果：三处已修、断言收紧，复查 10/10 合规
- 08-app-icon-assets 自修第 1 轮：DESIGN.md 单色图标规则与 PRD 04 C7（16px 托盘按 16px 规则）矛盾，按 PRD 改正 DESIGN.md 文字并补 64px；Scale 从公用 GlyphFor 挪到 RenderMono；补测试（单色放大、各档线宽、Win 88%、24/32/48、Mac 64） → 结果：已修，复查 17/17 合规
- 07-windows-main-window 自修第 1 轮：长的名字错误被裁掉（同根因：macOS 表单错误会换行撑高）；窗口圆角 8px 未设置；旧方法不存在的测试、ApiKeyHelp 双来源；配置行第二行 12px、状态区图标 16px、卡片间距 20px；表单弹窗未登记 Dialogs.Current、保存不设 busy、行按钮不看 busy、忙碌时 Alt+F4 可关弹窗；补 5 条测试遗漏 → 结果：通过，CI run 44001912479 全部通过，复查关闭 8/9；截断那条用户 2026-11-17 定为缩短重名提示文字，另行修改

## 无关发现（发现一条记一条）
- windows/ModelBridge.App/UI/MainWindow.xaml:42 版本号到状态圆点间距 12px，DESIGN.md 规定状态行各项之间 16px（旧代码）；不处理：间距略紧；约 5 分钟。
- core/internal/control/update_test.go 没有对应的 update.go（测试与实现一一对应规则），原有问题
（无）
