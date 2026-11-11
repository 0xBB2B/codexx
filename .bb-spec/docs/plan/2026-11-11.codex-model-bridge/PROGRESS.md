# 执行进度
| 序号 | Plan | 状态 | 完成时间 |
|---|---|---|---|
| 01 | repo-scaffold | done | 2026-11-11 |
| 02 | core-model-catalog | done | 2026-11-11 |
| 03 | core-proxy-routing | done | 2026-11-11 |
| 04 | core-request-normalize | done | 2026-11-11 |
| 05 | core-codex-config | done | 2026-11-11 |
| 06 | core-control | done | 2026-11-11 |
| 07 | core-update-check | done | 2026-11-11 |
| 08 | macos-system | done | 2026-11-11 |
| 09 | macos-ui | pending | — |
| 10 | windows-system | pending | — |
| 11 | windows-ui | pending | — |
| 12 | windows-installer | pending | — |
| 13 | release-pipeline | pending | — |
## 当前
下一组：`07-core-update-check`、`08-macos-system`（并行）；`10-windows-system` 等 GitHub 仓库和 01 的 Windows CI 验证。
- 01 记录：Review 发现 Windows 冒烟测试断言写错、App 项目写死自包含会触发 NETSDK1151（依据 .NET SDK 源码报错原文，未实测），已由主流程修正；计划 01、13 同步改为发布时再指定 `-r win-x64 --self-contained`。修正后由主流程复核，未重派 Review。
- 02 记录：Review 合规 17/19、0 违规；按意见自修一次（记录损坏/保存失败降级、默认等级校验、回传获取时间、8MB 上限、负 priority、Windows 环境变量大小写、补 4xx/非法正文测试），修后由主流程复跑通过，未重派 Review。参数表目前只有 auto，其他主流模型待调研结果补充。
- 03 记录：Review 合规 17/19；按意见自修一次——安全：中台 trailer 原文泄漏、未挡 DNS 重绑定（加 Host 白名单、拒绝带 Origin 的请求）、路径 `..` 未规范化；鲁棒：上游响应头 60 秒超时、坏 zstd 改为原样转发官方、Base URL 解析失败返回规定 JSON、ErrorLog 改写注入日志。实现方未先跑 Red，主流程用变异检查确认三项安全测试能拦住回退。
- 05 记录：首轮 Review 合规 15/17，自修一轮（存原文实现逐字节还原、先存状态后换配置、重复开启的接管、符号链接、权限、原子备份、写入前重读比较、类型校验）；独立复审 7/8 修好，剩悬空符号链接和 auto 下架语义。经用户同意再修一轮，并新增 PRD 03 C11（auto 下架时还原 model）；第三轮先 Red 后 Green，116 个用例通过。已知未处理：`[[desktop]]` 数组表写法会在写备份后才报 config_invalid（配置不变）。
- 04 记录：Review 合规 15/18、0 安全问题；对照 Codex 源码确认压缩返回格式可被识别。自修一轮（先 Red 后 Green）：context_compaction 不发中台、总结请求单独 10 分钟等头超时、只有 data 行的 SSE 也记推理 id、官方请求只替换 input 段其余逐字节保留、字符串 input 只整理 tools。旧用例 `/v1/responses/compact` 多级路径改为 `/v1/responses/input_tokens`（compact 已由本工具接管）。未验证：官方 message/function_call 条目带的 encrypted 字段发到中台是否被拒，需用真实中台回放。
- 06 记录：Review 合规 19/23；自修一轮（先 Red 后 Green）：刷新模型后重写接入项（C11）、请求处理中关 stdin 也 5 秒内退出、activate 失败写入 Status、协议文档补错误码与 keys 说明、save 失败整体回滚、关闭转发非 blocked 错误保持原状态、settings 原子写、save 只拉一次中台、重启后恢复 taken_over/restore_blocked；测试专用参数移入 `coretest` 构建标记，正式构建无这些参数。catalog.Build 增加 RelayIDs 以复用回退逻辑。
- 07 记录：Review 合规 13/14；自修一轮：自动检查失败写一行日志（不含响应正文）、响应体 1MB 上限、shutdown 先停检查循环、发布页地址必须以 https://github.com/ 开头；补开发版本号、空 tag、并发测试（后三项为回归保护，首次即通过）。
- 08 记录：Review 合规 13/17；自修一轮（先 Red 后 Green）：ChatGPT 退出等待 30 秒、核心重启延迟 2 秒、单实例改用分布式通知、稳定运行 60 秒后复位重启许可（产品决定）、取消即返回、写失败统一 processExited、重启后 init 出错先停进程、过期重启放弃、按 \n 切行、钥匙串设 AfterFirstUnlockThisDeviceOnly 并区分无权访问。待签名真机手动验证：覆盖写入后钥匙串属性仍在（未签名测试环境读不回该属性，用例 skip）。
- 01 的 Windows 部分已在 CI（windows-2025）验证通过。
## 阻塞
（无）
