---
name: proxy-official-hook
description: 转发带账号标识的官方请求时，把请求头副本交给 control 的回调，回调不阻塞转发
---
# 转发官方请求时通知 control
## 目标
本机转发服务每转发一个带账号标识的官方请求，就把请求头的副本交给一个回调，原请求照常转发。
## 业务规则（来源：PRD 01-account-model-sync）
- 只在官方请求带 `ChatGPT-Account-ID` 头时通知；不带的、发往中转的、「只用中转」方式下被拒绝的请求都不通知（约束 1）。
- 通知不拖慢原请求：回调由调用方保证立即返回，转发结果和不通知时一样（约束 4）。
- 头副本只在内存里交给回调，proxy 不记录、不写日志（约束 3）；中转请求继续用 rewriteRelayHeaders 清空官方头（现有行为）。
## 涉及文件
- core/internal/proxy/server.go（修改）
## 函数清单
### core/internal/proxy/server.go
| 函数名 | 职责 |
|---|---|
| `Config.OnOfficialRequest`（新字段） | 回调，参数是官方请求头的副本；nil 表示不通知 |
| `Server.ServeHTTP`（修改） | 官方分支转发前，请求带账号标识时调用回调 |
## 协作关系
ServeHTTP → decideRoute → 官方分支 → OnOfficialRequest(r.Header.Clone()) → forward。control 在 03 里提供回调。
## 验证方式
- 测试入口：proxy 包现有 newEnv + fakeUpstream，Config 里设一个记录调用的回调。
- 测试输入：带 ChatGPT-Account-ID 和 Authorization 的官方 /v1/responses 请求；不带账号标识的官方请求；中转模型请求；「只用中转」下的官方请求。
- 预期结果：只有第一种触发回调一次，回调拿到的头里有 Authorization 和 ChatGPT-Account-ID；所有请求的上游响应与没有回调时一致。
- [ ] 回调里修改头副本不影响实际转发出去的头
- [ ] OnOfficialRequest 为 nil 时行为与现在完全相同
