---
name: 02-login-detect
description: 运行 codex login status，只按退出码和固定输出判断 Codex 是否已登录
---
# 登录状态检测

## 目标
新增一个只依赖 codex 程序的检测函数，返回"已登录 / 没登录 / 检测失败"三种结果之一，不保存、不记录命令输出。

## 业务规则（来源：PRD 08-access-mode C1；AGENTS.md 项目约束）
- 运行当前 ChatGPT 自带的 codex 程序：`<codex> login status`，使用用户平常的 Codex 数据目录（不改 `CODEX_HOME`，继承本工具进程的环境变量）。
- 退出码 0 → 已登录。
- 退出码 1 且输出（标准输出和标准错误合并，去掉首尾空白）恰好是 `Not logged in` → 没登录。
- 其他情况一律检测失败：其他退出码；退出码 1 但输出不同；codex 程序不存在或不是文件；10 秒内没有结束（超时后结束进程）。
- 命令输出只用于上面这一次比较：不写日志、不放进错误信息、不返回给调用方。原因：API Key 登录时这个命令会打出部分遮蔽的 Key。
- 不读取、不打开 Codex 数据目录里的任何文件（`auth.json` 等）。

## 涉及文件
- 新建 `core/internal/codexauth/login.go`
- 新建 `core/internal/codexauth/login_test.go`

## 函数清单
### core/internal/codexauth/login.go
| 函数名 | 职责 |
|---|---|
| `Result`（类型）及常量 `LoggedIn`、`NotLoggedIn`、`Failed` | 三种检测结果；字符串值分别为 `logged_in`、`not_logged_in`、`failed`，供控制协议直接输出 |
| `Timeout`（包级变量，默认 10 秒） | 检测超时时间，测试可调小 |
| `Detect` | 运行 `login status` 并按规则给出结果 |

## 协作关系
- 只用标准库 `os/exec`、`context`、`errors`、`strings`。
- 调用方：`control.Service`（plan 05）在本工具启动、打开转发、重新检测时调用，codex 路径来自现有 `locateCodex`。

## 验证方式
- 测试入口：`codexauth.Detect`（`go test ./internal/codexauth`）。
- 测试输入：临时目录里的假 codex 脚本（`#!/bin/sh`，按需打印文字、`exit N`、或 `sleep`），测试里把 `Timeout` 调成 200 毫秒；Windows 上跳过依赖 sh 的用例。
- 预期结果与验证项：
- [ ] 脚本 `exit 0`（打印 "Logged in using ChatGPT"）→ `LoggedIn`。
- [ ] 脚本向标准错误打印 `Not logged in` 并 `exit 1` → `NotLoggedIn`；打印到标准输出、带尾随换行也一样。
- [ ] 脚本打印 `Error checking login status: boom` 并 `exit 1` → `Failed`。
- [ ] 脚本 `exit 2` → `Failed`。
- [ ] 路径不存在 → `Failed`。
- [ ] 脚本 `sleep 5` → 在 `Timeout` 后不久返回 `Failed`，进程被结束。
- [ ] 脚本确认收到的参数正好是 `login status`，且 `CODEX_HOME` 环境变量和测试进程设定的值相同（脚本把参数和环境写进临时文件供断言）。
- [ ] 脚本输出里带特殊字符串 `sk-SECRET-MARKER`：`Detect` 的返回值里拿不到它（返回类型只有三种结果）。
