# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | repo-scaffold | done | 2026-11-11 |
| 02 | core-model-catalog | done | 2026-11-11 |
| 03 | core-proxy-routing | done | 2026-11-11 |
| 04 | core-request-normalize | done | 2026-11-11 |
| 05 | core-codex-config | done | 2026-11-11 |
| 06 | core-control | pending | — |
| 07 | core-update-check | pending | — |
| 08 | macos-system | pending | — |
| 09 | macos-ui | pending | — |
| 10 | windows-system | pending | — |
| 11 | windows-ui | pending | — |
| 12 | windows-installer | pending | — |
| 13 | release-pipeline | pending | — |
## 当前
下一步：`06-core-control`。
- 01 记录：Review 发现 Windows 冒烟测试断言写错、App 项目写死自包含会触发 NETSDK1151（依据 .NET SDK 源码报错原文，未实测），已由主流程修正；计划 01、13 同步改为发布时再指定 `-r win-x64 --self-contained`。修正后由主流程复核，未重派 Review。
- 02 记录：Review 合规 17/19、0 违规；按意见自修一次（记录损坏/保存失败降级、默认等级校验、回传获取时间、8MB 上限、负 priority、Windows 环境变量大小写、补 4xx/非法正文测试），修后由主流程复跑通过，未重派 Review。参数表目前只有 auto，其他主流模型待调研结果补充。
- 03 记录：Review 合规 17/19；按意见自修一次——安全：中台 trailer 原文泄漏、未挡 DNS 重绑定（加 Host 白名单、拒绝带 Origin 的请求）、路径 `..` 未规范化；鲁棒：上游响应头 60 秒超时、坏 zstd 改为原样转发官方、Base URL 解析失败返回规定 JSON、ErrorLog 改写注入日志。实现方未先跑 Red，主流程用变异检查确认三项安全测试能拦住回退。
- 05 记录：首轮 Review 合规 15/17，自修一轮（存原文实现逐字节还原、先存状态后换配置、重复开启的接管、符号链接、权限、原子备份、写入前重读比较、类型校验）；独立复审 7/8 修好，剩悬空符号链接和 auto 下架语义。经用户同意再修一轮，并新增 PRD 03 C11（auto 下架时还原 model）；第三轮先 Red 后 Green，116 个用例通过。已知未处理：`[[desktop]]` 数组表写法会在写备份后才报 config_invalid（配置不变）。
- 04 记录：Review 合规 15/18、0 安全问题；对照 Codex 源码确认压缩返回格式可被识别。自修一轮（先 Red 后 Green）：context_compaction 不发中台、总结请求单独 10 分钟等头超时、只有 data 行的 SSE 也记推理 id、官方请求只替换 input 段其余逐字节保留、字符串 input 只整理 tools。旧用例 `/v1/responses/compact` 多级路径改为 `/v1/responses/input_tokens`（compact 已由本工具接管）。未验证：官方 message/function_call 条目带的 encrypted 字段发到中台是否被拒，需用真实中台回放。
- 01 的 Windows 部分（.sln/.csproj 能否编译、冒烟测试）尚未在 Windows 上验证，等 GitHub 仓库建好后用 CI 验证；验证通过前不开始 10-windows-system。
## 阻塞
（无）
