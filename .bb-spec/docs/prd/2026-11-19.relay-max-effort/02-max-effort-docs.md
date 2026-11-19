---
name: 02-max-effort-docs
description: 官网「常见问题」第 3 条和 README 说明已登录 ChatGPT 时怎么让 Max 档出现
date: 2026-11-19
---

# 说明怎么打开 Max

## 用户故事

### P1：作为已登录 ChatGPT 的用户，我想知道为什么 Codex 里没有 Max 档、怎么打开它

**用例（主路径）**
- 场景：小林在 Codex 里只看到 xhigh，找不到 Max。
- 操作：打开官网的「常见问题」，展开第 3 条。
- 预期：看到要去 ChatGPT 设置的「Available reasoning efforts」里勾上 Max；没登录 ChatGPT 时不需要这一步。

## 约束

- C1 官网「常见问题」一节新增一条问答，排在第 3 条：在「选了官方模型，请求会发给中转吗？」之后、「API Key 存在哪里？」之前。写法和其他问答相同，默认收起，第 1 条仍默认展开。文案逐字如下：
  - 中文问题：中转模型怎么选 Max 思考等级？
  - 中文回答：中转模型支持 Max 时，如果已登录 ChatGPT，Codex 里能选哪几档思考等级，由 ChatGPT 设置里的「Available reasoning efforts」决定，默认没勾 Max。去那里勾上 Max 就能选。没登录 ChatGPT（只用中转）时，这一步可以跳过。选了 Max，中转收到的思考等级就是 max。
  - 英文问题：How do I select the Max reasoning effort for a relay model?
  - 英文回答：If the relay model supports Max and you are signed in to ChatGPT, the reasoning efforts you can select in Codex come from “Available reasoning efforts” in ChatGPT settings, and Max is unchecked by default. Check Max there and you can select it. When you are not signed in (Relay only), skip this step. With Max selected, the relay receives max as the reasoning effort.
  - 两段回答最后的 max 用代码片段样式（`<code>`）显示。
- C2 README 新增一段说明，内容同 C1 的中文问答。

## 验收

- [ ] C1：官网「常见问题」共 8 条，第 3 条的问题和回答与 C1 逐字一致；只有第 1 条带 open；切到 English 显示英文版；回答里的 max 在 `<code>` 里。
- [ ] C2：README 里有「中转模型怎么选 Max 思考等级」的说明，内容与 C1 中文回答一致。

## 验证路径

1. 打开官网，滚到「常见问题」→ 共 8 条，第 3 条是新问答且收起；展开后文字与 C1 一致。
2. 点「English」→ 第 3 条显示英文问答。
3. 打开 README → 有同样的中文说明。
