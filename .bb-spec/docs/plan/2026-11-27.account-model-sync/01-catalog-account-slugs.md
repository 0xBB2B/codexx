---
name: catalog-account-slugs
description: catalog 用调用方给的账号模型名单过滤官方模型，交集为空时报错；存取账号模型名单文件
---
# catalog 接受账号模型名单
## 目标
合并模型目录的官方模型可以按"查到的账号模型名单"过滤；名单能存进数据目录并读回来。
## 业务规则（来源：PRD 01-account-model-sync）
- 查到的模型名单和 ChatGPT 自带清单（`codex debug models --bundled`）取交集，作为合并模型目录里的官方模型（约束 5）。
- 交集为空时按失败处理：合并模型目录不变（约束 5）。所以调用方显式给了名单、交集为空时，Build 要返回专门的错误，不写目录文件。
- 没给名单时保持现状：按 `models_cache.json` 的 slug 过滤，空名单或交集为空时用全部官方模型。
- 名单文件只存模型名，不存凭据、账号标识（约束 3、8）。
- 「只用中转」方式下没有官方条目，名单不起作用（现有行为）。
## 涉及文件
- core/internal/catalog/catalog.go（修改）
- core/internal/catalog/merge.go（修改）
- core/internal/catalog/store.go（修改）
## 成品定义
账号模型名单文件，存在数据目录 `<DataDir>/account-models.json`，写法沿用 Store.writeJSON（临时文件 + Sync + Rename）：
```json
{
  "slugs": ["gpt-6-luna", "gpt-5.6-terra", "gpt-5.6-luna", "codex-auto-review"]
}
```
- `slugs`：最近一次查询成功、且和自带清单有交集时，官方服务返回的全部模型名，按返回顺序，不去重以外不做加工。
- 文件不存在、读失败、JSON 坏掉、`slugs` 为空：都当作"从没查到过"（返回 nil）。
## 函数清单
### core/internal/catalog/catalog.go
| 函数名 | 职责 |
|---|---|
| `BuildOptions.AccountModels`（新字段） | 调用方查到的账号模型名单；nil 表示没有，按 ModelsCachePath 过滤 |
| `ErrNoAccountModels`（新错误） | 显式名单与自带清单交集为空 |
| `Build`（修改） | 有 AccountSlugs 时按它过滤，交集为空返回 ErrNoAccountModels 且不写文件；否则维持现状 |
### core/internal/catalog/merge.go
| 函数名 | 职责 |
|---|---|
| `IntersectOfficial`（新） | 返回官方条目中 slug 在名单里的那些，不做回退 |
### core/internal/catalog/store.go
| 函数名 | 职责 |
|---|---|
| `Store.SaveAccountModels`（新） | 按成品格式写 account-models.json |
| `Store.LoadAccountModels`（新） | 读回名单，异常情况返回 nil |
## 协作关系
Build → LoadOfficial → （AccountSlugs 非 nil：IntersectOfficial；否则 FilterOfficial(AccountSlugs(ModelsCachePath))）。control 在 03 里调用 Store 存取名单并把名单放进 BuildOptions。无外部依赖。
## 验证方式
- 测试入口：`catalog.Build`（用现有 catalog 测试里的假 codex 程序）、`catalog.Store` 的存取方法。
- 测试输入：自带清单含 a、b、c；AccountSlugs 分别为 [b, x]、[x, y]、nil（配 models_cache.json 含 a）。
- 预期结果：[b, x] → 官方条目只有 b；[x, y] → 返回 ErrNoAccountModels，目录文件不存在或保持旧内容；nil → 按 models_cache 只有 a。
- [ ] SaveAccountSlugs 后文件内容是 `{"slugs":[...]}`，LoadAccountSlugs 读回同样的名单
- [ ] 文件缺失、内容坏掉、slugs 为空 → LoadAccountSlugs 返回 nil
- [ ] RelayOnly 时 AccountSlugs 不影响结果（没有官方条目）
