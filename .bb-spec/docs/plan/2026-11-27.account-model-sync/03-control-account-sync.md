---
name: control-account-sync
description: control 按时机用转发中的官方凭据查账号模型列表，变了就存名单、重建目录、推送 status 新字段
---
# 按账号查官方模型并重建目录
## 目标
转发开着、「官方 + 中转」方式下，按时机查当前 ChatGPT 账号的模型列表；名单变了时重建合并模型目录，并通过 status 告诉 App 弹提示。
## 业务规则（来源：PRD 01-account-model-sync）
- 只在「官方 + 中转」方式、带 `ChatGPT-Account-ID` 的官方请求经过时考虑查询（约束 1）。
- 查询时机：本工具启动后第一个这样的请求；账号标识和上次查时不同；离上次查（不论成败）超过 1 小时。同一时间最多一个查询（约束 2）。
- 查询只发往官方 ChatGPT 服务地址（Options.OfficialChatGPTURL，和转发用的同一个），用原请求带的凭据头；凭据和账号标识只在内存里用，不写文件、不写日志，不发往中转（约束 3）。账号标识只在内存里保留用于比较。
- 查询在后台进行，不拖慢原请求（约束 4）；超时 5 秒（和 Codex 自己查 /models 一致）。
- 失败（网络错误、非 2xx、JSON 看不懂、models 为空、拿不到 client_version）或与自带清单交集为空：名单文件和目录都不变，status 不变（约束 5）。
- 成功且有交集：存名单文件；新目录的官方模型集合和旧的不同 → 重建目录、`official_models_revision` 加 1 并推送 status；相同 → 只存名单，不重建、不加 1（约束 6）。
- 本工具启动、开转发、刷新模型等任何重建：有存下的名单就用它过滤官方模型，没有就按 models_cache.json（约束 8）；这些重建不加 1（约束 9）。
- 卸载时删掉名单文件（数据目录清单）。
## 涉及文件
- core/internal/control/service.go（修改）
- core/internal/control/account_models.go（新建）
- core/PROTOCOL.md（修改）
## 成品定义
官方 ChatGPT 服务的模型列表请求（照 openai/codex `codex-rs/codex-api/src/endpoint/models.rs`、`codex-rs/model-provider/src/models_endpoint.rs`）：
```
GET <OfficialChatGPTURL>/models?client_version=<主.次.修订>
请求头（从原请求原样复制，原请求没有的不加）：Authorization、ChatGPT-Account-ID、originator、User-Agent
超时：5 秒
成功响应：200，{"models":[{"slug":"gpt-6-luna", ...}, ...]}，只取每项的 slug
client_version：读 models_cache.json 顶层 "client_version"，去掉 "-" 之后的预发布后缀；读不到就不查（按失败算）
```
Status 新字段（core/internal/control/service.go 的 Status 结构体，JSON 名 `official_models_revision`）：
```json
{
  "official_models_revision": 0
}
```
PROTOCOL.md 的 Status 示例加这一项，并在字段说明里加一段：
```markdown
`official_models_revision` 是本次 core 运行期间官方模型按 ChatGPT 账号更新的次数，从 0 开始。转发开着、「官方 + 中转」方式下，本工具在转发官方请求时查到当前账号能用的官方模型变了、重建了合并模型目录，就加 1。App 看到它比上一次收到的值大时，显示提示「官方模型已按当前账号更新」；core 启动、开转发等其他重建不会让它变化。
```
数据目录清单 uninstallFiles 加一项 `"account-models.json"`（格式见 catalog-account-slugs 计划）。
## 函数清单
### core/internal/control/account_models.go
| 函数名 | 职责 |
|---|---|
| `Service.onOfficialRequest` | 交给 proxy 的回调：判断时机，需要时启动后台查询，立即返回 |
| `Service.syncAccountModels` | 后台：发查询、解析 slug、存名单、重建、比较官方集合、加 revision 并通知 |
| `fetchAccountSlugs` | 按成品定义发请求，返回 slug 列表或错误 |
| `clientVersion` | 从 models_cache.json 读 client_version 并去掉预发布后缀 |
### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `Options.Now`（新字段） | 取当前时间，测试用来拨快 1 小时；nil 用 time.Now |
| `Status.OfficialModelsRevision`（新字段） | 见成品定义 |
| `Service.startProxy`（修改） | 构造 proxy.Config 时设 OnOfficialRequest |
| `Service.rebuild`（修改） | 把存下的名单放进 BuildOptions.AccountSlugs；记录当前官方模型集合 |
| `Service.statusLocked`（修改） | 填 OfficialModelsRevision |
## 协作关系
proxy 官方分支 → onOfficialRequest（持锁判断时机、标记查询中）→ go syncAccountModels → fetchAccountSlugs（net/http，发往官方）→ catalog.Store.SaveAccountSlugs → catalog.Build（AccountSlugs）→ notifyLocked。依赖 catalog-account-slugs、proxy-official-hook。
## 验证方式
- 测试入口：control 包现有 newEnv（Options.OfficialChatGPTURL 指向 httptest 假官方服务，假服务对 /models 按 ChatGPT-Account-ID 返回不同名单，同时承接 /responses）；writeModelsCache 写入含 client_version 的缓存；Options.Now 用可拨动的假时钟。经本机转发服务发带账号标识的官方请求。
- 预期结果：
- [ ] 账号 A 第一次请求 → 假服务收到一次 /models（带原 Authorization、ChatGPT-Account-ID、client_version 参数）；目录官方模型 = A 名单 ∩ 自带清单；集合变了时 revision 变 1 并推送 status
- [ ] 1 小时内再来 A 的请求 → 没有新 /models；拨快 61 分钟后再来 → 再查；名单相同时 revision 不变、目录文件不改写
- [ ] 换账号 B → 立即查，revision 加 1；连发多个并发请求只查一次
- [ ] /models 返回 500、非 JSON、空 models、交集为空，或缓存缺 client_version → 目录和名单文件不变，revision 不变，原请求照常返回
- [ ] 不带账号标识的请求、「只用中转」方式、中转模型请求 → 不查
- [ ] 假中转服务收不到 /models；数据目录和日志里搜不到 Authorization 值和账号标识；account-models.json 只有 slugs
- [ ] 重启 Service（同一数据目录）→ 开转发后的目录按存下的名单过滤，revision 为 0
- [ ] PrepareUninstall 后 account-models.json 被删除
