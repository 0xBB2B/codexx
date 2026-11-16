---
name: 03-pages-workflow
description: 新增 GitHub Actions 官网发布流程：默认分支上官网文件改动或手动运行时，先检查后发布
---
# 官网发布流程

## 目标
官网目录有改动并推到默认分支，或在 Actions 页面手动运行时，先跑检查程序；通过后只把官网目录发布到 GitHub Pages。

## 业务规则（来源：PRD 02-site-publish）
- 只发布 `site/` 目录里的文件。
- 流程在两种情况下运行：推到默认分支、且改到了 `site/` 下的文件；或者在 Actions 页面手动运行。只改了别的文件的推送不触发。默认分支以外的推送和手动运行都不发布。
- 先检查、后发布：检查失败时发布这一步不运行，线上保持上一版。
- 检查失败时日志列出违规的文件、行号和原因，不打印违规原文（检查程序本身已保证）。
- 发布后 `https://0xbb2b.github.io/codexx/` 显示这次的 `site/` 内容。
- 前提：仓库设置里 Pages 的来源设为 GitHub Actions。这是对外公开的操作，由用户自己设置或明确同意后再做，本计划不做。
- 检查只针对 `site/`，检查程序自己的源码不在检查范围内。

## 涉及文件
- 新建 `.github/workflows/pages.yml`

## 新增技术
| 名称 | 类别 | 来源 | 用途 | 现有手段为什么不够 | 替代方案为什么不选 | 版本策略 |
|---|---|---|---|---|---|---|
| `actions/configure-pages` | CI Action | 用户点名 | 在流程里读取仓库的 Pages 设置 | 现有 checkout、setup-go、setup-dotnet 都不能发布 Pages | 自己调 GitHub 接口发布，要手写鉴权和上传，容易错 | 最新稳定版（2026-11-16 查询为 v6.0.0） |
| `actions/upload-pages-artifact` | CI Action | 用户点名 | 只把 `site/` 打包成待发布内容 | 现有插件产不出 Pages 认的包 | 通用 upload-artifact 包格式不对 | 最新稳定版（2026-11-16 查询为 v5.0.0） |
| `actions/deploy-pages` | CI Action | 用户点名 | 检查通过后把包发布到 Pages | 同上 | 推 gh-pages 分支要多维护一个分支，也做不到检查不过就不发 | 最新稳定版（2026-11-16 查询为 v5.0.1） |

## 成品定义

`.github/workflows/pages.yml`（版本号落盘前再用 `gh api /repos/actions/<名>/releases/latest --jq .tag_name` 核对一次，有更新就换成最新的）：

```yaml
name: pages
on:
  push:
    paths:
      - 'site/**'
  workflow_dispatch:
permissions:
  contents: read
concurrency:
  group: pages
  cancel-in-progress: false
jobs:
  check:
    # 推送的 paths 过滤不能限定分支名，默认分支会从 feat 分支换成 main，所以在这里比对
    if: github.ref_name == github.event.repository.default_branch
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/setup-go@v7.0.0
        with:
          go-version-file: scripts/sitecheck/go.mod
          cache: false
      - name: Check site
        working-directory: scripts/sitecheck
        run: go run . ../../site
  deploy:
    needs: check
    runs-on: ubuntu-latest
    permissions:
      pages: write
      id-token: write
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v7.0.1
      - uses: actions/configure-pages@v6.0.0
      - uses: actions/upload-pages-artifact@v5.0.0
        with:
          path: site
      - id: deployment
        uses: actions/deploy-pages@v5.0.1
```

## 函数清单
无过程式代码，流程文件即成品。

## 协作关系
`check` 任务调用 `scripts/sitecheck`（计划 01）检查 `site/`（计划 02）；`deploy` 依赖 `check` 成功。

## 验证方式
- 测试入口：本地语法检查 `ruby -ryaml -e 'YAML.load_file(".github/workflows/pages.yml")'`；推送后在 GitHub Actions 页面看运行结果。
- 预期结果：
  - [ ] 文件能被解析成 YAML，`on.push.paths` 只有 `site/**`，有 `workflow_dispatch`。
  - [ ] `deploy` 有 `needs: check`；`upload-pages-artifact` 的 `path` 是 `site`。
  - [ ] `check` 任务的 `if` 限定默认分支。
  - [ ] 所有 `uses:` 都写到具体版本号，三个 Pages 插件是查询到的最新稳定版。
  - [ ] 真实发布验证（开启 Pages 后由用户确认再做）：推一个只改 `site/` 的提交 → 检查和发布通过；推一个加了 `https://other.example.org/` 的提交 → 检查失败、发布没运行。
