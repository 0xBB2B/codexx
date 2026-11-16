# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | 01-site-checker | done | 2026-11-16 |
| 02 | 02-site-page | pending | — |
| 03 | 03-pages-workflow | pending | — |
## 当前
并行执行 `02-site-page.md` 和 `03-pages-workflow.md`。
## 阻塞
（无）
## 重试与自修（每次记一条）
- 01-site-checker 自修第 1 轮：github 仓库子路径没放行（check.go:91）、网址后紧跟标点被误报（check.go:23）、补 main 退出码测试 → 结果：三处已修、测试通过；复查发现修法带来漏检（非 ASCII 截断可绕过主机检查、路径 .. 绕过前缀），进第 2 轮
- 01-site-checker 自修第 2 轮：非 ASCII 字符位置判定 + 路径 .. 判违规 → 结果：上一轮 3 条已关闭；复查新报 1 条 PRD 违规（. / .. 检查误伤 127.0.0.1、relay 路径）和 3 条故意构造的绕过，2 轮用完，标 blocked 等用户裁决；用户 2026-11-16 选"只防误写"，修好 . / .. 只查 github 后通过
## 无关发现（发现一条记一条）
- scripts/sitecheck/check.go:29 只认 http(s)://，`https:\\evil.org/` 这种反斜杠写法浏览器能打开但检查不到；PRD 只要求查 http(s)://，扩大范围要另定
