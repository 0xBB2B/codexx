---
name: 01-core-installer-url
description: 核心检查更新时从附件清单挑本平台安装包，返回安装包地址并标明是不是安装包
---
# 核心挑选本平台安装包地址

## 目标
核心检查到新版本时，返回本平台安装包的下载地址；附件里没有本平台安装包时返回发布页地址，并用一个布尔字段告诉两端返回的是哪一种。

## 业务规则（来源：PRD 01-direct-download）
- 发现新版本时，在最新发布信息的附件清单（GitHub `assets[]`，每项有 `name`、`browser_download_url`）里找本平台安装包：macOS 文件名恰好是 `model-bridge-<版本>-macos.dmg`，Windows 恰好是 `model-bridge-<版本>-windows-setup.exe`，`<版本>` 是去掉开头 `v` 的标签名。
- 下载地址必须以 `https://github.com/` 开头才算找到。
- 找到：结果的地址是安装包下载地址，`installer` 为 true。没找到（附件里没有、名字对不上、地址不以 `https://github.com/` 开头、不是 macOS/Windows）：地址是发布页 `html_url`，`installer` 为 false，仍算新版本。
- 判断新旧、发布页地址必须以 `https://github.com/` 开头的现有规则不变。
- 自动检查和手动检查用同一结果（status 里的 update 和 check_update 的返回都带这个字段）。
- 核心自己不下载安装包，只返回地址。

## 涉及文件
- core/internal/update/check.go（修改）
- core/internal/update/check_test.go（修改）
- core/internal/control/service.go（修改）
- core/internal/control/ 下现有 update 相关测试（修改）
- core/PROTOCOL.md（修改）

## 成品定义
core/PROTOCOL.md 中 `check_update` 一行改为：

```
| check_update | {"manual": 布尔} | {"latest_version": 字符串或 null, "url": 字符串或 null, "installer": 布尔, "is_newer": 布尔} |
```

status 示例中的 update 一行改为：

```
  "update": {"latest_version": null, "url": null, "installer": false},
```

并在协议文档里说明 update 字段的地方补一句（放在 update 字段说明处；没有说明处就紧跟 status 示例之后）：

```
`update.url` 在 `installer` 为 true 时是本平台安装包（macOS `model-bridge-<版本>-macos.dmg`，Windows `model-bridge-<版本>-windows-setup.exe`）的下载地址；为 false 时是这个版本的发布页。
```

## 函数清单
### core/internal/update/check.go
| 函数名 | 职责 |
|---|---|
| `Config`（结构体加字段 `AssetSuffix`） | 本平台安装包文件名后缀，`-macos.dmg` 或 `-windows-setup.exe`，空串表示本平台没有安装包 |
| `Result`（结构体加字段 `Installer`） | URL 是否为安装包下载地址 |
| `Check` | 解析发布信息时一并解析 `assets[]`；判定为新版本后调用 `installerURL` 选地址 |
| `installerURL`（新增） | 按 `model-bridge-<版本><AssetSuffix>` 精确匹配附件名，且地址以 `releaseURLBase` 开头时返回该地址 |

### core/internal/control/service.go
| 函数名 | 职责 |
|---|---|
| `UpdateInfo`、`UpdateResult`（各加 `installer` JSON 字段） | 把 `Result.Installer` 传给两端 |
| `CheckUpdate` | 构造 `update.Config` 时按 `runtime.GOOS` 填 `AssetSuffix`（darwin、windows 以外为空串）；把 Installer 写进返回值和 status 的 update |

## 协作关系
两端调用 check_update 或读 status → `Service.CheckUpdate` → `update.Check`（请求 GitHub `releases/latest`）→ `installerURL`。

## 验证方式
- 测试入口：`update.Check`（用 check_test.go 现有的 httptest 假 GitHub），`control` 层 check_update 请求与 status 的 update 字段。
- 测试输入：最新发布 tag `v0.1.2`、`html_url` 为 `https://github.com/o/r/releases/tag/v0.1.2`，`assets` 取下列组合；当前版本 0.1.1；`AssetSuffix` 分别为 `-macos.dmg`、`-windows-setup.exe`、空串。
- 预期结果：
- [ ] 附件含 `model-bridge-0.1.2-macos.dmg`（地址 `https://github.com/o/r/releases/download/v0.1.2/model-bridge-0.1.2-macos.dmg`）和 windows 包：后缀 `-macos.dmg` → URL 是 dmg 地址、Installer true；后缀 `-windows-setup.exe` → setup.exe 地址、true
- [ ] 附件名 `model-bridge-0.1.1-macos.dmg` 或 `ModelBridge.dmg` → URL 是 html_url、Installer false、IsNewer true
- [ ] 附件名对但地址是 `https://example.com/...` → html_url、false
- [ ] `assets` 缺失或为空 → html_url、false
- [ ] 后缀空串 → html_url、false
- [ ] 不是新版本时 Installer false、URL 空（现有行为不变）
- [ ] check_update 返回和 status.update 都带 `installer`，值与 Result 一致
