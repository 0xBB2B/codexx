---
name: 01-site-checker
description: 用 Go 写官网发布前的检查程序：网址只许白名单内，Key 只许示例 Key，违规只报文件、行号和原因
---
# 官网发布前检查程序

## 目标
运行一条命令就能检查官网目录：有违规时列出每处违规并以非 0 退出，没有违规时以 0 退出。

## 业务规则（来源：PRD 02-site-publish）
- 检查规则只写允许出现的内容，不写任何真实地址、Key 或品牌，否则规则本身就把它们公开了。
- 官网目录（含子目录）里每个文本文件都要检查。含 NUL 字节的文件当作二进制文件跳过。
- 网址：文本中以 `http://` 或 `https://` 开头的串都算网址，网站部分（主机名，不含端口）只允许以下几种：
  - `github.com`，且路径是 `/0xBB2B/codexx`，或以 `/0xBB2B/codexx/`、`/0xBB2B/codexx?`、`/0xBB2B/codexx#` 开头，且解码后的路径里没有 `.` 或 `..` 段；
  - `relay.example.com`，路径不限；
  - `127.0.0.1`，端口和路径不限。
  - 其他一律违规，原因写"网址不在白名单内"。主机名比较不区分大小写，路径区分大小写。
- 网址中间遇到第一个非 ASCII 字符时：它是中文标点 `。，；：！？）】」、` 之一、且后面是行尾、空白、另一个非 ASCII 字符或下一个 `http(s)://`，网址就在这里结束；否则整条网址违规。网址末尾的 `.,;:!?)]}` 不算网址。
- 只防误写：故意构造的绕过写法（中文标点后接中文再接域名、路径结尾 `/..`、网址中夹制表符）不处理（用户 2026-11-16 决定）。
- Key：文本中出现 `sk-` 后面紧跟至少一个字母、数字、`_` 或 `-` 的串，且 `sk-` 前一个字符不是字母或数字（避免把 `task-`、`disk-` 当成 Key），就算一个 Key。只允许整串恰好是 `sk-example-0000`，其他一律违规，原因写"Key 不是示例 Key"。
- 违规输出每处一行，格式 `<相对官网目录的文件路径>:<行号>: <原因>`。不打印违规的网址或 Key 原文。
- 行号从 1 开始；同一行有多处违规时每处各报一行。

## 涉及文件
- 新建 `scripts/sitecheck/go.mod`
- 新建 `scripts/sitecheck/check.go`
- 新建 `scripts/sitecheck/check_test.go`
- 新建 `scripts/sitecheck/main.go`
- 修改 `Makefile`

## 成品定义

`scripts/sitecheck/go.mod`（独立模块，只用标准库，不和 `core/` 的模块混在一起）：

```
module sitecheck

go 1.27
```

`Makefile` 改动：在 `.PHONY` 和 `test` 里加上 `test-site`，并新增这个目标。CI 的 `core-and-macos` 任务跑的就是 `make test`，所以检查程序的测试会随 CI 一起跑。

```make
.PHONY: test test-core test-macos test-site build-core

test: test-core test-macos test-site

test-site:
	cd scripts/sitecheck && go test ./...
```

## 函数清单
### scripts/sitecheck/check.go
| 函数名 | 职责 |
|---|---|
| `Violation` | 一处违规：文件（相对官网目录、用 `/` 分隔）、行号、原因 |
| `Check` | 遍历官网目录的全部文件，返回按文件路径、行号排序的违规列表；读目录或文件出错时返回错误 |
| `checkLine` | 检查一行文本，返回这一行里每处违规的原因 |
| `allowedURL` | 判断一个网址是否在白名单内 |

### scripts/sitecheck/main.go
| 函数名 | 职责 |
|---|---|
| `main` | 取第一个命令行参数作为官网目录，调用 `Check`；逐行打印违规；有违规退出码 1，出错打印错误并退出码 2，否则打印"检查通过"并退出码 0 |

## 协作关系
`main` → `Check` → `checkLine` → `allowedURL`。只读本地文件，不访问网络。

## 验证方式
- 测试入口：`cd scripts/sitecheck && go test ./...`；包级入口 `Check(root string) ([]Violation, error)`，`Violation` 的字段为 `File string`、`Line int`、`Reason string`。
- 测试输入：在 `t.TempDir()` 里写临时官网目录和文件。
- 预期结果：
  - [ ] 只含 `https://github.com/0xBB2B/codexx`、`https://github.com/0xBB2B/codexx/releases`、`https://relay.example.com/v1`、`http://127.0.0.1:47311`、`sk-example-0000` → 没有违规。
  - [ ] 含 `https://other.example.org/` → 1 处违规，原因"网址不在白名单内"，行号正确。
  - [ ] 含 `https://github.com/someone/else` 或 `https://github.com/0xBB2B/codexx-fork` → 违规。
  - [ ] 含 `http://www.w3.org/2000/svg` → 违规（页面里内联的 SVG 不能带这个地址）。
  - [ ] 含 `sk-abc123` → 1 处违规，原因"Key 不是示例 Key"；含 `task-list`、`disk-0` → 没有违规；含 `sk-example-00001` → 违规。
  - [ ] 同一行两处违规 → 报两条；违规在子目录文件的第 3 行 → `File` 为 `sub/x.html`，`Line` 为 3。
  - [ ] 含 NUL 字节的文件 → 跳过，不报违规。
  - [ ] 违规的 `Reason` 里不含违规网址或 Key 的原文。
  - [ ] 目录不存在 → 返回错误。
