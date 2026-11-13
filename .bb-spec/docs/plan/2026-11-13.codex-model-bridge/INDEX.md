# codex-model-bridge 接入方式 实施计划
## 阶段 1：核心能力（可并行）
- [01-relay-only-config](01-relay-only-config.md) — Codex 配置写入支持「只用中台」接入项
- [02-login-detect](02-login-detect.md) — 运行 codex login status 判断是否已登录
- [03-relay-only-catalog](03-relay-only-catalog.md) — 模型目录支持只放中台模型
- [04-relay-only-proxy](04-relay-only-proxy.md) — 「只用中台」时本机转发拒绝官方请求
## 阶段 2：服务层
- [05-access-mode-service](05-access-mode-service.md) — 后台服务按检测结果选择并切换接入方式 [依赖: 01-relay-only-config, 02-login-detect, 03-relay-only-catalog, 04-relay-only-proxy]
## 阶段 3：界面
- [06-macos-access-mode-ui](06-macos-access-mode-ui.md) — macOS 主窗口与首次设置的接入方式界面 [依赖: 05-access-mode-service]
