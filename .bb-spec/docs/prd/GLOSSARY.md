# 术语表

> 全项目术语唯一权威源，仅 /prd 维护。跨语言 / 跨文档以英文锚点对齐；别名只登记实际用过的语言写法。

| 锚点 | 定义 | 别名 |
|---|---|---|
| relay | 用户另外购买、通过 Base URL 和 API Key 访问的第三方模型服务，提供 Responses API；公开内容里不出现它的真实名称和地址 | 简体「中转」；English「relay」 |
| official-model | ChatGPT 当前版本自带模型清单里的模型，请求最终发往官方服务 | 简体「官方模型」 |
| relay-model | 某个中转配置的模型列表接口返回的模型，请求最终发往该配置的中转服务；Codex 里显示为「<配置名> · <模型>」 | 简体「中转模型」 |
| relay-config | 一组「名字 + Base URL + API Key」，代表一家中转服务；最多 10 个、同时生效，名字不区分大小写不能重复 | 简体「中转配置」「配置」 |
| auto-model | 中转提供的、由中转自动选择具体模型的模型，模型名为 `auto` | 简体「auto 模型」「auto 智能模型」 |
| model-params | 合并模型目录里一个模型的上下文窗口、思考等级（含默认等级）、输入类型（文字、图片）；中转模型的取自中转模型列表接口 | 简体「模型参数」 |
| merged-catalog | 本工具生成的模型目录文件：官方模型在前，中转模型在后；Codex 的模型选择器按它显示 | 简体「合并模型目录」「合并目录」 |
| local-proxy | 本工具在本机提供、只接受本机连接的转发服务；按请求里的模型把请求转到官方服务或中转 | 简体「本机转发服务」「本机代理」「本机分流」 |
| forwarding | 本工具的开关状态：开启时 Codex 配置写入了接入项，Codex 请求经过本机转发服务；关闭时 Codex 配置是开启前的样子 | 简体「转发」 |
| access-entries | 本工具在 Codex 用户配置里写入或移除的几项。「官方 + 中转」方式：`openai_base_url`、`model_catalog_json`、`model`、`[desktop]` 表里的 `enabled-reasoning-efforts`，以及被移除的 `model_provider`；「只用中转」方式：`model_provider`、`[model_providers.modelbridge]` 表、`model_catalog_json`、`model` | 简体「接入项」 |
| access-mode | 转发开着时本工具改 Codex 配置的方式，由 Codex 是否已登录自动决定：「官方 + 中转」（已登录，官方模型和中转模型并列）或「只用中转」（没登录，Codex 只连本工具、不需要登录，只有中转模型） | 简体「接入方式」 |
| login-start | 用户登录系统后自动启动本工具的设置，默认开启，可以在菜单里关闭 | 简体「登录时启动」 |
| project-site | 发布在 GitHub Pages 上、介绍本工具的中英双语单页网站 | 简体「官网」 |
| app-icon | 本工具在 macOS、Windows 上的应用图标：主色圆角底板上的白色双向箭头；菜单栏、托盘用同一图形的单色版 | 简体「应用图标」「图标」 |
