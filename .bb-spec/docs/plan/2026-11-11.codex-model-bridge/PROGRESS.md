# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | repo-scaffold | done | 2026-11-11 |
| 02 | core-model-catalog | pending | — |
| 03 | core-proxy-routing | pending | — |
| 04 | core-request-normalize | pending | — |
| 05 | core-codex-config | pending | — |
| 06 | core-control | pending | — |
| 07 | core-update-check | pending | — |
| 08 | macos-system | pending | — |
| 09 | macos-ui | pending | — |
| 10 | windows-system | pending | — |
| 11 | windows-ui | pending | — |
| 12 | windows-installer | pending | — |
| 13 | release-pipeline | pending | — |
## 当前
下一组并行：`02-core-model-catalog`、`03-core-proxy-routing`、`05-core-codex-config`。
- 01 记录：Review 发现 Windows 冒烟测试断言写错、App 项目写死自包含会触发 NETSDK1151（依据 .NET SDK 源码报错原文，未实测），已由主流程修正；计划 01、13 同步改为发布时再指定 `-r win-x64 --self-contained`。修正后由主流程复核，未重派 Review。
- 01 的 Windows 部分（.sln/.csproj 能否编译、冒烟测试）尚未在 Windows 上验证，等 GitHub 仓库建好后用 CI 验证；验证通过前不开始 10-windows-system。
## 阻塞
（无）
