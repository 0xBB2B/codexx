---
name: 01-relay-max-effort
description: 中转模型的等级名原样写进合并模型目录，max 写 max；转发时推理强度原样不改
date: 2026-11-19
---

# 中转的 max 档原样写成 max

## 用户故事

### P0：作为用户，我想在 Codex 里选到中转模型的最高档思考等级，以便中转真的按 max 推理

**用例（主路径）**
- 场景：「中转」接口给 `example-model-a` 返回的思考等级是 `["none","low","medium","high","xhigh","max"]`。小林已在 ChatGPT 设置的「Available reasoning efforts」里勾上 Max。
- 操作：重启 ChatGPT，在 Codex 里选「中转 · example-model-a」，思考等级选 max，发一条消息。
- 预期：模型选择器里这个模型的等级依次是 none、low、medium、high、xhigh、max，没有 ultra。中转收到的推理强度是 `max`。

**用例（边界 / 异常）**
- 场景：同一模型选 xhigh 发消息。预期：中转收到 `xhigh`。
- 场景：接口给 `example-model-b` 的等级是 `["low","max"]`。预期：目录里是 low、max，默认 max。
- 场景：和官方同名的中转模型，接口没给思考等级，照抄官方等级（含 ultra）。预期：目录里的等级和官方条目相同，包括 ultra，名字原样不变。

## 约束

- C1 中转模型的思考等级取自接口时，等级名原样写进合并模型目录，`max` 写成 `max`。默认等级的选法不变：有 high 就用 high，否则用最高一档；选出 max 时写 `max`。
- C2 发往中转的请求里，推理强度原样转发，本工具不改等级名。
- C3 思考等级照抄自官方同名条目的中转模型，等级名原样不变。
- C4 没登录 ChatGPT（只用中转方式）时，本工具写进 Codex 配置、决定显示哪几档的列表里包含 `max`（中转模型有 max 档时）。

## 验收

- [ ] C1：接口等级 `["low","high","max"]` → 目录里依次是 low、high、max，默认 high，没有 ultra。接口等级 `["low","max"]` → 默认 max。
- [ ] C2：用这类模型发推理强度为 `max` 的请求 → 测试中转收到 `max`；发 `xhigh` → 收到 `xhigh`；发 `ultra`（只用来检查本工具不改写）→ 收到 `ultra`。
- [ ] C3：同名模型照抄官方等级 low、medium、high、xhigh、max、ultra → 目录里的等级相同。
- [ ] C4：只用中转方式下，中转模型有 max 档 → Codex 配置里写入的可选等级列表包含 `max`，不包含只因改名才出现的 `ultra`。

## 验证路径

1. 测试中转给 `example-model-a` 返回 `["none","low","medium","high","xhigh","max"]`，开启转发 → 合并模型目录里这个模型的等级是这 6 档，没有 ultra。
2. 用这个模型发推理强度为 `max` 的请求 → 测试中转收到 `max`；发 `xhigh` → 收到 `xhigh`。
3. 让 Codex 处于未登录状态，开启转发 → Codex 配置里写入的可选等级列表包含 `max`。
