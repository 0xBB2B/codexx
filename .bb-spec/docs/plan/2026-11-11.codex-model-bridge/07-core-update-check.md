---
name: 07-core-update-check
description: 核心程序检查本工具在 GitHub 的最新正式版本，启动时一次、之后每 24 小时一次
---
# 核心：更新检查

## 目标
核心程序能判断 GitHub 上有没有比当前更新的正式版本，并把版本号和发布页地址告诉外壳。

## 业务规则（来源：PRD 06-update-check）
- 启动后 1 分钟内检查一次，之后运行期间每 24 小时一次；外壳可随时调用 `check_update {"manual": true}`。
- 只访问 `GET https://api.github.com/repos/<仓库>/releases/latest`，请求头只有 `Accept: application/vnd.github+json` 和 `User-Agent: model-bridge/<当前版本>`；不带 Base URL、Key 或任何用户信息。
- `<仓库>` 在编译时由 CI 注入（`-ldflags "-X modelbridge/internal/update.repository=<owner>/<repo>"`，CI 里取 `${{ github.repository }}`）；未注入时不检查，`check_update` 返回 `is_newer: false`。
- 该接口本身只返回正式版（草稿、预发布不会出现）；再校验 `tag_name` 去掉开头 `v` 后是 `主.次.修订` 三段数字，否则视为没有新版。
- 新版本 = 按三段数字比较大于当前版本。发布页地址取返回的 `html_url`。
- 自动检查失败：不推送错误，只记日志"更新检查失败"（不记响应正文）。手动检查失败：返回错误 `update_unavailable`。
- 发现新版时，Status 的 `update` 字段带上版本和地址，并推送 status 事件。
- 不下载、不安装任何东西。退出后不再检查。

## 涉及文件
- core/internal/update/check.go（新建）
- core/internal/update/semver.go（新建）
- core/internal/update/check_test.go、semver_test.go（新建）
- core/internal/control/service.go（修改：启动定时检查，接 check_update 方法）

## 函数清单
### core/internal/update/check.go
| 函数名 | 职责 |
|---|---|
| `Latest` | 请求 GitHub 最新发布，返回版本号和 html_url |
| `Scheduler` | 启动后 1 分钟检查一次、之后每 24 小时一次，结果回调给控制层；Stop 时停止 |
### core/internal/update/semver.go
| 函数名 | 职责 |
|---|---|
| `Parse` | 解析 `v1.2.3` / `1.2.3`，其他格式报错 |
| `Newer` | 比较两个版本，前者是否更新 |

## 协作关系
control.Service 启动时创建 Scheduler；check_update 方法直接调用 Latest。外部依赖：GitHub REST 接口（公开、无需登录）。

## 验证方式
- 测试入口：`cd core && go test ./internal/update/...`
- 测试输入：httptest 假 GitHub（可返回指定 tag_name、html_url、状态码），把接口地址作为可替换的包级变量
- 预期结果：
- [ ] 当前 1.2.0、最新 v1.3.0 → is_newer 为 true，url 为 html_url
- [ ] 最新 v1.2.0 或 v1.1.9 → is_newer 为 false
- [ ] tag 为 1.3.0-beta → is_newer 为 false
- [ ] 假 GitHub 收到的请求只有规定的两个请求头，路径为 /repos/<仓库>/releases/latest
- [ ] 假 GitHub 返回 500 或断网：manual 返回 update_unavailable；自动检查不报错
- [ ] 用可替换的时钟推进 24 小时 → 发生第二次检查；Stop 后再推进不再检查
- [ ] 未注入仓库名 → 不发请求，is_newer 为 false
