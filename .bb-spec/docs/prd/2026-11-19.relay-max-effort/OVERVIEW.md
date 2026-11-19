---
name: relay-max-effort
description: 中转模型的 max 思考等级在 Codex 里直接显示为 max，选它时中转收到 max；README 和官网说明怎么在 ChatGPT 里打开 Max
date: 2026-11-19
features: [model-bridge, project-site]
---

# 中转模型的 max 档直接写成 max

## 背景与问题

- 用户反馈："在codex里选择了ultra但是后台接收到的思考等级是xhigh"。
- 本工具现在的做法：
  - 中转接口给的思考等级里，`max` 写进合并模型目录时改名成 `ultra`；
  - 转发请求时，推理强度是 `ultra` 才改回 `max`。
  
  当初这样做，是因为 ChatGPT 默认不显示 max 档（2026-11-11.codex-model-bridge 的决策记录）。
- 2026-11-19 实测：用本机 ChatGPT 自带的 codex 0.169.0-alpha.11.1、本工具生成的目录和一个本地假服务器抓请求。
  - 选 ultra，Codex 发出的推理强度是 `xhigh`；
  - 选 max，发的是 `max`；
  - 选 xhigh，发的是 `xhigh`。
  
  Codex 把 ultra 当成"xhigh 加自动分派任务"，请求里从来不发 `ultra`。所以转发时 ultra→max 的改写从来没生效过，中转的 max 档实际选不到；用户选"最高档"，中转只收到第二档。
- ChatGPT 桌面端登录账号后，模型选择器显示哪几档，由账号设置「Available reasoning efforts」决定，默认不含 Max。用户 2026-11-19 确认，在那里勾上 Max 后能看到 Max。没登录（只用中转方式）时，显示哪几档由本工具写进 Codex 配置的列表决定。
- 2026-11-19 核对用户的中转：接口给的思考等级最高到 `max`，从不返回 `ultra`；有的模型最高只到 `xhigh`。

## 目标

- 中转模型的思考等级里有 `max` 时，Codex 里显示为 max。用户选 max，中转收到 `max`。
- 用户在 Codex 里选哪一档，中转就收到哪一档，本工具不改等级名。
- README 和官网说明：已登录 ChatGPT 时，怎么让 Max 档出现。

## 非目标

- 不替用户改 ChatGPT 账号里的「Available reasoning efforts」设置。
- 本工具 App 的界面不加提示。
- 官方模型的等级不变。照抄官方同名条目等级的中转模型，等级名也原样不变。
- 不处理 Codex 选 ultra 时发 xhigh 这一行为本身。

## 子需求清单

| 序号 | 子需求 | 解决的子问题 | 优先级 |
|---|---|---|---|
| 01 | [中转的 max 档原样写成 max](01-relay-max-effort.md) | 目录里怎么写、转发时改不改 | P0 |
| 02 | [说明怎么打开 Max](02-max-effort-docs.md) | 用户怎么知道要去 ChatGPT 设置里勾 Max | P1 |

## 依赖与交付顺序

01、02 互不依赖，可以并行。

## 整体验证路径

1. 测试中转给 `example-model-a` 返回思考等级 `["none","low","medium","high","xhigh","max"]`，开启转发。合并模型目录里这个模型的等级依次是 none、low、medium、high、xhigh、max，没有 ultra，默认 high。
2. 用这个模型发推理强度为 `max` 的请求，测试中转收到 `max`；发 `xhigh`，收到 `xhigh`。
3. 打开官网的「常见问题」，第 3 条是「中转模型怎么选 Max 思考等级？」，展开后的文字和 02 一致；切到 English，显示英文版。
4. README 里有一段同样的说明。

## 决策记录

- **中转的 max 直接写成 max，删掉改名和转发时的改写**（用户 2026-11-19 选定）。
  - 原因：Codex 选 ultra 时发的是 xhigh，转发时分不出用户选的是 ultra 还是 xhigh，改名方案无法修好。
  - 代价：已登录 ChatGPT 的用户要先去「Available reasoning efforts」勾上 Max 才能选到这一档。没勾时最高只能选到 xhigh，和现在的实际效果一样，不会变差。
  - 本需求替换 2026-11-11.codex-model-bridge 的 C22 和 2026-11-19.relay-model-params 的 C6。
- **提示只写进 README 和官网，App 界面不动**（用户 2026-11-19 选定）。被否决的做法：
  - 主窗口加一行提示：要出界面预览，两端都要改；
  - 不提示：用户找不到 Max 时不知道原因。
- **官网放在「常见问题」第 3 条，回答开头加「中转模型支持 Max 时」**（用户 2026-11-19 选定）。原因：用户的中转里有模型最高只到 xhigh，不加限定，这些模型的用户会奇怪为什么找不到 Max。
