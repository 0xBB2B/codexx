---
name: 08-app-icon-assets
description: 用 Go 标准库写图标生成程序，按规范画出各尺寸图标并输出 icns、ico、菜单栏和托盘 PNG，接进打包
---
# 应用图标资源与打包

## 目标
一条命令按 DESIGN.md「应用图标」的几何参数生成全部图标文件，并让 macOS App、dmg、Windows exe、安装程序、卸载项都用上应用图标。

## 业务规则（来源：PRD 04-app-icon）
- 图形：100×100 画布，圆心 (50,50)、半径 34 的圆 + 两个箭头，路径 `M31.5 42.6H68.5l-9.6-9.6M68.5 57.4H31.5l9.6 9.6`；只描线不填充，圆头线端和拐角。线宽：显示尺寸 ≥64px 为边长 6%；24～48px 为 9%；16px 为 12% 且去掉外圈，箭头路径改为 `M22 38H78l-13-13M78 62H22l13 13`。
- macOS 应用图标：底板 #0f766e、图形 #ffffff；1024px 画布上底板 824×824 居中、四周留 100px、圆角 184px，其余尺寸按比例；32px 四周留 2px、16px 四周留 1px；图形缩到底板大小居中。尺寸 16、32、128、256、512、1024（icns 里含 16、32、64、128、256、512、1024 各像素尺寸以覆盖 @2x）。
- Windows 应用图标：底板铺满、#0f766e，256px 画布上圆角 48px，按比例；图形 #ffffff 缩到画布 88% 居中。ico 含 16、24、32、48、256。
- macOS 菜单栏图标：18×18（以及 36×36 的 @2x），无底板，线宽 9%，图形放大 1.2 倍居中，纯黑带透明度，作模板图标用。
- Windows 托盘图标：16×16（以及 32×32 给高 DPI），16px 规则（线宽 12%、无外圈），两套：#172b3a（浅色任务栏）和 #ffffff（深色任务栏）。
- 生成结果提交进仓库；只用 Go 标准库（image、image/png、encoding/binary），抗锯齿按"点到线段的距离"计算覆盖率。
- 打包：macOS App 包的 Info.plist 指向图标、icns 放进 `Contents/Resources`；dmg 里 App 显示该图标。Windows：exe 用 ico 作应用图标（csproj `ApplicationIcon`），安装程序 `SetupIconFile`，卸载项显示 exe 图标（已有 `UninstallDisplayIcon`）。

## 涉及文件
- 新建 `scripts/icongen/go.mod`、`main.go`、`draw.go`、`draw_test.go`、`formats.go`、`formats_test.go`
- 新建（生成产物）`macos/Resources/AppIcon.icns`、`macos/Resources/MenuBarIcon.png`、`macos/Resources/MenuBarIcon@2x.png`、`windows/ModelBridge.App/Assets/AppIcon.ico`、`windows/ModelBridge.App/Assets/TrayIconLight.ico`、`windows/ModelBridge.App/Assets/TrayIconDark.ico`
- 修改 `macos/Resources/Info.plist`、`scripts/package-macos.sh`、`windows/ModelBridge.App/ModelBridge.App.csproj`、`windows/installer/model-bridge.iss`、`Makefile`

## 成品定义
`scripts/icongen/go.mod`：
```
module icongen

go 1.27
```
`macos/Resources/Info.plist` 在 `CFBundleExecutable` 一行之后加：
```xml
  <key>CFBundleIconFile</key><string>AppIcon</string>
```
`windows/ModelBridge.App/ModelBridge.App.csproj` 的第一个 `PropertyGroup` 里加：
```xml
    <ApplicationIcon>Assets\AppIcon.ico</ApplicationIcon>
```
并加入资源项（托盘图标在运行时读取）：
```xml
  <ItemGroup>
    <Resource Include="Assets\TrayIconLight.ico" />
    <Resource Include="Assets\TrayIconDark.ico" />
    <Resource Include="Assets\AppIcon.ico" />
  </ItemGroup>
```
`windows/installer/model-bridge.iss` 的 `[Setup]` 段加：
```ini
SetupIconFile=..\ModelBridge.App\Assets\AppIcon.ico
```
`Makefile` 加目标（并加进 `.PHONY`，不加进 `test`，测试另加 `test-icongen` 并加进 `test`）：
```make
icons:
	cd scripts/icongen && go run . ../..

test-icongen:
	cd scripts/icongen && go test ./...
```

## 函数清单
### scripts/icongen/draw.go
| 函数名 | 职责 |
|---|---|
| `Glyph` | 一种尺寸的图形参数：线宽比例、是否有外圈、箭头路径、缩放 |
| `GlyphFor` | 按显示尺寸返回对应的图形参数 |
| `RenderAppIconMac` / `RenderAppIconWin` | 画带底板的应用图标 |
| `RenderMono` | 画无底板的单色图标（给定颜色） |
| `coverage` | 像素中心到圆或线段的距离换算成覆盖率 |
### scripts/icongen/formats.go
| 函数名 | 职责 |
|---|---|
| `WriteICNS` | 把多张 PNG 写成 icns |
| `WriteICO` | 把多张 PNG 写成 ico（PNG 内嵌格式） |
### scripts/icongen/main.go
| 函数名 | 职责 |
|---|---|
| `main` | 以仓库根为参数，生成上面列出的全部文件 |

### scripts/package-macos.sh
| 位置 | 职责 |
|---|---|
| 组装 App 包处 | 复制 `AppIcon.icns`、`MenuBarIcon*.png` 到 `Contents/Resources` |

## 协作关系
`main` → `Render*` → `WriteICNS` / `WriteICO` / `png.Encode`。plan 09 读取这里生成的菜单栏、托盘图标。

## 验证方式
- 测试入口：`cd scripts/icongen && go test ./...`；包级入口 `RenderAppIconMac(size)`、`RenderAppIconWin(size)`、`RenderMono(size, color)`、`WriteICNS`、`WriteICO`。
- 预期结果：
  - [ ] `RenderAppIconMac(1024)`：(50,50) 像素透明；(512,512) 是 #ffffff 或 #0f766e；(150,512) 是 #0f766e（底板内、图形外）。
  - [ ] `RenderAppIconWin(256)`：(2,2) 透明（圆角处）；(128,4) 是 #0f766e（铺满）。
  - [ ] `RenderMono(16, …)`：没有外圈（距中心约 34% 半径的一圈上没有不透明像素），有两条箭头。
  - [ ] `WriteICNS` 输出以 `icns` 魔数开头、长度字段等于文件长度，含 7 个尺寸条目；`WriteICO` 输出头部条目数为 5，每个条目指向合法 PNG。
  - [ ] 运行 `make icons` 后仓库里出现上面列出的 6 个文件，再运行一次文件内容不变（可重复生成）。
  - [ ] `scripts/package-macos.sh` 打出的 App 包里有 `Contents/Resources/AppIcon.icns`，Info.plist 有 `CFBundleIconFile`。
