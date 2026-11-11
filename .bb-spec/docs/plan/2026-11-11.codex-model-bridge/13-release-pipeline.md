---
name: 13-release-pipeline
description: GitHub Actions 在发布正式版时打包 macOS dmg 和 Windows 安装包并上传；macOS 打包脚本；README 放行说明
---
# 打包发布流程

## 目标
维护者在 GitHub 发布正式版本后，自动产出两个安装包并附到发布页；平时提交时自动跑测试。

## 业务规则（来源：PRD 07-release、06-update-check）
- 发布正式版（release published 且非预发布）触发打包；草稿、预发布不触发。推送和拉取请求时只跑测试。
- 版本号 = 发布标签去掉开头 `v`；注入核心程序（`modelbridge/internal/version.value`）、macOS Info.plist、Windows 程序和安装包。仓库名注入核心程序（`modelbridge/internal/update.repository`，取 `${{ github.repository }}`）。
- macOS：只编译 arm64、最低 13.0；组装 `ModelBridge.app`（外壳可执行文件、`Contents/MacOS/model-bridge-core`、Info.plist），整体做临时签名（`codesign --force --deep -s -`），用系统自带 `hdiutil` 生成含"应用程序"快捷方式的 `model-bridge-<版本>-macos.dmg`。
- Windows：`dotnet publish` 自包含 win-x64 到 `dist\windows-amd64`，放入核心程序，用构建机预装的 Inno Setup 生成 `model-bridge-<版本>-windows-setup.exe`。
- 上传用构建机预装的 `gh release upload`，不用第三方 Action。任一平台失败 → 不上传该平台残缺文件，流程失败。
- 工作流、脚本、日志里不出现真实地址、Key、中台品牌；工作流不需要任何自定义密钥（只用自带的 GITHUB_TOKEN）。
- README 写明 macOS 首次打开放行步骤（系统设置 > 隐私与安全性 > 仍要打开）和 Windows SmartScreen 放行步骤（更多信息 > 仍要运行），以及升级后钥匙串可能询问一次是否允许访问。

## 涉及文件
- .github/workflows/ci.yml（新建）
- .github/workflows/release.yml（新建）
- scripts/package-macos.sh（新建）
- README.md（新建）

## 成品定义

`.github/workflows/ci.yml`：
```yaml
name: ci
on:
  push:
  pull_request:
jobs:
  core-and-macos:
    runs-on: macos-26
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/setup-go@v7.0.0
        with:
          go-version-file: core/go.mod
      - run: make test
  windows:
    runs-on: windows-2025
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/setup-dotnet@v6.1.0
        with:
          dotnet-version: '10.0.x'
      - run: dotnet test windows/ModelBridge.sln
```

`.github/workflows/release.yml`：
```yaml
name: release
on:
  release:
    types: [published]
permissions:
  contents: write
jobs:
  macos:
    if: ${{ !github.event.release.prerelease }}
    runs-on: macos-26
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/setup-go@v7.0.0
        with:
          go-version-file: core/go.mod
      - run: make test
      - run: scripts/package-macos.sh "${GITHUB_REF_NAME#v}" "${{ github.repository }}"
      - run: gh release upload "$GITHUB_REF_NAME" "dist/model-bridge-${GITHUB_REF_NAME#v}-macos.dmg"
        env:
          GH_TOKEN: ${{ github.token }}
  windows:
    if: ${{ !github.event.release.prerelease }}
    runs-on: windows-2025
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/setup-go@v7.0.0
        with:
          go-version-file: core/go.mod
      - uses: actions/setup-dotnet@v6.1.0
        with:
          dotnet-version: '10.0.x'
      - run: |
          VERSION="${GITHUB_REF_NAME#v}"
          (cd core && go test ./... && GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X modelbridge/internal/version.value=$VERSION -X modelbridge/internal/update.repository=${{ github.repository }}" -o ../dist/windows-amd64/model-bridge-core.exe ./cmd/model-bridge-core)
          dotnet test windows/ModelBridge.sln
          dotnet publish windows/ModelBridge.App/ModelBridge.App.csproj -c Release -o dist/windows-amd64 -p:Version="$VERSION"
          "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" windows/installer/model-bridge.iss "/DAppVersion=$VERSION" "/DSourceDir=$(cygpath -w "$PWD/dist/windows-amd64")"
          gh release upload "$GITHUB_REF_NAME" "dist/model-bridge-$VERSION-windows-setup.exe"
        env:
          GH_TOKEN: ${{ github.token }}
```

## 函数清单
### scripts/package-macos.sh
| 函数名 | 职责 |
|---|---|
| `build_core` | 带版本和仓库注入编译 darwin/arm64 核心程序 |
| `build_shell` | `swift build -c release --arch arm64` 编译外壳 |
| `assemble_app` | 组装 ModelBridge.app，替换 Info.plist 版本号 |
| `sign_adhoc` | 对整个 .app 临时签名 |
| `make_dmg` | 用 hdiutil 生成含"应用程序"快捷方式的 dmg |

## 协作关系
ci.yml 在每次推送时跑全部测试；release.yml 依赖 01 的 Makefile、08 的 Info.plist、12 的安装脚本。外部依赖：GitHub Actions 构建机（macos-26 arm64、windows-2025，预装 Inno Setup 6 和 gh）。

## 验证方式
- 测试入口：本机运行 `scripts/package-macos.sh 0.0.1 example/model-bridge`；在测试仓库发布 `v0.0.1` 正式版和一个预发布版
- 测试输入：测试仓库
- 预期结果：
- [ ] 本机生成 `dist/model-bridge-0.0.1-macos.dmg`；挂载后 .app 里 `lipo -archs` 只有 arm64，Info.plist 版本为 0.0.1、最低 13.0；`codesign -dv` 显示临时签名
- [ ] 测试仓库发布正式版 → 发布页出现两个规定文件名的安装包；发布预发布版 → release 工作流的两个任务都跳过
- [ ] 安装后工具显示的版本号与标签一致；更新检查指向测试仓库
- [ ] 在仓库、两个安装包解开后的全部文件、工作流日志里搜索测试用地址、Key、品牌词，都没有命中
- [ ] README 含两个平台的放行步骤和钥匙串提示
