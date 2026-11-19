# update-direct-download 实施计划
## 阶段 1：核心
- [01-core-installer-url](01-core-installer-url.md) — 核心检查更新时挑本平台安装包地址，返回 installer 字段
## 阶段 2：两端（可并行）
- [02-macos-update-text](02-macos-update-text.md) — macOS 按 installer 写更新项副标题和可访问名称 [依赖: 01-core-installer-url]
- [03-windows-update-text](03-windows-update-text.md) — Windows 按 installer 写更新项副标题和可访问名称 [依赖: 01-core-installer-url]
