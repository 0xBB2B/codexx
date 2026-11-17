# DESIGN.md

> 本项目的设计规范。出原型、写界面代码、审查界面代码都以它为准；新建和修改都要经用户同意后才写入。
>
> 写法要求：
> - 每项写具体数值和单位（px、ms 等），不写"简洁""大方""适中"这类形容词代替数值。
> - 颜色按用途命名，每个颜色写色值。
> - 间距和圆角的每个值都是 4 的倍数（0 也算）；表示全圆角（胶囊形按钮、圆形头像）时写 9999px，不算违规。
> - 颜色对比度符合 WCAG 2.2 AA：正文文字对背景至少 4.5:1；大号文字（24px 及以上，或 18.66px 及以上加粗）、图标、输入框和按钮的边框对相邻背景至少 3:1。
> - 项目用不上的一节写"不适用：<原因>"。

## 1. 风格定位

给想在 Codex 里用中转模型的普通用户用的跨平台桌面小工具，要让人觉得它像系统自带的设置工具：安静、可信、不打扰。界面上一律用中性名称和抽象图形，不出现服务品牌、真实服务地址、密钥或模型名称。

能检查的特征：
- 原生桌面外观：macOS 用交通灯按钮和菜单栏入口，Windows 用系统窗口按钮和任务栏通知区域入口；不用网页导航、面包屑或官网式横幅。
- 紧凑的分组表单：窗口默认宽 620px，内容分组放在白色卡片里，卡片内边距 24px。
- 两端都按本规范自绘控件（按钮、输入框、开关、卡片、提示条、弹窗、菜单面板），不使用系统默认外观；两端固定浅色外观。
- 一个强调色：只有主色 #0f766e 用来表示主操作、开关打开和选中，其余都是中性色。

## 2. 颜色

只有浅色一套。

| 用途 | 名字 | 浅色 | 深色 | 对比度（对哪个背景） |
|---|---|---|---|---|
| 主色（主按钮、链接、开关打开） | primary | #0f766e | 不适用：只有浅色主题 | 5.47:1（对 surface）；5.10:1（对 background） |
| 主色悬停 | primary-hover | #115e59 | 不适用：只有浅色主题 | 7.58:1（on-primary 对它） |
| 主色上的文字 | on-primary | #ffffff | 不适用：只有浅色主题 | 5.47:1（对 primary） |
| 页面背景（窗口内容区） | background | #f5f7fa | 不适用：只有浅色主题 | — |
| 卡片、弹窗、菜单、标题栏背景 | surface | #ffffff | 不适用：只有浅色主题 | — |
| 正文文字 | text | #172b3a | 不适用：只有浅色主题 | 14.55:1（对 surface）；13.56:1（对 background） |
| 次要文字 | text-muted | #536575 | 不适用：只有浅色主题 | 6.02:1（对 surface）；5.61:1（对 background） |
| 禁用文字 | text-disabled | #586878 | 不适用：只有浅色主题 | 4.87:1（对 disabled-bg） |
| 禁用背景、开关关闭底色 | disabled-bg | #e9edf1 | 不适用：只有浅色主题 | — |
| 分隔线（只用于不可交互的分组） | border | #e2e7ec | 不适用：只有浅色主题 | 装饰用，不作为识别控件或状态的唯一线索 |
| 输入框、按钮边框 | border-strong | #7b8998 | 不适用：只有浅色主题 | 3.58:1（对 surface）；3.33:1（对 background） |
| 选中浅底（菜单项悬停、显隐按钮按下、转发总览开着时的背景） | selected-bg | #e7f2f0 | 不适用：只有浅色主题 | 主色文字对它 4.78:1；text 对它 12.72:1；text-muted 对它 5.27:1；border-strong 对它 3.13:1 |
| 成功 | success | #16734b | 不适用：只有浅色主题 | 5.85:1（对 surface）；5.49:1（对 success-bg #f0faf4） |
| 警告 | warning | #8a530e | 不适用：只有浅色主题 | 6.02:1（对 warning-bg #fff9ed）；6.32:1（对 surface） |
| 错误 | danger | #b42318 | 不适用：只有浅色主题 | 6.06:1（对 danger-bg #fff3f1） |
| 信息 | info | #245d9a | 不适用：只有浅色主题 | 6.22:1（对 info-bg #f1f6fc） |
| 聚焦描边 | focus | #0f766e | 不适用：只有浅色主题 | 5.10:1（对 background） |
| 弹窗遮罩 | scrim | #172b3a52 | 不适用：只有浅色主题 | — |

状态浅底：success-bg #f0faf4、warning-bg #fff9ed、danger-bg #fff3f1、info-bg #f1f6fc，要和对应状态色的图标、文字一起出现。

## 3. 字体与字号

- 字体：-apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif
- 等宽字体：ui-monospace, SFMono-Regular, Consolas, monospace，用于版本号、地址和密钥输入框

| 用途 | 字号 | 行高 | 字重 |
|---|---|---|---|
| 页面标题（首次设置窗口的大标题） | 20px | 1.3 | 600 |
| 区块标题（分组卡片标题、弹窗标题） | 16px | 1.3 | 600 |
| 正文（弹窗正文） | 14px | 1.5 | 400 |
| 表单标签、按钮、菜单项、提示框正文、标题栏 | 13px | 1.5 | 标签和按钮 600，其余 400 |
| 辅助文字（帮助文字、状态区、字段错误） | 12px | 1.5 | 400 |
| 代码（地址、密钥、版本号） | 12px | 1.5 | 400 |

## 4. 间距

- 尺度：0、4、8、12、16、20、24、32、40、44、48 px，界面里的间距只能从这组值里取
- 页面外边距：窗口内容区四周 24px
- 卡片内边距：24px
- 表单项之间：20px；标签列宽 112px，标签和控件之间 12px
- 按钮内边距：左右 20px；提示框里的按钮左右 16px
- 内容分组之间：20px
- 帮助文字、字段错误和控件之间：8px

## 5. 圆角、边框、阴影

| 项 | 值 |
|---|---|
| 小圆角（标签、小标记） | 4px |
| 中圆角（按钮、输入框、提示框、菜单项） | 8px |
| 大圆角（分组卡片、弹窗、菜单、macOS 窗口外框） | 12px |
| Windows 窗口外框 | 8px |
| 全圆角（开关、开关圆点、状态圆点） | 9999px |
| 边框宽度 | 1px |
| 卡片阴影 | 无 |
| 弹窗、菜单阴影 | 0 16px 48px #172b3a29 |
| 窗口阴影 | 0 22px 70px #172b3a38, 0 2px 8px #172b3a22 |

## 6. 布局与断点

- 断点：不适用：桌面窗口宽度固定为 620px，不随屏幕变窄
- 页面最大宽度：窗口默认宽度 620px，内容不够时不撑满屏幕
- 栅格：不适用：只有单栏表单，表单行用两列（标签 112px + 控件）

页面框架：

| 部分 | 位置和尺寸 | 写法（HTML 结构和类名） | 不同断点下怎么变 |
|---|---|---|---|
| 顶栏（窗口标题栏） | 窗口顶部；macOS 高 48px、标题居中、左侧交通灯；Windows 高 32px、标题靠左、右侧三个系统按钮 | `<div class="titlebar mac">`、`<div class="titlebar win">` | 不变 |
| 侧边栏 | 不适用：桌面工具不设侧栏 | — | — |
| 导航 | 不适用：没有页面导航，其他入口放在菜单栏或托盘菜单里 | — | — |
| 页面标题区 | 只在首次设置窗口出现，位于内容区顶部 | `<div class="intro">` | 不变 |
| 内容区 | 标题栏下方，四周 24px，分组之间 20px；主窗口从上到下是「转发总览」、「中转配置」卡片 | `<div class="win-body">` 内放 `<section class="hero">` 和 `<section class="group">` | 不变 |
| 底部状态区 | 窗口底部，最小高度 48px，左右 24px，12px 字；从左到右：版本、运行状态圆点和文字、ChatGPT 检测结果、中转模型数，最右是「检查更新」；各项之间 16px。后两项用「标签 + 取值」写法，前面带 16px 的图标（正常是 success 色对勾，没有时是 text-muted 色横线）。「ChatGPT」一项的取值：找到并且能用时写「已检测到」（对勾）；没找到写「未找到」（横线）；找到了但读不到它自带的官方模型清单写「版本暂不支持」（横线） | `<div class="statusbar">` | 不变 |

页面框架写法：

```html
<div class="window mac">            <!-- Windows 用 class="window win" -->
  <div class="titlebar mac">
    <span class="lights"><i class="l-close"></i><i class="l-min"></i><i class="l-max"></i></span>
    <span class="tb-title">模型接入助手</span>
  </div>
  <div class="win-body">
    <section class="group">
      <h3>分组标题</h3>
      <div class="field">
        <label for="x">标签</label>
        <div class="ctl"><!-- 控件 --><div class="help">帮助文字</div></div>
      </div>
    </section>
  </div>
  <div class="statusbar">
    <span class="mono">v1.2.0</span><span class="dot"></span><span>后台运行中</span>
    <span class="st-item"><svg class="si">…</svg><span class="lv"><span class="lv-label">ChatGPT</span><strong class="lv-value">已检测到</strong></span></span>
    <span class="st-item"><svg class="si">…</svg><span class="lv"><span class="lv-label">中转模型</span><strong class="lv-value">12 个</strong><span class="lv-note">（3 个配置）</span></span></span>
    <span class="grow"></span><button class="btn-link">检查更新</button>
  </div>
</div>
```

```html
<div class="titlebar win">
  <span class="tb-title"><svg class="ico">…</svg>模型接入助手</span>
  <span class="win-ctl"><i>—</i><i>☐</i><i class="x">✕</i></span>
</div>
```

```html
<div class="intro"><h2>连接中转服务</h2><p class="muted">说明文字</p></div>
```

## 7. 组件

### 按钮

- 写法：主按钮 `<button class="btn-primary">完成设置</button>`；次按钮 `<button class="btn-secondary">保存修改</button>`；文字按钮 `<button class="btn-link">检查更新</button>`；图标按钮 `<button class="btn-icon" aria-label="显示 API Key" aria-pressed="false"><svg class="si">…</svg></button>`
- 高度：44px（图标按钮 44×44px；文字按钮最小高度 44px）
- 内边距：左右 20px；字号 13px，字重 600；圆角 8px

| 状态 | 主按钮 | 次按钮 |
|---|---|---|
| 默认 | 背景 primary，文字 on-primary，边框 1px primary | 背景 surface，文字 text，边框 1px border-strong |
| 悬停 | 背景和边框 primary-hover | 背景 background |
| 按下 | 背景和边框 primary-hover，内容下移 1px | 背景 disabled-bg |
| 禁用 | 背景和边框 disabled-bg，文字 text-disabled，鼠标显示禁止；处理中时左侧加 16px 转圈，文字写正在做什么 | 同主按钮禁用 |
| 聚焦 | 2px focus 描边，向外偏移 3px | 同主按钮 |

图标按钮被按下（aria-pressed="true"）时：背景 selected-bg，图标 primary，边框 primary。

### 输入框

- 写法：`<label for="url">Base URL <span class="req">必填</span></label><div class="ctl"><input id="url" class="input mono"><div class="help">帮助文字</div></div>`；出错时 `<input class="input" aria-invalid="true" aria-describedby="url-err"><div class="field-error" id="url-err"><svg class="si">…</svg>错误说明</div>`；密钥输入用 `<div class="key-wrap">` 把输入框和显隐图标按钮包在一起，间距 8px，默认 type="password"
- 尺寸：高 44px，左右内边距 12px，圆角 8px

| 状态 | 样子 |
|---|---|
| 默认 | 背景 surface，文字 text，边框 1px border-strong |
| 悬停 | 边框 text-muted |
| 按下 | 同聚焦 |
| 禁用 | 背景 disabled-bg，文字 text-disabled，边框仍是 1px border-strong |
| 聚焦 | 2px focus 描边，向外偏移 3px |

出错时边框改成 danger，错误文字替换字段下方原来的帮助文字（同一位置、同样 12px，改成 danger 色并加图标），不额外加行；需要显示错误的字段都要有一行常驻帮助文字占住这个位置。帮助文字和字段错误都只占一行，放不下时末尾用省略号截断，鼠标悬停显示全文；文案要写得在一行内放得下。

### 开关

- 写法：`<div class="switch-row"><button class="switch" role="switch" aria-checked="true" aria-label="使用中转"><span class="knob"></span></button><span class="switch-text"><strong>已开启</strong></span></div>`
- 尺寸：40×24px，圆点 16×16px，整行最小高度 44px；旁边一律写「已开启 / 已关闭」，不只靠颜色区分

| 状态 | 样子 |
|---|---|
| 默认 | 打开：背景和边框 primary，圆点 on-primary 靠右；关闭：背景 disabled-bg，边框 1px border-strong，圆点 border-strong 靠左 |
| 悬停 | 鼠标变成手形 |
| 按下 | 圆点 200ms 滑到另一侧 |
| 禁用 | 边框和圆点 border，鼠标显示禁止，下方帮助文字写原因 |
| 聚焦 | 2px focus 描边，向外偏移 3px |

### 表格

不适用：界面上没有多列的表格数据。多个中转配置用「配置列表」；只读的一项信息用「标签 + 取值」。

### 标签 + 取值

- 写法：`<span class="lv"><span class="lv-label">接入方式</span><strong class="lv-value">官方 + 中转</strong><span class="lv-note">· 已检测到 ChatGPT 登录</span></span>`
- 用在只读的一项信息上（转发总览里的「接入方式」一行、底部状态区的每一项）；要填写的表单标签仍按「输入框」一节
- 标签：text-muted 色，字重 400；取值：text 色，字重 600；取值后面的补充说明：text-muted 色，字重 400；三者字号相同，跟所在的行（转发总览里 13px，底部状态区 12px）
- 标签和取值之间 8px；取值和补充说明之间 4px
- 不可点击，没有悬停、按下、禁用、聚焦状态

### 转发总览

- 写法：

```html
<section class="hero on">
  <div class="hero-top">
    <svg class="app-icon" width="48" height="48">…</svg>
    <div class="hero-text"><div class="hero-title">使用中转 · 已开启</div><div class="hero-sub">一句说明</div></div>
    <button class="switch" role="switch" aria-checked="true" aria-label="使用中转"><span class="knob"></span></button>
  </div>
  <div class="hero-mode"><span class="lv"><span class="lv-label">接入方式</span><strong class="lv-value">官方 + 中转</strong><span class="lv-note">· 已检测到 ChatGPT 登录</span></span><span class="grow"></span><button class="btn-secondary sm">重新检测</button></div>
</section>
```

- 主窗口内容区的第一块，放转发开关和接入方式
- 外框：圆角 12px，边框 1px，内边距上下 16px、左右 24px，上下两行间距 12px，无阴影。转发开着（`on`）时背景 selected-bg、边框 primary；关着时背景 surface、边框 border
- 第一行：左边 48px 的应用图标，16px 之后是文字，最右边是开关。标题 16px、字重 600、text 色，写「使用中转 · 已开启」或「使用中转 · 已关闭」；标题下方 4px 是一句 12px、text-muted 色的说明
- 说明文字：开着时写当前接入方式的一句话（「官方模型在上，中转模型在下；选官方模型时仍访问官方服务。」或「不用登录 ChatGPT。只有中转模型，默认选中「<配置名> · auto」。」，都没有 auto 时后半句写「没有默认选中的模型」）；关着时写「已关闭：Codex 配置已还原为开启前的样子。」；找不到 ChatGPT 时写「找到 ChatGPT 后才能打开。」；读不到 ChatGPT 自带的官方模型清单时写「当前 ChatGPT 版本暂不支持，暂时不能打开。」
- 第二行（只在转发开着时有）：最小高度 44px，左边是「标签 + 取值」写法的接入方式，右边是「重新检测」次按钮（高 44px，左右 16px）
- 开关按「开关」一节；这里标题已经写了「已开启 / 已关闭」，开关旁边不再重复

| 状态 | 样子 |
|---|---|
| 默认 | 如上，开着和关着两种样子 |
| 悬停 | 外框不变（不可点击）；开关和按钮按各自一节 |
| 按下 | 不适用：外框不可点击；开关和按钮按各自一节 |
| 禁用 | 找不到 ChatGPT，或读不到 ChatGPT 自带的官方模型清单时，转发保持关闭、开关禁用，说明文字写原因，窗口上方另有一条带「重新检测」的错误提示条；正在检测时只有「重新检测」禁用，显示转圈和「正在检测…」，开关照常可用 |
| 聚焦 | 外框不可聚焦；Tab 先到开关，再到「重新检测」，聚焦的控件显示 2px focus 描边，向外偏移 3px |

### 配置列表

- 写法：

```html
<ul class="cfg-list" aria-label="中转配置">
  <li class="cfg-row">
    <div class="cfg-main">
      <div class="cfg-name">中转</div>
      <div class="cfg-meta"><span class="cfg-status ok"><svg class="si">…</svg>6 个模型</span><span class="cfg-url mono">https://relay.example.com/v1</span></div>
    </div>
    <div class="cfg-actions"><button class="btn-secondary sm">编辑</button><button class="btn-secondary sm">删除</button></div>
  </li>
</ul>
<div class="cfg-foot"><button class="btn-secondary">添加配置</button><span class="help">已有 3 个，最多 10 个。</span></div>
```

- 放在分组卡片里，卡片标题下面是列表，列表下面 20px 是底部一行（`cfg-foot`：左边「添加配置」按钮，右边 12px 处一行 12px 的 text-muted 说明文字）
- 一行：高 64px，上下内边距 8px，内容上下居中；行与行之间 1px border 分隔线（第一行上面没有）；左边文字区和右边按钮区间距 12px
- 文字区第一行是名字：13px，字重 600，text 色，不折行
- 文字区第二行：12px，先是状态，8px 之后是地址。状态不折行、不截断；地址用等宽字体、text-muted 色，放不下时末尾用省略号截断
- 状态的五种样子：正常是 success 色对勾图标加「6 个模型」；这个配置拉取模型列表失败是 warning 色警告图标加「没更新，沿用上次的 4 个」；从没拉成功过是 warning 色警告图标加「还没获取到模型」；正在拉取是 16px 转圈加 text-muted 色的「正在获取…」；转发关着时是 text-muted 色的「转发关闭时不获取」，前面没有图标。图标和后面的文字颜色相同
- 按钮区：每个按钮按「按钮」一节的次按钮画，高 44px、左右内边距 16px，按钮之间 8px；顺序是「重试」（只在状态是 warning 时出现）、「编辑」、「删除」
- 列表最多显示 3 行（192px），更多时列表自己上下滚动，窗口高度不变；滚动条按平台原样
- 状态和「重试」都画在这一行里面，行高和窗口高度不因此改变；某个配置拉取模型列表失败只在它自己那一行显示

| 状态 | 样子 |
|---|---|
| 默认 | 如上 |
| 悬停 | 行本身不变（行不可点击）；行里的按钮按「按钮」一节 |
| 按下 | 不适用：行不可点击；行里的按钮按「按钮」一节 |
| 禁用 | 只剩一个配置时「删除」禁用，底部说明文字写「至少保留一个配置。」；已有 10 个时「添加配置」禁用，底部说明文字写「已有 10 个，达到上限。」；某一行正在拉取或正在删除时，这一行的按钮都禁用 |
| 聚焦 | 行本身不可聚焦；Tab 依次经过每一行的「重试」「编辑」「删除」，最后到「添加配置」，聚焦的按钮显示 2px focus 描边，向外偏移 3px |

### 卡片

- 写法：`<section class="group"><h3>转发</h3>…</section>`
- 背景 surface，边框 1px border，圆角 12px，内边距 24px，卡片内各项间距 20px，无阴影

| 状态 | 样子 |
|---|---|
| 默认 | 如上 |
| 悬停 | 不变（卡片不可点击） |
| 按下 | 不适用：卡片不可点击 |
| 禁用 | 不适用：禁用体现在卡片内的控件上 |
| 聚焦 | 不适用：卡片不可聚焦 |

### 弹窗

- 写法：

```html
<div class="scrim">
  <div class="modal" role="alertdialog" aria-modal="true" aria-labelledby="mt">
    <div class="modal-header"><svg class="ico">…</svg><h3 id="mt">标题</h3></div>
    <div class="modal-body">说明会发生什么、会影响什么</div>
    <div class="modal-footer"><button class="btn-secondary">取消</button><button class="btn-primary">确认操作</button></div>
  </div>
</div>
```

- 宽 520px，最大为视口宽度减 32px；背景 surface，圆角 12px，阴影 0 16px 48px #172b3a29；遮罩 scrim；头部内边距 24px 24px 0，正文 12px 24px 24px，底部 16px 24px，底部上方 1px border 分隔线；按钮靠右，间距 12px，确认按钮在最右并写明具体动作（如「还原并退出」）

| 状态 | 样子（关闭按钮、操作按钮） |
|---|---|
| 默认 | 操作按钮按「按钮」一节的默认样子 |
| 悬停 | 按「按钮」一节 |
| 按下 | 按「按钮」一节 |
| 禁用 | 处理中时确认按钮禁用并显示转圈 |
| 聚焦 | 打开后焦点落在「取消」上，Tab 只在弹窗内循环，Escape 等同于取消，关闭后焦点回到触发它的控件 |

表单弹窗（弹窗里放要填写的字段，如添加、编辑中转配置）：

```html
<div class="modal" role="dialog" aria-modal="true" aria-labelledby="ft">
  <div class="modal-header"><svg class="ico">…</svg><h3 id="ft">添加配置</h3></div>
  <div class="modal-body form">
    <div class="field"><label for="n">名字 <span class="req">必填</span></label><div class="ctl"><input id="n" class="input"><div class="help">帮助文字</div></div></div>
  </div>
  <div class="modal-footer"><button class="btn-secondary">取消</button><button class="btn-primary">保存</button></div>
</div>
```

- 尺寸、圆角、阴影、头部和底部同上；正文里的字段按「输入框」一节画，标签列 112px，字段之间 20px
- 打开后焦点落在第一个输入框上；编辑时输入框里已填好现在的值
- 点确认按钮时一次检查所有字段，每个出错的字段按「输入框」一节在自己下方显示错误，焦点移到第一个出错的输入框；弹窗不关闭，已填的内容都留着
- 不属于某一个字段的错误（如连不上中转、写不进系统凭据库）用错误提示条，浮在弹窗上方：距弹窗上沿 12px、左右各 24px，不改变弹窗高度
- 处理中时：输入框和「取消」禁用，确认按钮禁用并显示转圈，文字写正在做什么
- 成功后弹窗关闭，结果用成功提示条显示在主窗口上方

### 提示条

- 写法：`<div class="alert warning" role="status"><svg class="si">…</svg><div><div class="alert-title">标题</div><div class="alert-text">说明</div><div class="alert-actions"><button class="btn-secondary sm">重试</button></div></div></div>`；错误用 `class="alert danger" role="alert"`，另有 `info`、`success`
- 浮在窗口内容区上方，不占布局、不挤开下面的内容，窗口高度不因提示改变：距标题栏下沿 12px、左右各 24px，多条时纵向排列、间距 8px，阴影用「弹窗、菜单阴影」；内边距 16px，圆角 8px，边框 1px 状态色，背景对应的状态浅底（不透明）；标题用状态色 600 字重，说明用 text；按钮高 44px、左右 16px；右侧 44×44px 的 ✕ 关闭按钮（所有提示条都有，用负外边距抵消内边距，不撑高提示条），图标色 text-muted；由状态决定的提示条（如"没有找到 ChatGPT"）被关掉后，状态再次变化时重新出现；由状态决定的提示条不论颜色都不自动消失，4 秒自动消失只用于操作完成后弹出的一次性提示；同一条提示不重复叠加，问题解决后（例如下一次同类操作成功）自动移除
- 成功（success）和信息（info）提示 4 秒后自动消失；警告（warning）和错误（danger）不自动消失，直到用户点 ✕ 或问题解决；需要用户处理的要给出操作
- 长说明（如文件路径、被手改的设置项）不放进提示条，提示条只给「查看详情」，详情用弹窗

| 状态 | 样子（关闭按钮、操作链接） |
|---|---|
| 默认 | 操作按 btn-secondary 或 btn-link |
| 悬停 | 按「按钮」一节 |
| 按下 | 按「按钮」一节 |
| 禁用 | 处理中时按钮禁用并显示转圈 |
| 聚焦 | 2px focus 描边，向外偏移 3px |

### 菜单（菜单栏菜单 / 托盘菜单）

- 写法：

```html
<div class="menu" role="menu" aria-label="模型接入助手">
  <div class="menu-head"><svg class="app-icon" width="16" height="16">…</svg><div><strong>模型接入助手</strong><div class="mi-sub">转发已开启 · 后台运行中</div></div></div>
  <div class="menu-sep"></div>
  <div class="menu-item" role="menuitemcheckbox" aria-checked="true"><span class="mi-check"><svg class="si">…</svg></span><span class="mi-label">使用中转</span></div>
  <div class="menu-item" role="menuitem"><span class="mi-check"></span><span class="mi-label">打开配置…</span><span class="kbd">⌘,</span></div>
</div>
```

- 宽 286px，内边距 4px，背景 surface，边框 1px border，圆角 12px，阴影 0 16px 48px #172b3a29；在平台入口旁边弹出（macOS 菜单栏图标下方，Windows 托盘图标上方）；分组之间用 1px border 分隔线（左右缩进 8px）
- 头部（两端相同）：内边距 12px；左边是 16px 的应用图标，8px 之后是两行文字：标题 13px、字重 600、text 色，状态 12px、text-muted 色
- 菜单项：最小高度 44px，左 8px、右 12px，圆角 8px，字号 13px；左侧 16px 的位置放勾选图标（勾选菜单项打勾，普通项留空对齐）
- 分组顺序：标题与状态 / 转发、打开配置 / 登录时启动、检查更新 / 卸载、退出

| 状态 | 样子 |
|---|---|
| 默认 | 背景 surface，文字 text |
| 悬停 | 背景 selected-bg |
| 按下 | 背景 selected-bg，执行后菜单关闭 |
| 禁用 | 文字 text-disabled，鼠标显示禁止 |
| 聚焦 | 方向键移动焦点，2px focus 描边；Escape 关闭菜单 |

### 加载指示

- 写法：`<span class="spinner" aria-hidden="true"></span>`，旁边一定有说明正在做什么的文字
- 16×16px，2px 边框，右侧透明，800ms 转一圈；处理期间禁止重复提交，不显示假的百分比

### 应用图标与菜单栏 / 托盘图标

- 图形：圆圈里一左一右两个箭头，和官网顶栏的图形是同一个。画在 100×100 的画布上，线端和拐角都是圆头，不填充：

```html
<svg viewBox="0 0 100 100" fill="none" stroke="currentColor" stroke-width="6" stroke-linecap="round" stroke-linejoin="round">
  <circle cx="50" cy="50" r="34"/>
  <path d="M31.5 42.6H68.5l-9.6-9.6M68.5 57.4H31.5l9.6 9.6"/>
</svg>
```

- 线条粗细按显示尺寸分三档：

| 显示尺寸 | 线宽（占画布边长） | 图形 |
|---|---|---|
| 64px 及以上 | 6% | 外圈加两个箭头 |
| 24px 到 48px | 9% | 外圈加两个箭头 |
| 16px | 12% | 只留两个箭头，去掉外圈：`<path d="M22 38H78l-13-13M78 62H22l13 13"/>` |

- 应用图标：底板 primary #0f766e，图形 on-primary #ffffff（图形对底板 5.47:1）
  - macOS：1024×1024px 画布上，底板 824×824px 居中（四周各留 100px），圆角 184px，其余尺寸按比例缩小；32px 的四周留 2px，16px 的四周留 1px；图形缩到底板大小，放在底板正中
  - macOS 64px 及以上的底板带投影：向下偏移尺寸的 1/64，高斯模糊的标准差为尺寸的 1/32（即 CSS drop-shadow 的第三个值），颜色 #172b3a、不透明度 22%（0x38）；32px 及以下不带投影。Windows 图标和单色图标不带投影
  - Windows：底板铺满画布，256×256px 画布上圆角 48px，其余尺寸按比例缩小；图形缩到画布的 88%，放在正中
  - 提供的尺寸：macOS 16、32、64、128、256、512、1024px；Windows 16、24、32、48、256px
- 窗口里用应用图标的地方：主窗口「转发总览」左边 48px（线宽 9%）；弹窗头部标题左边 24px（线宽 9%）；Windows 标题栏标题左边 16px（线宽 12%，只留两个箭头）；两端菜单面板头部标题左边 16px（线宽 12%，只留两个箭头）。底板圆角按所在平台的比例
- 菜单栏 / 托盘单色图标：同一个图形，没有底板；18px 及以上线宽 9%、图形放大到 1.2 倍放在画布正中；16px 按上表 16px 规则（线宽 12%、只留两个箭头），不放大
  - macOS 菜单栏：18×18px，只画形状不带颜色，颜色由系统按菜单栏深浅自动换成深色或白色
  - Windows 托盘：16×16px，浅色任务栏用 text #172b3a（对 #ffffff 14.55:1），深色任务栏用 #ffffff

| 状态 | 样子 |
|---|---|
| 默认 | 上面写的样子 |
| 悬停 | 没有这个状态 |
| 按下 | 没有这个状态；菜单栏和托盘图标按下时的高亮底由系统画 |
| 禁用 | 没有这个状态 |
| 聚焦 | 没有这个状态 |

## 8. 动效

| 项 | 时长 | 缓动 |
|---|---|---|
| 悬停、按下反馈 | 200ms | ease-out |
| 展开、收起（菜单、开关圆点滑动） | 200ms | ease-out |
| 弹窗出现、消失 | 200ms | ease-out |
| 转圈指示 | 800ms 一圈 | linear |

- 操作开始 200ms 后仍未完成才显示加载状态；成功和信息提示条 4 秒后自动消失，警告和错误提示条不自动消失
- 用户系统设置了"减少动态效果"时：停止转圈和开关滑动动画，直接切换到结果，保留状态文字

## 9. 无障碍

- 聚焦：所有可操作元素聚焦时显示 focus 色描边，宽度 2px，向外偏移 3px
- 点击区域：至少 44×44 px（系统自带的标题栏按钮按平台原样）
- 颜色不是唯一的信息来源：成功、警告、错误同时有图标和文字；开关旁写「已开启 / 已关闭」；勾选菜单项用对勾图标
- 表单：每个输入框都有看得见的标签；帮助文字和错误信息用 aria-describedby 关联，出错的输入框加 aria-invalid
- 动态状态：错误用 role="alert"，不打断的反馈用 role="status"
- 密钥：API Key 默认遮蔽，显隐按钮有可访问名称和按下状态；只在需要的地方显示密钥内容

行为边界（界面上不能暗示这些行为）：
- 不提供模型设置、代理端口、自动安装更新的入口
- 「登录时启动」默认开启，用户可以在菜单里关闭；关闭时如果转发还开着，要先提醒"工具没在运行时 Codex 不可用"
- 改动 Codex 配置后需要重启 ChatGPT 才生效时，只用提示条告诉用户"重启 ChatGPT 后生效"，本工具不重启 ChatGPT
- 关闭主窗口只隐藏窗口，入口仍在菜单栏或通知区域；退出程序才会移除入口并停止检查更新
- 检查更新只针对本工具，启动时查一次，之后每 24 小时一次；只提示新版并打开下载页，不暗示更新官方客户端

例外：用户不接受某条不达标的修改建议时记在这里，以后检查时不再报。

| 项 | 实际值 | 用户确认日期 |
|---|---|---|
| 菜单项聚焦描边向外偏移 3px | 描边画在菜单项内侧（偏移 0）：菜单面板内边距只有 4px，向外画会被裁掉 | 2026-11-11 |
| 弹窗背后的半透明遮罩 | 不画遮罩：弹窗是独立系统窗口，无法覆盖整个桌面 | 2026-11-11 |
| 中转配置的添加、编辑、删除弹窗居中并带遮罩 | macOS 用系统 sheet：从主窗口顶部滑出，主窗口在弹窗关闭前不能操作，不画遮罩 | 2026-11-17 |
| macOS 主窗口左上角三个窗口按钮与 48px 顶栏标题垂直居中 | macOS 13～15 系统标题栏只有 28px，按钮最多比居中位置偏上 3px，以保证按钮能点到 | 2026-11-17 |
| 禁用控件显示「禁止」光标 | 光标保持默认箭头 | 2026-11-11 |
| 菜单面板外框圆角 12px 与阴影 | macOS 由系统菜单窗口绘制外框圆角和阴影 | 2026-11-11 |

## 10. 终端界面

不适用：本项目只有图形界面，没有命令行输出。

## 11. 官网

第 1 到 10 节管 App 的界面；这一节管项目的 GitHub Pages 介绍页（一个单页网站，中文和英文各一份内容，右上角切换语言）。官网里画的 App 界面图按第 1 到 10 节的 App 规范画，界面语言保持中文。

### 风格定位

给第一次听说本工具的人看的介绍页，要让人觉得它和 App 是同一个产品：安静、清楚、可信。

能检查的特征：
- 白底，区块之间用白色和浅灰交替；只有主色 #0f766e 一个强调色。
- 内容居中，最宽 1088px；标题居中。
- 官网自己的元素都没有阴影；只有 App 界面图带 App 的窗口阴影。
- 全页不放真实服务地址、密钥、服务品牌；示例只用 `relay.example.com`、`sk-example-0000`。不放星标数、用户数、用户评价。
- 不引用外部字体和外部脚本。

### 颜色

只有浅色一套，全部取自第 2 节已有的颜色。

| 用途 | 色值 | 对比度（对哪个背景） |
|---|---|---|
| 主色（主按钮、链接、选中的页签、步骤编号） | #0f766e | 5.47:1（对页面背景）；5.10:1（对交替区块背景）；4.78:1（对主色浅底） |
| 主色悬停（主按钮悬停和按下、文字链接悬停） | #115e59 | 白字对它 7.58:1；它对页面背景 7.58:1 |
| 主色上的文字 | #ffffff | 5.47:1（对主色） |
| 主色浅底（首屏标签、步骤编号圆、图标底、语言切换的选中项） | #e7f2f0 | — |
| 页面背景（页面、卡片、顶栏） | #ffffff | — |
| 交替区块背景（隔一个区块用一次、导航链接悬停） | #f5f7fa | — |
| 按下浅底（次按钮、导航链接按下）、禁用背景（禁用按钮的背景和边框） | #e9edf1 | 正文文字对它 12.37:1；禁用文字对它 4.87:1 |
| 禁用文字（禁用按钮上的文字） | #586878 | 4.87:1（对禁用背景） |
| 正文文字（也用于文字链接按下） | #172b3a | 14.55:1（对页面背景）；13.56:1（对交替区块背景） |
| 次要文字（说明文字、导航链接、页脚） | #536575 | 6.02:1（对页面背景）；5.61:1（对交替区块背景） |
| 分隔线（卡片边框、顶栏下边线，只作装饰） | #e2e7ec | 装饰用 |
| 控件边框（次按钮、语言切换） | #7b8998 | 3.58:1（对页面背景）；3.33:1（对交替区块背景） |
| 聚焦描边 | #0f766e | 5.47:1（对页面背景） |

首屏背景：从顶部中间向外的椭圆渐变，720px × 320px，从主色浅底 #e7f2f0 过渡到透明。

### 字体与字号

- 字体：-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif
- 等宽字体：ui-monospace, SFMono-Regular, Consolas, monospace，用于代码片段

| 用途 | 字号 | 行高 | 字重 |
|---|---|---|---|
| 首屏大标题 | 48px（窗口宽 900px 及以下时 36px），字间距 -0.5px | 1.2 | 700 |
| 区块标题 | 32px | 1.3 | 700 |
| 下载卡片标题 | 24px | 1.4 | 600 |
| 首屏说明、卡片标题、常见问题的问题 | 18px | 首屏说明 1.6，标题 1.4 | 首屏说明 400，标题 600 |
| 正文、按钮 | 16px | 1.6 | 正文 400，按钮 600 |
| 导航、标签、提示、代码片段 | 14px | 1.6（代码片段 1.5） | 导航 400，标签 600 |
| 小字（模型列表的分组名、页脚声明） | 12px | 1.6 | 400（分组名 600） |

### 间距

- 尺度：0、4、8、12、16、20、24、32、48、64、96 px
- 页面左右留白：24px；内容最宽 1088px（含左右留白）
- 区块上下留白：96px（窗口宽 900px 及以下时 64px）；首屏上 96px、下 64px
- 区块标题和内容之间：48px
- 卡片之间：24px；卡片内边距：32px（安全卡片 24px）
- 按钮左右内边距：24px；按钮之间 12px

### 圆角、边框、阴影

| 项 | 值 |
|---|---|
| 小圆角（代码片段、接入方式上的小标签、模型列表的行） | 4px |
| 中圆角（按钮、页签、语言切换、导航链接） | 8px |
| 大圆角（卡片、页签外框、图标底、流程节点） | 12px |
| 全圆角（首屏标签、步骤编号圆） | 9999px |
| 边框宽度 | 1px；流程图里代表本工具的节点 2px 主色；模型列表里中转模型的行左侧 4px 主色条 |
| 阴影 | 无 |

### 布局与断点

- 断点：900px。窗口宽 900px 及以下时：导航链接隐藏（保留品牌、语言切换、GitHub 链接），多列的卡片改成一列，首屏大标题 36px，区块上下留白 64px
- App 界面图固定按 620px 宽画：窗口宽 760px 及以下时整体缩到 80%，600px 及以下时缩到 52%，356px 及以下时缩到 46%
- 窗口宽 600px 及以下：品牌副名「模型接入助手」隐藏；页面左右留白 16px；首屏上 64px、下 48px；首屏大标题 32px；流程图节点不设最小宽；页签左右内边距 12px；顶栏元素间距 12px（右侧一组 8px）；下载区「首次打开要手动放行」卡片内边距 24px，系统和步骤两列改成单列
- 窗口宽 400px 及以下：语言切换按钮左右内边距 4px；品牌名 14px；顶栏元素间距 8px（右侧一组 4px）；GitHub 链接内边距 0
- 窗口宽 900px 及以下：流程图整体改成上下排，箭头转 90° 指向下方；分叉的两个节点仍上下排
- 页面最大宽度：1088px；常见问题最宽 760px；首屏大标题最宽 840px
- 栅格：步骤 3 列、接入方式 2 列、安全 2 列、下载 2 列，列间距 24px

区块顺序：顶栏 → 首屏（一句话介绍）→ 界面 → 三步上手 → 两种接入方式 → 安全说明 → 常见问题 → 下载 → 页脚。「界面」「接入方式」「常见问题」三个区块用交替区块背景。

| 部分 | 位置和尺寸 | 写法（HTML 结构和类名） | 900px 及以下怎么变 |
|---|---|---|---|
| 顶栏 | 吸在页面顶部，高 64px，白底，下边 1px 分隔线 | `<header class="nav"><div class="wrap nav-in">` 里依次放 `<a class="brand">`、`<nav class="links">`、`<div class="nav-r">` | 隐藏 `.links` |
| 侧边栏 | 不适用：单页网站不设侧栏 | — | — |
| 导航 | 顶栏里靠右的 6 个页内链接，点了滚到对应区块 | `<nav class="links"><a href="#steps">上手</a>…</nav>` | 隐藏 |
| 首屏 | 顶栏下方，文字居中 | `<section class="hero">` 里放 `<p class="eyebrow">`、`<h1>`、`<p class="lead">`、`<div class="ctas">`、`<p class="need">` | 大标题 36px |
| 区块 | 每个区块一个 section，标题居中 | `<section class="sec" id="steps"><div class="wrap"><div class="sec-h"><h2>标题</h2><p>说明</p></div>…</div></section>`；交替背景加 `class="sec alt"` | 上下留白 64px |
| 页脚 | 页面底部，上边 1px 分隔线，上下 32px | `<footer class="foot"><div class="wrap foot-in">` | 两组内容换行 |

### 组件

官网没有输入框、表格、弹窗、提示条。

#### 按钮

- 写法：主按钮 `<a class="btn btn-1" href="…">下载</a>`；次按钮 `<a class="btn btn-2" href="…">查看源码</a>`；按钮里可以放一个 20px 的图标，图标和文字间距 8px
- 禁用的主按钮没有可去的地址，写成 `<button class="btn btn-1" type="button" disabled>暂未发布</button>`，里面不放图标，文字写明为什么点不了
- 高 48px，左右内边距 24px，字号 16px，字重 600，圆角 8px
- 首屏按钮组 `<div class="ctas">`：从左到右是「下载 macOS 版 v<版本号>」「下载 Windows 版 v<版本号>」两个主按钮和「查看源码」次按钮，整组居中，按钮之间 12px；一行放不下时换行，行与行之间 12px。还没发过正式版时，两个下载按钮换成一个禁用的主按钮「暂未发布」

| 状态 | 主按钮 | 次按钮 |
|---|---|---|
| 默认 | 背景和边框 #0f766e，文字 #ffffff | 背景 #ffffff，文字 #172b3a，边框 1px #7b8998 |
| 悬停 | 背景和边框 #115e59 | 背景 #f5f7fa |
| 按下 | 背景和边框 #115e59，整体下移 1px | 背景 #e9edf1 |
| 禁用 | 背景和边框 #e9edf1，文字 #586878，鼠标显示禁止；悬停和按下时样子不变；Tab 键跳过它 | 不适用：次按钮都是链接 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px | 同主按钮 |

#### 导航链接

- 写法：`<nav class="links"><a href="#steps">上手</a></nav>`
- 高 44px，左右内边距 12px，字号 14px，圆角 8px

| 状态 | 样子 |
|---|---|
| 默认 | 文字 #536575，无背景 |
| 悬停 | 文字 #172b3a，背景 #f5f7fa |
| 按下 | 文字 #172b3a，背景 #e9edf1 |
| 禁用 | 不适用 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px |

#### 文字链接

- 写法：顶栏的 GitHub 链接 `<a class="gh" href="…">GitHub</a>`；页脚链接 `<div class="foot-r"><a href="…">GitHub 仓库</a><a href="#top">回到顶部</a></div>`；下载区的「查看所有版本」链接 `<p class="more"><a href="…">查看所有版本</a></p>`
- GitHub 链接：高 44px（行高 44px），左右内边距 4px（窗口宽 400px 及以下时 0），字号 14px，字重 600，没有下划线
- 页脚链接：高 44px（行高 44px），字号 14px，字重 400，带下划线；两个链接之间 24px
- 「查看所有版本」链接：高 44px（行高 44px），字号 14px，字重 400，带下划线；在两张下载卡片下方 16px 处，水平居中
- 颜色直接切换，没有过渡动画

| 状态 | 样子 |
|---|---|
| 默认 | 文字 #0f766e（对页面背景 5.47:1） |
| 悬停 | 文字 #115e59（对页面背景 7.58:1） |
| 按下 | 文字 #172b3a（对页面背景 14.55:1） |
| 禁用 | 不适用 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px |

#### 语言切换

- 写法：`<div class="lang" role="group" aria-label="语言 / Language"><button aria-pressed="true" lang="zh-CN">中文</button><button aria-pressed="false" lang="en">English</button></div>`
- 外框 1px #7b8998，圆角 8px；每格高 44px，左右内边距 12px，字号 14px，字重 600

| 状态 | 样子 |
|---|---|
| 默认 | 没选中的一格：背景 #ffffff，文字 #536575 |
| 悬停 | 背景 #f5f7fa，文字 #172b3a |
| 按下（选中） | 背景 #e7f2f0，文字 #0f766e |
| 禁用 | 不适用 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px |

#### 页签

- 写法：`<div class="tabs" role="tablist"><button role="tab" aria-selected="true">主窗口</button>…</div>`，下面每个页签对应一个 `<figure class="shot">`，没选中的加 `hidden`
- 外框：白底，1px #e2e7ec，圆角 12px，内边距 4px，页签之间 4px；每个页签高 44px，左右内边距 20px，字号 14px，字重 600，圆角 8px
- 窄屏：页签文字只排一行（`white-space:nowrap`）；页签条最宽等于内容区（`max-width:100%`）；一行放不下时整个页签换到下一行（`flex-wrap:wrap`），每行居中，行与行之间 4px；窗口宽 600px 及以下时每个页签左右内边距 12px
- 英文页签文字：Setup / Main window / Menu bar

| 状态 | 样子 |
|---|---|
| 默认 | 没选中：无背景，文字 #536575 |
| 悬停 | 文字 #172b3a |
| 按下（选中） | 背景 #0f766e，文字 #ffffff |
| 禁用 | 不适用 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px |

#### 卡片

- 写法：步骤 `<ol class="steps"><li><span class="num">1</span><h3>标题</h3><p>说明</p></li></ol>`；接入方式 `<article class="mode"><span class="when">已登录 ChatGPT 时</span><h3>…</h3><p>…</p><div class="ml">…</div></article>`；安全 `<ul class="safe"><li><svg class="ic">…</svg><div><h3>…</h3><p>…</p></div></li></ul>`；下载 `<article class="get"><h3>macOS</h3><p>…</p><a class="btn btn-1" href="…">下载 macOS 版 v1.2.3</a></article>`，系统要求和按钮之间 24px；还没发过正式版时按钮换成禁用的主按钮
- 背景 #ffffff，边框 1px #e2e7ec，圆角 12px，内边距 32px（安全卡片 24px），无阴影
- 步骤编号圆：40×40px，全圆角，背景 #e7f2f0，文字 #0f766e 字重 700
- 安全卡片图标：48×48px 的底（内边距 12px，圆角 12px，背景 #e7f2f0），图标 #0f766e

| 状态 | 样子 |
|---|---|
| 默认 | 如上 |
| 悬停 | 不变（卡片不可点击） |
| 按下 | 不适用：卡片不可点击 |
| 禁用 | 不适用 |
| 聚焦 | 不适用：卡片不可聚焦 |

#### 常见问题

- 写法：`<div class="faq"><details open><summary>问题</summary><p>回答</p></details>…</div>`，第一个默认展开
- 问题：字号 18px，字重 600，上下内边距 20px；回答：16px，文字 #536575，下内边距 24px；每项下边 1px #e2e7ec

| 状态 | 样子 |
|---|---|
| 默认 | 收起，只显示问题 |
| 悬停 | 问题文字 #0f766e，鼠标变成手形 |
| 按下 | 展开或收起回答 |
| 禁用 | 不适用 |
| 聚焦 | 2px #0f766e 描边，向外偏移 3px |

#### 标签和代码片段

- 首屏标签：`<p class="eyebrow">开源 · macOS / Windows 托盘小工具</p>`，全圆角，背景 #e7f2f0，文字 #0f766e，14px 字重 600，内边距 4px 16px
- 条件标签：`<span class="when">已登录 ChatGPT 时</span>`，圆角 4px，背景 #e7f2f0，文字 #0f766e，14px 字重 600，左右内边距 8px
- 代码片段：`<code>https://relay.example.com/v1</code>`，14px 等宽字，背景 #f5f7fa，边框 1px #e2e7ec，圆角 4px，左右内边距 4px
- 这三种都不可点击，没有悬停、按下、禁用、聚焦状态

#### 流程图

- 写法：`<div class="flow" role="img" aria-label="…"><div class="node"><b>Codex</b><span>ChatGPT 桌面版</span></div><i class="arrow">→</i><div class="node hub">…</div><i class="arrow">→</i><div class="fork"><div class="node">…</div><div class="node">…</div></div></div>`
- 节点：白底，1px #e2e7ec，圆角 12px，内边距 12px 20px，最小宽 168px；名称 16px 字重 700，说明 14px #536575
- 代表本工具的节点（`.hub`）：边框 2px #0f766e，背景 #e7f2f0
- 箭头：24px，颜色 #7b8998；节点和箭头间距 16px；分叉的两个节点上下排，间距 12px
- 不可点击，没有交互状态

#### 模型列表示意

- 写法：`<div class="ml" aria-hidden="true"><div class="ml-t">Codex 模型列表（示意）</div><div class="ml-g">官方模型</div><div class="ml-r">官方模型 A</div><div class="ml-g">中转模型</div><div class="ml-r on">中转 · auto<em>默认</em></div></div>`
- 外框：背景 #f5f7fa，1px #e2e7ec，圆角 8px，内边距 8px；行：白底，圆角 4px，内边距 8px 12px，14px，行间距 4px；中转模型的行左侧加 4px #0f766e 竖条；「默认」字样 12px 字重 600 #0f766e
- 只用中性名称（官方模型 A、中转 · auto），不写真实模型名
- 不可点击，没有交互状态

#### App 界面图

- 写法：`<figure class="shot"><div class="stage"><div class="am" role="img" aria-label="主窗口示意图"><div class="am-window" inert>…</div></div></div><figcaption>说明</figcaption></figure>`
- 图里的内容用 HTML 和 CSS 按 App 规范画，数据全部是示例；整张图不可操作（`inert`），说明文字 16px #536575 居中
- macOS 窗口左上角三个圆点（关闭、最小化、最大化）用系统原色：#ff5f57、#febc2e、#28c840，直径 12px
- 「菜单栏菜单」图的桌面背景：从 #dfe7ee 经 #eef2f6（60% 处）到 #e7f2f0 的 160° 渐变；顶部菜单栏高 24px，背景 #ffffffe0（白色 88% 不透明），下边框 1px #e2e7ec；菜单栏里本工具图标的底色 #172b3a1f（深色 12% 不透明）

### 动效

| 项 | 时长 | 缓动 |
|---|---|---|
| 按钮悬停、按下的背景变化 | 200ms | ease-out |
| 点导航滚到对应区块 | 由浏览器的平滑滚动决定（`scroll-behavior: smooth`） | 浏览器默认 |

- 用户系统设置了"减少动态效果"时：滚动直接跳到目标位置，按钮背景直接切换

### 无障碍

- 聚焦：所有可操作元素聚焦时显示 2px #0f766e 描边，向外偏移 3px
- 禁用的按钮用 `<button disabled>`，Tab 键跳过它，按钮上的文字写明原因（如「暂未发布」）
- 点击区域：按钮高 48px；导航链接、文字链接、语言切换、页签、常见问题的问题高度至少 44px
- 语言：页面根元素按当前语言写 `lang`（`zh-CN` 或 `en`）；语言切换的两个按钮各自带 `lang`，选中的一个 `aria-pressed="true"`
- App 界面图整体是一张图：`role="img"` 加说明文字，Tab 键会跳过里面的内容；英文页的界面图保持中文界面，并注明"App 界面只有中文"
- 模型列表示意只是装饰，`aria-hidden="true"`，它表达的意思在旁边的正文里都写了
- 颜色不是唯一的信息来源：选中的页签和语言同时有 `aria-selected` / `aria-pressed`

文案约定：
- 英文页把「中转」统一叫 relay；中英文首屏都写一句"这个页面把你自己的模型服务叫作「中转」/ relay"
- 页脚保留一句：ChatGPT 和 Codex 是各自所有者的商标，本项目与它们的所有者没有隶属关系
- 首屏和下载区各有一组下载按钮，写「下载 macOS 版 v<版本号>」「下载 Windows 版 v<版本号>」（英文页 Download for macOS v<版本号>、Download for Windows v<版本号>），版本号是最新的正式版，点了直接下载这个版本对应的安装包
- 还没发过正式版时，下载按钮写「暂未发布」（英文页 Not released yet）并禁用
- 下载区的「查看所有版本」（英文页 All releases）指向仓库的 Releases 页面

### 终端界面

不适用：官网是网页，没有命令行输出。
