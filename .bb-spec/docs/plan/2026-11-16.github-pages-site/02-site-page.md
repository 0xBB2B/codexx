---
name: 02-site-page
description: 把确认过的原型移植成 site/index.html：中英双语单页、网页代码画的 App 界面图、语言切换
---
# 官网单页

## 目标
仓库 `site/index.html` 是一个自包含的中英双语介绍单页，浏览器直接打开就能看。

## 业务规则（来源：PRD 01-site-page）
- 从上到下九个区块，每个区块有固定的 `id`：
  - 顶栏（`top`）、首屏（`hero`）、界面长什么样（`screens`）、三步上手（`start`）、两种接入方式（`modes`）；
  - 安全说明（`security`）、常见问题（`faq`）、下载（`download`）、页脚（`footer`）。
  - 顶栏 6 个页内链接依次指向 `#screens #start #modes #security #faq #download`。
- 文案、布局和交互以确认过的原型为准，见下面「成品定义」。关键文字：
  - 中文大标题「让 ChatGPT 桌面版里的 Codex，用上你自己的模型服务」。
  - 页签「首次设置」「主窗口」「菜单栏菜单」，默认选中「主窗口」。
  - 常见问题共 7 条，只有第一条默认展开。
  - 下载按钮「去 Releases 页面下载」。
  - 页脚非隶属声明「ChatGPT 和 Codex 是各自所有者的商标，本项目与它们的所有者没有隶属关系。」
- 链接：「下载」和两个「去 Releases 页面下载」指向 `https://github.com/0xBB2B/codexx/releases`；「查看源码」「GitHub」「GitHub 仓库」指向 `https://github.com/0xBB2B/codexx`。
- 语言：
  - 中英文在同一个文件里，切换时网址不变。
  - 第一次打开时，浏览器首选语言以 `zh` 开头就显示中文，否则显示英文。
  - 点过切换后，把选择存进浏览器本地存储（键名 `modelbridge-site-lang`，值 `zh` 或 `en`），以后打开都按它显示；读写存储失败时按浏览器语言显示，页面照常工作。
  - 切换时 `<html lang>` 换成 `zh-CN` 或 `en`，`<title>` 换成对应语言。
- 英文页：
  - 「中台」写作 relay，App 界面图保持中文，三张图下都有 "The app’s interface is available in Chinese only."
  - 模型列表示意下有 "Relay models are labelled “中台” (relay) in the list."
- 页面只许出现以下网址：`https://github.com/0xBB2B/codexx`（含它下面的路径）、`relay.example.com`、`127.0.0.1`。
  - 内联 SVG 不写 `xmlns`，HTML 里的 SVG 不需要它。
  - 不加载任何外部字体、脚本、样式、图片；图标用内联 SVG，网站图标用 `data:` 地址。
- 示例 Key 只用 `sk-example-0000`，或用圆点遮住。
- 窗口宽 900px 及以下多列改一列、顶栏 6 个页内链接隐藏；760px、600px 及以下 App 界面图各缩小一档；任何宽度都没有横向滚动条。
- Tab 聚焦有 2px 绿色描边；悬停变深或出现浅底，按下更深。

## 涉及文件
- 新建 `site/index.html`
- 新建 `scripts/sitecheck/site.go`
- 新建 `scripts/sitecheck/site_test.go`

## 成品定义
- 页面的 HTML 结构、CSS、两种语言的全部文案，按原型文件 `.bb-spec/.cache/ui-preview/2026-11-16.github-pages-site.html` 里「官网首页（单页）」那一页的框内内容原样移植。
  - 只取框内页面，去掉外壳（左栏、方案和状态切换、外壳脚本）。
  - 中文、English 两个状态合并成同一个页面，用上面的语言规则切换。
- CSS 和 JavaScript 都写在 `index.html` 里，不拆文件。
- 颜色、字号、间距、组件以项目根 `DESIGN.md` 第 11 节「官网」为准；和原型有出入时以 DESIGN.md 为准。

## 函数清单
### site/index.html（内联脚本）
| 函数名 | 职责 |
|---|---|
| `pickLang` | 先读本地存储里的选择，读不到再看浏览器首选语言，返回 `zh` 或 `en` |
| `applyLang` | 显示对应语言的内容，并设置 `<html lang>` 和 `<title>` |
| `setLang` | 语言切换按钮的点击处理：调用 `applyLang`，并尝试存进本地存储 |
| `selectTab` | 界面图页签的点击处理：只显示选中的那张图，并更新页签的选中状态 |

常见问题的展开和收起用原生 `<details>`/`<summary>`，不写脚本。

### scripts/sitecheck/site.go
| 函数名 | 职责 |
|---|---|
| `defaultSiteDir` | 常量：从本模块目录到官网目录的相对路径 `../../site`；`main` 没有收到参数时用它 |

`main.go` 改为：没有命令行参数时检查 `defaultSiteDir`。

## 协作关系
`DOMContentLoaded` → `applyLang(pickLang())`；语言按钮 → `setLang`；页签 → `selectTab`。不访问网络。

## 验证方式
- 测试入口：`cd scripts/sitecheck && go test ./...`（`site_test.go` 读真实的 `../../site/index.html`），以及在浏览器里打开 `site/index.html`。
- 预期结果：
  - [ ] 对 `../../site` 调用 `Check` 返回 0 处违规。
  - [ ] 文件里按顺序出现 `id="top"`、`hero`、`screens`、`start`、`modes`、`security`、`faq`、`download`、`footer`。
  - [ ] 含上面列出的中文关键文字，以及英文的 "The app’s interface is available in Chinese only." 和 "Relay models are labelled “中台” (relay) in the list."。
  - [ ] 恰好 7 个 `<details>`，只有第一个带 `open`。
  - [ ] 不含 `<link rel="stylesheet"`、`<script src=`，也不含 `src="http` 或 `url(http`。
  - [ ] 浏览器检查：语言为 `zh-CN` / `en-US` 时首次打开分别显示中文 / 英文；切换后网址不变、刷新后保持；`<html lang>` 和标题跟着变；页签切换只显示一张图；在 375px、600px、760px、900px 宽下没有横向滚动条；网络记录里没有外部请求。
