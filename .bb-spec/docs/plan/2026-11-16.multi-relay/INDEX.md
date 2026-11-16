# multi-relay 实施计划
## 阶段 1：可并行的基础
- [01-core-relay-catalog](01-core-relay-catalog.md) — 模型目录支持多个中转配置
- [08-app-icon-assets](08-app-icon-assets.md) — 图标生成程序、图标文件与打包
- [10-site-readme-rename](10-site-readme-rename.md) — 官网与 README 改称中转
## 阶段 2：核心程序
- [02-core-proxy-routing](02-core-proxy-routing.md) — 本机转发按配置分流 [依赖: 01-core-relay-catalog]
- [03-core-control-relays](03-core-control-relays.md) — 控制协议与服务支持多配置 [依赖: 02-core-proxy-routing]
## 阶段 3：两端外壳
- [04-macos-relay-model](04-macos-relay-model.md) — macOS 多配置保存与界面模型 [依赖: 03-core-control-relays]
- [06-windows-relay-model](06-windows-relay-model.md) — Windows 多配置保存与界面模型 [依赖: 03-core-control-relays]
- [05-macos-main-window](05-macos-main-window.md) — macOS 主窗口新布局与弹窗 [依赖: 04-macos-relay-model]
- [07-windows-main-window](07-windows-main-window.md) — Windows 主窗口新布局与弹窗 [依赖: 06-windows-relay-model]
## 阶段 4：图标接入
- [09-menubar-tray-icons](09-menubar-tray-icons.md) — 菜单栏与托盘图标 [依赖: 08-app-icon-assets, 05-macos-main-window, 07-windows-main-window]
