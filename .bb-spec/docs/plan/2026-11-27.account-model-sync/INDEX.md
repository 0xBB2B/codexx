# account-model-sync 实施计划
## 阶段 1：各自独立的部件
- [catalog-account-slugs](01-catalog-account-slugs.md) — catalog 接受查到的账号模型名单并存取名单文件
- [proxy-official-hook](02-proxy-official-hook.md) — 转发带账号标识的官方请求时把请求头交给回调
- [macos-account-notice](04-macos-account-notice.md) — macOS 按 status 新字段显示不自动消失的提示
- [windows-account-notice](05-windows-account-notice.md) — Windows 按 status 新字段显示不自动消失的提示
## 阶段 2：串起来
- [control-account-sync](03-control-account-sync.md) — 按时机查账号模型列表、重建清单、推送 status [依赖: catalog-account-slugs, proxy-official-hook]
