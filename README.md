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

产物在 `dist/` 下。Windows 安装包只在发版时由 `.github/workflows/release.yml` 构建；拉取请求时只验证打包能跑通，不上传。

## 发版

版本号只由标签决定。拉取请求合并到 main 之后，在 main 的提交上打标签并推送：

```
git switch main && git pull
git tag v1.2.3
git push origin v1.2.3
```

- 标签格式是 `v<主>.<次>.<修订>`，可以带后缀（如 `v1.3.0-beta.1`）。带后缀的发成预发布版，已装正式版的用户收不到它的更新提示。
- 标签必须打在 main 的提交上。拉取请求用 squash 合并时，功能分支上的提交不在 main 上，所以要合并后再打。
- 自动流程先建草稿发布页，两端测试、打包、上传都成功后才公开，发布说明由 GitHub 自动生成。中途失败会删掉草稿；修好后删除标签再重推。
- 重推时如果提示这个标签「已有发布页」，先到 Releases 页面删掉那个发布页（包括没删掉的草稿）再推。
