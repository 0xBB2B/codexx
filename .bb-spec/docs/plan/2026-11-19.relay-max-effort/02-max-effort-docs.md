---
name: 02-max-effort-docs
description: 官网常见问题第 3 条与 README 说明怎么打开 Max；更新 sitecheck 的 FAQ 条数
---
# 说明怎么打开 Max（官网 + README）

## 目标
官网「常见问题」第 3 条和 README 讲清楚已登录 ChatGPT 时怎么让 Max 档出现。

## 业务规则（来源：PRD 02-max-effort-docs）
- C1：官网 site/index.html「常见问题」（#faq）新增一条问答，排第 3：在「选了官方模型，请求会发给中转吗？」之后、「API Key 存在哪里？」之前。写法同其他条（`<details><summary>…</summary><p>…</p></details>`，中英各 `<span data-l="zh">`/`<span data-l="en">`），默认收起，只有第 1 条带 open。共 8 条。文案逐字见「成品定义」。两段回答最后的 max 用 `<code>`。
- C2：README 新增一段，内容同中文问答。
- 官网样式按项目根 DESIGN.md 第 11 节，不新增样式。

## 涉及文件
- 修改 site/index.html
- 修改 README.md
- 修改 scripts/sitecheck/site_test.go（TestSite_FaqDetails 条数 7→8，并断言第 3 条文案）

## 成品定义

site/index.html 在「选了官方模型…」那条 `</details>` 之后插入一行：
```html
<details><summary><span data-l="zh">中转模型怎么选 Max 思考等级？</span><span data-l="en">How do I select the Max reasoning effort for a relay model?</span></summary><p><span data-l="zh">中转模型支持 Max 时，如果已登录 ChatGPT，Codex 里能选哪几档思考等级，由 ChatGPT 设置里的「Available reasoning efforts」决定，默认没勾 Max。去那里勾上 Max 就能选。没登录 ChatGPT（只用中转）时，这一步可以跳过。选了 Max，中转收到的思考等级就是 <code>max</code>。</span><span data-l="en">If the relay model supports Max and you are signed in to ChatGPT, the reasoning efforts you can select in Codex come from “Available reasoning efforts” in ChatGPT settings, and Max is unchecked by default. Check Max there and you can select it. When you are not signed in (Relay only), skip this step. With Max selected, the relay receives <code>max</code> as the reasoning effort.</span></p></details>
```

README.md 在「## 首次打开」一节之后、「## 卸载」之前插入：
```markdown
## 选 Max 思考等级

中转模型支持 Max 时，如果已登录 ChatGPT，Codex 里能选哪几档思考等级，由 ChatGPT 设置里的「Available reasoning efforts」决定，默认没勾 Max。去那里勾上 Max 就能选。没登录 ChatGPT（只用中转）时，这一步可以跳过。选了 Max，中转收到的思考等级就是 `max`。
```

## 函数清单
（无过程式代码；sitecheck 测试由 Test Agent 改）

## 协作关系
官网由 scripts/sitebuild 发布；sitecheck 测试校验 site/index.html。

## 验证方式
- 测试入口：cd scripts/sitecheck && go test ./...
- 预期结果：
  - [ ] details 共 8 个，只有第 1 个带 open
  - [ ] 第 3 个 details 的中英问题与回答逐字等于成品定义，回答内含 `<code>max</code>`
  - [ ] README 含「## 选 Max 思考等级」一节，正文与中文回答一致
