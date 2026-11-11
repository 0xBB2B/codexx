# 模型接入助手（ModelBridge）

一个托盘小工具：把 ChatGPT 桌面版里的 Codex 接到你自己的模型服务上。你填服务地址和 Key，它负责改好 Codex 配置，并在后台转发请求。

## 前提

电脑上已安装 ChatGPT 桌面版。

## 下载

到本仓库的 [Releases 页面](../../releases) 下载：

- macOS（Apple 芯片，macOS 13 及以上）：`model-bridge-<版本>-macos.dmg`
- Windows（64 位，Windows 10 1809 及以上）：`model-bridge-<版本>-windows-setup.exe`

## 首次打开

安装包没有做开发者签名，第一次打开系统会拦一下，手动放行即可：

- macOS：把 App 拖进「应用程序」，双击打开被拦后，进入 系统设置 > 隐私与安全性，点「仍要打开」。
- Windows：SmartScreen 弹窗里点「更多信息 > 仍要运行」。

升级到新版本后，钥匙串可能会问一次是否允许访问，选允许即可。

## 卸载

- macOS：用菜单里的「卸载模型接入助手…」。不要直接把 App 拖进废纸篓，否则 Codex 配置不会被还原。
- Windows：用托盘菜单卸载，或者从「应用和功能」卸载。

卸载只还原接入项，不会删除 ChatGPT、聊天数据和 `auth.json`。

## 构建和测试

```
make test
dotnet test --solution windows/ModelBridge.sln
```

打包 macOS 安装包：

```
scripts/package-macos.sh 0.0.1 <owner>/<repo>
```

产物在 `dist/` 下。Windows 安装包在发布正式版时由 `.github/workflows/release.yml` 构建；拉取请求时只验证打包能跑通，不上传。
