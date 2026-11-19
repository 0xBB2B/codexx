---
name: 01-core-max-passthrough
description: catalog 中转等级原样写（max 不改名 ultra），proxy 推理强度原样转发，删 UltraToMax
---
# 中转 max 原样透传（core）

## 目标
合并模型目录里中转模型的 max 档写成 max，发往中转的推理强度原样转发。

## 业务规则（来源：PRD 01-relay-max-effort）
- C1：中转模型的思考等级取自接口时，等级名原样写进合并模型目录，`max` 写 `max`。默认等级规则不变：有 high 用 high，否则用最高一档（none < minimal < low < medium < high < xhigh < max）；选出 max 时写 `max`。
- C2：发往中转的请求里推理强度原样转发，本工具不改等级名（max→max、xhigh→xhigh、ultra→ultra）。
- C3：思考等级照抄自官方同名条目的中转模型，等级名原样不变（可含 ultra）。
- C4：只用中转方式（Codex 未登录）时，本工具写进 Codex 配置 `[desktop] enabled-reasoning-efforts` 的可选等级列表，包含中转模型的 `max`，不出现只因改名才产生的 `ultra`。
- 背景：Codex 选 ultra 时实际发 xhigh，旧的 ultra→max 改写从未生效。

## 涉及文件
- 修改 core/internal/catalog/merge.go（relayEntry 去掉 max→ultra 改名；RelayRoute 删除 UltraToMax）
- 修改 core/internal/proxy/body.go（relayBody 不再改推理强度；删除 ultraEffortToMax）
- 修改 core/internal/proxy/route.go（RelayTarget、route 删除 UltraToMax）
- 修改 core/internal/proxy/server.go（relayBody 调用参数）
- 修改 core/internal/control/service.go（pushRelay 不再传 UltraToMax）
- 修改相关测试：core/internal/catalog/merge_test.go、store_test.go、catalog_test.go，core/internal/proxy/route_test.go，core/internal/control/service_test.go，core/internal/codexconfig/apply_test.go（如涉及）

## 函数清单
### core/internal/catalog/merge.go
| 函数名 | 职责 |
|---|---|
| `relayEntry`（修改） | 接口等级原样写入，只返回条目，不再返回改名标志 |
| `RelayRoute`（修改） | 删除 UltraToMax 字段 |
| `MergeRelay`（修改） | 路由不再记 UltraToMax |
### core/internal/proxy/body.go
| 函数名 | 职责 |
|---|---|
| `relayBody`（修改） | 只替换 model，推理强度原样 |
| `ultraEffortToMax`（删除） | — |
### core/internal/proxy/route.go、server.go；core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `RelayTarget` / `route`（修改） | 删除 UltraToMax |
| 调用 relayBody 处（修改） | 去掉多余参数 |
| `pushRelay`（修改） | 不再传 UltraToMax |

## 协作关系
MergeRelay → relayEntry 生成条目和路由；control.rebuild 取 res.ReasoningLevels 写入 Codex 配置的可选等级；pushRelay 把路由交给 proxy；proxy server 转发时调 relayBody。

## 验证方式
- 测试入口：cd core && go test ./internal/catalog ./internal/proxy ./internal/control ./internal/codexconfig
- 测试输入：测试中转返回 example-model-a 等级 ["none","low","medium","high","xhigh","max"]、example-model-b ["low","max"]、和官方同名且无等级的 gpt-x（官方等级含 ultra）
- 预期结果：
  - [ ] 目录：example-model-a 等级依次 none…max，无 ultra，默认 high；example-model-b 默认 max；gpt-x 等级与官方相同（含 ultra）
  - [ ] Result.ReasoningLevels 含 max、不含因改名产生的 ultra；只用中转方式写入 Codex 配置的可选等级列表含 max
  - [ ] 转发：推理强度 max、xhigh、ultra 发往测试中转后原样收到
  - [ ] 原有断言"ultra→max"的用例改为断言原样
