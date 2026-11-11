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

给想在 Codex 里用中台模型的普通用户用的跨平台桌面小工具，要让人觉得它像系统自带的设置工具：安静、可信、不打扰。界面上一律用中性名称和抽象图形，不出现服务品牌、真实服务地址、密钥或模型名称。

能检查的特征：
- 原生桌面外观：macOS 用交通灯按钮和菜单栏入口，Windows 用系统窗口按钮和任务栏通知区域入口；不用网页导航、面包屑或官网式横幅。
- 系统原生控件优先：按钮、输入框、开关、勾选、菜单、确认对话框都用系统控件（macOS 用 SwiftUI/AppKit 标准控件，Windows 用 WPF Fluent 主题）。两个平台都固定为浅色外观（macOS 设 aqua 外观，WPF 设 ThemeMode="Light"），因为本规范只定义了浅色配色。本文件的颜色只作强调色（如主操作按钮、开关打开、提示条图标）；控件的尺寸、圆角、悬停、按下、聚焦样式以系统为准，下面「组件」一节里的数值只约束自绘部分（分组卡片、提示条、状态区）。
- 紧凑的分组表单：窗口默认宽 620px，内容分组放在白色卡片里，卡片内边距 24px。
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
| 选中浅底（菜单项悬停、显隐按钮按下） | selected-bg | #e7f2f0 | 不适用：只有浅色主题 | 主色文字对它 4.78:1 |
| 成功 | success | #16734b | 不适用：只有浅色主题 | 5.85:1（对 surface）；5.49:1（对 success-bg #f0faf4） |
| 警告 | warning | #8a530e | 不适用：只有浅色主题 | 6.02:1（对 warning-bg #fff9ed） |
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

- 断点：窗口宽度小于 700px 时，内容区四周外边距改成 20px；小于 450px 时，表单标签放到控件上方
- 页面最大宽度：窗口默认宽度 620px，内容不够时不撑满屏幕
- 栅格：不适用：只有单栏表单，表单行用两列（标签 112px + 控件）

页面框架：

| 部分 | 位置和尺寸 | 写法（HTML 结构和类名） | 不同断点下怎么变 |
|---|---|---|---|
| 顶栏（窗口标题栏） | 窗口顶部；macOS 高 48px、标题居中、左侧交通灯；Windows 高 32px、标题靠左、右侧三个系统按钮 | `<div class="titlebar mac">`、`<div class="titlebar win">` | 不变 |
| 侧边栏 | 不适用：桌面工具不设侧栏 | — | — |
| 导航 | 不适用：没有页面导航，其他入口放在菜单栏或托盘菜单里 | — | — |
| 页面标题区 | 只在首次设置窗口出现，位于内容区顶部 | `<div class="intro">` | 不变 |
| 内容区 | 标题栏下方，四周 24px，分组之间 20px | `<div class="win-body">` 内放 `<section class="group">` | 小于 700px 时外边距改成 20px |
| 底部状态区 | 窗口底部，最小高度 48px，左右 24px，显示版本、运行状态和「检查更新」 | `<div class="statusbar">` | 不变 |

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
<div class="intro"><h2>连接到中台</h2><p class="muted">说明文字</p></div>
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
| 禁用 | 背景 disabled-bg，文字 text-disabled |
| 聚焦 | 2px focus 描边，向外偏移 3px |

出错时边框改成 danger，字段下方 8px 处显示 12px 的 danger 色图标和文字。

### 开关

- 写法：`<div class="switch-row"><button class="switch" role="switch" aria-checked="true" aria-label="转发到中台"><span class="knob"></span></button><span class="switch-text"><strong>已开启</strong></span></div>`
- 尺寸：40×24px，圆点 16×16px，整行最小高度 44px；旁边一律写「已开启 / 已关闭」，不只靠颜色区分

| 状态 | 样子 |
|---|---|
| 默认 | 打开：背景和边框 primary，圆点 on-primary 靠右；关闭：背景 disabled-bg，边框 1px border-strong，圆点 border-strong 靠左 |
| 悬停 | 鼠标变成手形 |
| 按下 | 圆点 200ms 滑到另一侧 |
| 禁用 | 边框和圆点 border，鼠标显示禁止，下方帮助文字写原因 |
| 聚焦 | 2px focus 描边，向外偏移 3px |

### 表格

不适用：界面上没有列表数据。键值信息用 `<dl class="kv">` 两列显示（标签列 112px，间距 12px）。

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

- 写法：用系统确认对话框（macOS 用 NSAlert，Windows 用 Fluent 主题下的 MessageBox 或 ContentDialog 风格的原生对话框）。标题、说明、按钮文字照各需求文档；确认按钮写明具体动作（如「关闭并重启」），取消按钮为默认焦点，Escape 等同于取消。
- 样式、尺寸、阴影、遮罩都以系统为准。

### 提示条

- 写法：`<div class="alert warning" role="status"><svg class="si">…</svg><div><div class="alert-title">标题</div><div class="alert-text">说明</div><div class="alert-actions"><button class="btn-secondary sm">重试</button></div></div></div>`；错误用 `class="alert danger" role="alert"`，另有 `info`、`success`
- 放在窗口内容区顶部；内边距 16px，圆角 8px，边框 1px 状态色，背景对应的状态浅底；标题用状态色 600 字重，说明用 text；按钮高 44px、左右 16px；不自动消失，需要用户处理的要给出操作

| 状态 | 样子（关闭按钮、操作链接） |
|---|---|
| 默认 | 操作按 btn-secondary 或 btn-link |
| 悬停 | 按「按钮」一节 |
| 按下 | 按「按钮」一节 |
| 禁用 | 处理中时按钮禁用并显示转圈 |
| 聚焦 | 2px focus 描边，向外偏移 3px |

### 菜单（菜单栏菜单 / 托盘菜单）

- 写法：用系统原生菜单（macOS 用 MenuBarExtra 的菜单样式，Windows 用托盘图标的原生右键菜单）。第一行是不可点的状态说明「转发已开启/已关闭 · 后台运行中」，可勾选的项用系统勾选标记，快捷键用系统显示方式。
- 分组顺序：状态 / 转发、打开配置 / 登录时启动、检查更新 / 卸载、退出，组之间用系统分隔线。
- 宽度、行高、悬停、键盘导航以系统为准。

### 加载指示

- 写法：`<span class="spinner" aria-hidden="true"></span>`，旁边一定有说明正在做什么的文字
- 16×16px，2px 边框，右侧透明，800ms 转一圈；处理期间禁止重复提交，不显示假的百分比

## 8. 动效

| 项 | 时长 | 缓动 |
|---|---|---|
| 悬停、按下反馈 | 200ms | ease-out |
| 展开、收起（菜单、开关圆点滑动） | 200ms | ease-out |
| 弹窗出现、消失 | 200ms | ease-out |
| 转圈指示 | 800ms 一圈 | linear |

- 操作开始 200ms 后仍未完成才显示加载状态；提示条不自动消失
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
- 改动 Codex 配置后需要重启 ChatGPT 才生效时，要用弹窗说明会中断 Codex 当前任务，用户确认后才重启
- 关闭主窗口只隐藏窗口，入口仍在菜单栏或通知区域；退出程序才会移除入口并停止检查更新
- 检查更新只针对本工具，启动时查一次，之后每 24 小时一次；只提示新版并打开下载页，不暗示更新官方客户端

例外：用户不接受某条不达标的修改建议时记在这里，以后检查时不再报。

| 项 | 实际值 | 用户确认日期 |
|---|---|---|

## 10. 终端界面

不适用：本项目只有图形界面，没有命令行输出。
