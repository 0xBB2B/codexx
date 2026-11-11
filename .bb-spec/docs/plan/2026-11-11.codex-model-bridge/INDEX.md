# codex-model-bridge 实施计划
## 阶段 1：骨架
- [01-repo-scaffold](01-repo-scaffold.md) — core、macos、windows 三个子项目骨架和统一命令
## 阶段 2：核心程序
- [02-core-model-catalog](02-core-model-catalog.md) — 参数表、官方清单、中台列表、合并模型目录 [依赖: 01-repo-scaffold]
- [03-core-proxy-routing](03-core-proxy-routing.md) — 本机转发、分流、凭据隔离、中台错误中性化 [依赖: 01-repo-scaffold]
- [04-core-request-normalize](04-core-request-normalize.md) — 中途换模型、去官方专有工具、代为压缩 [依赖: 03-core-proxy-routing]
- [05-core-codex-config](05-core-codex-config.md) — 接入项写入、还原、手改检测 [依赖: 01-repo-scaffold]
- [06-core-control](06-core-control.md) — 控制协议与核心主流程 [依赖: 02、04、05]
- [07-core-update-check](07-core-update-check.md) — GitHub 新版检查 [依赖: 06-core-control]
## 阶段 3：macOS 外壳
- [08-macos-system](08-macos-system.md) — 核心对接、钥匙串、登录启动、单实例、重启 ChatGPT [依赖: 06-core-control]
- [09-macos-ui](09-macos-ui.md) — 设置窗口、主窗口、菜单栏菜单、弹窗 [依赖: 08-macos-system]
## 阶段 4：Windows 外壳
- [10-windows-system](10-windows-system.md) — 核心对接、凭据管理器、登录启动、单实例、重启 ChatGPT [依赖: 06-core-control]
- [11-windows-ui](11-windows-ui.md) — 设置窗口、主窗口、托盘菜单、弹窗 [依赖: 10-windows-system]
- [12-windows-installer](12-windows-installer.md) — Inno Setup 安装与可中止的卸载 [依赖: 11-windows-ui]
## 阶段 5：发布
- [13-release-pipeline](13-release-pipeline.md) — CI 测试、发布打包、README [依赖: 07、09、12]
