---
name: 01-repo-scaffold
description: 建立 core（Go）、macos（Swift Package）、windows（.NET）三个子项目骨架和统一命令
---
# 仓库骨架

## 目标
仓库里有三个能各自编译、各自跑通一个空测试的子项目，以及统一的构建和测试命令。

## 业务规则（来源：PRD OVERVIEW、07-release）
- 结构：核心程序（Go）负责转发、配置读写、模型目录、更新检查；macOS 外壳（Swift）和 Windows 外壳（C#/.NET WPF）只负责界面、系统凭据库、登录时启动、重启 ChatGPT。
- macOS 只支持 Apple 芯片、macOS 13 及以上；Windows 支持 64 位 Windows 10 及以上。
- 产品显示名"模型接入助手"，代码和文件里用中性英文名 ModelBridge / model-bridge；任何地方都不出现真实地址、Key、中台品牌（AGENTS.md 项目约束）。
- 本机 Mac 没有 .NET，windows/ 只在 GitHub Actions 的 windows-2025 构建机上编译和测试。

## 涉及文件
- core/go.mod（新建）
- core/cmd/model-bridge-core/main.go（新建）
- core/internal/version/version.go（新建）
- macos/Package.swift（新建）
- macos/Sources/ModelBridge/App.swift（新建）
- macos/Tests/ModelBridgeTests/SmokeTests.swift（新建）
- windows/ModelBridge.sln（新建）
- windows/ModelBridge.App/ModelBridge.App.csproj（新建）
- windows/ModelBridge.App/App.xaml、App.xaml.cs（新建）
- windows/ModelBridge.Tests/ModelBridge.Tests.csproj（新建）
- Makefile（新建）
- .gitignore（新建）

## 成品定义

`core/go.mod`（Go 版本写本机 `go version` 的主次版本；依赖版本在落盘前用 `go list -m -versions` 查最新稳定版，以下为 2026-11-11 查到的值）：
```
module modelbridge

go 1.27

require (
	github.com/klauspost/compress v1.20.4
	github.com/pelletier/go-toml/v2 v2.4.3
)
```

`macos/Package.swift`：
```swift
// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ModelBridge",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "ModelBridge", path: "Sources/ModelBridge"),
        .testTarget(name: "ModelBridgeTests", dependencies: ["ModelBridge"], path: "Tests/ModelBridgeTests"),
    ]
)
```

`windows/ModelBridge.App/ModelBridge.App.csproj`：
```xml
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>WinExe</OutputType>
    <TargetFramework>net10.0-windows10.0.19041.0</TargetFramework>
    <SupportedOSPlatformVersion>10.0.17763.0</SupportedOSPlatformVersion>
    <UseWPF>true</UseWPF>
    <UseWindowsForms>true</UseWindowsForms>
    <Nullable>enable</Nullable>
    <AssemblyName>ModelBridge</AssemblyName>
    <RootNamespace>ModelBridge</RootNamespace>
  </PropertyGroup>
</Project>
```

`windows/ModelBridge.Tests/ModelBridge.Tests.csproj`（MSTest 版本落盘前到 nuget.org 查最新稳定版）：
```xml
<Project Sdk="MSTest.Sdk/<最新稳定版>">
  <PropertyGroup>
    <TargetFramework>net10.0-windows10.0.19041.0</TargetFramework>
    <UseWPF>true</UseWPF>
    <UseWindowsForms>true</UseWindowsForms>
    <Nullable>enable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <ProjectReference Include="..\ModelBridge.App\ModelBridge.App.csproj" />
  </ItemGroup>
</Project>
```

`Makefile`：
```make
.PHONY: test test-core test-macos build-core

test: test-core test-macos

test-core:
	cd core && go test ./...

test-macos:
	cd macos && swift test

build-core:
	cd core && GOOS=darwin GOARCH=arm64 go build -o ../dist/darwin-arm64/model-bridge-core ./cmd/model-bridge-core
	cd core && GOOS=windows GOARCH=amd64 go build -o ../dist/windows-amd64/model-bridge-core.exe ./cmd/model-bridge-core
```

`.gitignore`：
```
dist/
.build/
macos/.build/
windows/**/bin/
windows/**/obj/
.playwright-mcp/
```

## 新增技术
| 名称 | 类别 | 来源 | 用途 | 现有手段为什么不够 | 替代方案为什么不选 | 版本策略 |
|---|---|---|---|---|---|---|
| Go | 语言 | Claude 提议 | 核心程序 | 项目为空 | Rust 需自拼流式反向代理；两个外壳各写一遍要测两套安全逻辑 | 最新稳定版 |
| Swift（SwiftUI/AppKit/XCTest） | 语言 | Claude 提议 | macOS 外壳 | Go 无原生 macOS 界面库 | Tauri/Wails 是网页内核，违反"原生不是网页" | Xcode 自带最新稳定版 |
| C# + .NET（WPF、WinForms NotifyIcon） | 语言+框架 | Claude 提议 | Windows 外壳 | Go 无成熟原生 Windows 界面库 | WinUI 3 需额外装 Windows App SDK 运行时；Win32 C++ 开发量大数倍 | 最新 LTS（.NET 10） |
| MSTest | 测试工具 | Claude 提议 | Windows 外壳测试 | .NET 不自带测试框架 | xUnit 可用，但 MSTest 由微软官方维护、少一个第三方来源 | 最新稳定版 |
| github.com/pelletier/go-toml/v2 | 库 | Claude 提议 | 解析 config.toml 并拿到键的字节位置 | 标准库不支持 TOML；需字节位置才能只改接入项 | BurntSushi/toml 拿不到字节位置 | 最新稳定版 |
| github.com/klauspost/compress | 库 | Claude 提议 | 解压 zstd 请求体 | 标准库不支持 zstd | DataDog/zstd 依赖 cgo，交叉编译 Windows 困难 | 最新稳定版 |

## 函数清单
### core/cmd/model-bridge-core/main.go
| 函数名 | 职责 |
|---|---|
| `main` | 程序入口；本计划只打印版本号后退出，后续计划替换为控制循环 |
### core/internal/version/version.go
| 函数名 | 职责 |
|---|---|
| `Current` | 返回编译时通过 `-ldflags -X` 注入的版本号，未注入时返回 `0.0.0-dev` |
### macos/Sources/ModelBridge/App.swift
| 函数名 | 职责 |
|---|---|
| `ModelBridgeApp`（`@main` 结构体） | 应用入口，本计划只建一个空的菜单栏入口 |
### windows/ModelBridge.App/App.xaml.cs
| 函数名 | 职责 |
|---|---|
| `App.OnStartup` | 应用入口，本计划只启动后立即保持运行、不显示窗口 |

## 协作关系
三个子项目互不依赖编译；后续计划由外壳在运行时启动 core 的可执行文件。

## 验证方式
- 测试入口：`make test`（本机）；`dotnet test windows/ModelBridge.sln`（windows-2025 构建机）
- 测试输入：无
- 预期结果：Go 和 Swift 测试全部通过；`version.Current()` 未注入时返回 `0.0.0-dev`，用 `-ldflags "-X modelbridge/internal/version.value=1.3.0"` 编译后返回 `1.3.0`
- [ ] `make test` 通过
- [ ] `make build-core` 产出 darwin-arm64 和 windows-amd64 两个可执行文件
- [ ] windows 测试项目在构建机上能编译并跑通一个空测试
