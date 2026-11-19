---
name: 03-site-figure
description: 官网三张 App 界面图按 760px 宽画，窄屏按 65%/42%/37% 缩放且不出现横向滚动条
---
# 官网 App 界面图同步加宽

## 目标
官网「界面长什么样」区块里的 App 界面图和实际窗口一样宽（760px），窄屏按新比例缩放。

## 业务规则（来源：PRD 02-site-figure）
- 三张界面图（首次设置、主窗口、菜单栏菜单）按 760px 宽画，图里内容和文字不变。
- 浏览器窗口宽 809px 及以上不缩放；808px 及以下缩到 65%；600px 及以下缩到 42%；356px 及以下缩到 37%。
- 各宽度下页面都没有横向滚动条。
- 官网其他区块不变。

## 涉及文件
- 修改 `site/index.html`（第 16 行 `.am-window`、第 53 行 `.am-scene` 的宽度，第 62-64 行三条缩放规则）
- 修改 `scripts/sitecheck/site_test.go`（新增用例）

## 成品定义
`site/index.html` 第 62-64 行原样替换为：

```css
@media (max-width:808px){.am{zoom:.65}}
@media (max-width:600px){.am{zoom:.42}}
@media (max-width:356px){.am{zoom:.37}}
```

第 16 行、第 53 行里的 `width:620px` 都改为 `width:760px`，同一行其余样式不变。

## 函数清单
### scripts/sitecheck/site_test.go
| 函数名 | 职责 |
|---|---|
| `TestSite_AppFigureWidth`（新增） | 检查界面图宽 760 和三条缩放规则 |

## 协作关系
`scripts/sitecheck` 的测试读 `site/index.html` 原文做断言；`scripts/sitebuild` 生成发布用页面，不受影响。

## 验证方式
- 测试入口：`cd scripts/sitecheck && go test ./...`。
- 测试输入：仓库里的 `site/index.html`。
- 预期结果：
- [ ] `.am-window` 与 `.am-scene` 的宽是 `760px`，页面里不再出现 `width:620px`。
- [ ] 缩放规则恰好是：`max-width:808px` → `.65`、`max-width:600px` → `.42`、`max-width:356px` → `.37`；不再有 `max-width:760px` → `.8` 等旧规则。
- [ ] 用浏览器打开页面，窗口宽 1200、808、600、375、356、320px 时，界面图分别约 760、494、319、319、281、281px 宽，页面没有横向滚动条（人工或浏览器自动化核对一次）。
