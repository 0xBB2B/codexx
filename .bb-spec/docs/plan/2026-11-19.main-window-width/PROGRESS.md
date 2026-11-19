# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | 01-macos-windows | done | 2026-11-19 |
| 02 | 02-windows-windows | done | 2026-11-19 |
| 03 | 03-site-figure | done | 2026-11-19 |
## 当前
全部完成，进入收尾。
## 阻塞
（无）
## 重试与自修（每次记一条）
- 03-site-figure 实现重试第 1 次：旧测试 TestSite_NarrowScreenZoomOverridesLaterThan600 写死 .46，主窗口改为 .37 → 通过
- 01-macos-windows 实现重试第 1 次：测试按"包含文字"找到 44 高的新版本按钮，主窗口改为按按钮宽度判断单行 → 21/21 通过
- 02-windows-windows 自修第 1 轮：WindowChrome 边缘仍显示改尺寸光标，ResizeBorderThickness 改 0；补 3 条断言（边框为 0、检查更新的可访问名称、状态区左侧不超宽不重叠）→ CI 运行 44450750113 通过，复查关闭
## 无关发现（发现一条记一条）
（无）
