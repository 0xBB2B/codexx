# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | 01-core-relay-catalog | pending | — |
| 08 | 08-app-icon-assets | done | 2026-11-16 |
| 10 | 10-site-readme-rename | done | 2026-11-16 |
| 02 | 02-core-proxy-routing | done | 2026-11-16 |
| 03 | 03-core-control-relays | done | 2026-11-16 |
| 04 | 04-macos-relay-model | pending | — |
| 06 | 06-windows-relay-model | pending | — |
| 05 | 05-macos-main-window | pending | — |
| 07 | 07-windows-main-window | pending | — |
| 09 | 09-menubar-tray-icons | pending | — |
## 当前
准备并行执行 `01-core-relay-catalog.md`、`08-app-icon-assets.md`、`10-site-readme-rename.md`。
## 阻塞
（无）
## 重试与自修（每次记一条）
- 03-core-control-relays 自修第 1 轮：save/delete/refresh 在写 Codex 配置失败后磁盘目录与缓存未回滚；models_changed 对从没获取成功的配置恒为 false；init 未限 10 个；补 4 条测试 → 结果：已修，复查 12/12，新报备份还原不安全
- 03-core-control-relays 自修第 2 轮：备份时读失败的文件被误删、还原写盘非原子且错误被忽略 → 结果：已修（只记录读成功或确定不存在的文件、原子还原、还原失败可见并在成功后清除、Sync），复查 12/12 无 ❌
- 01-core-relay-catalog 自修第 1 轮：「何时联网」改为每个配置显式模式（联网/只读缓存/用传入列表）、多个配置并发获取、relay_test.go 残留「中台」、补测试（缓存不含 Key/URL、空列表、单配置刷新失败） → 结果：
- 10-site-readme-rename 自修第 1 轮：底部状态行缺标签+取值、版本号字体、添加配置按钮样式、菜单栏图标 18px；收紧 3 条区分度弱的断言 → 结果：三处已修、断言收紧，复查 10/10 合规
- 08-app-icon-assets 自修第 1 轮：DESIGN.md 单色图标规则与 PRD 04 C7（16px 托盘按 16px 规则）矛盾，按 PRD 改正 DESIGN.md 文字并补 64px；Scale 从公用 GlyphFor 挪到 RenderMono；补测试（单色放大、各档线宽、Win 88%、24/32/48、Mac 64） → 结果：已修，复查 17/17 合规
## 无关发现（发现一条记一条）
- core/internal/control/update_test.go 没有对应的 update.go（测试与实现一一对应规则），原有问题
（无）
