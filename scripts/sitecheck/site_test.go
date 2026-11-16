package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const siteDir = "../../site"

func readSite(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(siteDir + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSite_SectionOrder(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	ids := []string{"top", "hero", "screens", "start", "modes", "security", "faq", "download", "footer"}
	prev := -1
	for _, id := range ids {
		pos := strings.Index(html, `id="`+id+`"`)
		if pos < 0 {
			t.Fatalf("缺少区块 id=%q", id)
		}
		if pos <= prev {
			t.Errorf("区块 %q 的位置 %d 不在前一个区块之后 %d", id, pos, prev)
		}
		prev = pos
	}
}

func TestSite_TopbarLinksOrder(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	start := strings.Index(html, `id="top"`)
	end := strings.Index(html, `id="hero"`)
	if start < 0 || end < start {
		t.Fatal("找不到 top 与 hero 区块")
	}
	bar := html[start:end]
	prev := -1
	for _, anchor := range []string{"screens", "start", "modes", "security", "faq", "download"} {
		pos := strings.Index(bar, `href="#`+anchor+`"`)
		if pos < 0 {
			t.Fatalf("顶栏缺少链接 #%s", anchor)
		}
		if pos <= prev {
			t.Errorf("顶栏链接 #%s 顺序不对", anchor)
		}
		prev = pos
	}
}

func TestSite_RequiredText(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		name string
		text string
	}{
		{"中文标语", "让 ChatGPT 桌面版里的 Codex，用上你自己的模型服务"},
		{"首次设置", "首次设置"},
		{"主窗口", "主窗口"},
		{"菜单栏菜单", "菜单栏菜单"},
		{"下载按钮", "去 Releases 页面下载"},
		{"商标声明", "ChatGPT 和 Codex 是各自所有者的商标，本项目与它们的所有者没有隶属关系。"},
		{"英文界面仅中文", "The app’s interface is available in Chinese only."},
		{"英文中转标注", "Relay models are labelled “中转” (relay) in the list."},
		{"英文 relay 说明", "This page calls your own model service the “relay”."},
		{"中文首屏末句", "这个页面把你自己的模型服务叫作「中转」。"},
		{"转发总览标题", "使用中转 · 已开启"},
		{"转发总览说明", "官方模型在上，中转模型在下；选官方模型时仍访问官方服务。"},
		{"已检测到登录", "· 已检测到 ChatGPT 登录"},
		{"中转配置卡片", "中转配置"},
		{"配置数量说明", "已有 2 个，最多 10 个。"},
		{"首次设置大标题", "连接中转服务"},
		{"首次设置名字栏", ">名字<"},
		{"模型名示例", "中转 · auto"},
		{"第二个配置模型示例", "A家 · auto"},
		{"第二个配置示例地址", "https://relay.example.com/alt/v1"},
		{"语言存储键", "modelbridge-site-lang"},
		{"下载链接", "https://github.com/0xBB2B/codexx/releases"},
		{"源码链接", "https://github.com/0xBB2B/codexx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(html, tt.text) {
				t.Errorf("页面缺少文字 %q", tt.text)
			}
		})
	}
}

func TestSite_NoOldTerms(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for _, old := range []string{"中台", "运行状态", "relay2.example.com"} {
		if strings.Contains(html, old) {
			t.Errorf("页面仍含旧文字 %q", old)
		}
	}
}

func TestReadme_NoOldTerm(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "中台") {
		t.Error("README 仍含「中台」")
	}
}

func pane(t *testing.T, html, index string) string {
	t.Helper()
	p := regexp.MustCompile(`(?s)<figure\b[^>]*data-pane="` + index + `"[^>]*>.*?</figure>`).FindString(html)
	if p == "" {
		t.Fatalf("找不到 data-pane=%q 的面板", index)
	}
	return p
}

func TestSite_PaneContent(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		name string
		pane string
		text string
	}{
		{"接入方式", "1", `class="lv-label">接入方式<`},
		{"官方加中转", "1", "官方 + 中转"},
		{"重新检测", "1", "重新检测"},
		{"配置一", "1", "<span>中转</span>"},
		{"配置二", "1", "<span>A家</span>"},
		{"添加配置", "1", "添加配置"},
		{"菜单栏使用中转", "2", "使用中转"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(pane(t, html, tt.pane), tt.text) {
				t.Errorf("面板 %s 缺少文字 %q", tt.pane, tt.text)
			}
		})
	}
}

func TestSite_MainWindowStatusRow(t *testing.T) {
	t.Parallel()
	main := pane(t, readSite(t), "1")
	tests := []struct {
		name string
		re   string
	}{
		{"ChatGPT 标签", `class="lv-label">ChatGPT<`},
		{"ChatGPT 取值", `class="lv-value[^"]*">已检测到<`},
		{"中转模型标签", `class="lv-label">中转模型<`},
		{"中转模型数量", `class="lv-value[^"]*">10 个<`},
		{"配置数量", `（2 个配置）`},
		{"版本号等宽", `class="[^"]*\bam-mono\b[^"]*"[^>]*>v1\.0\.0<`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !regexp.MustCompile(tt.re).MatchString(main) {
				t.Errorf("主窗口面板里没有匹配 %s", tt.re)
			}
		})
	}
}

func TestSite_AddConfigButtonNotSmall(t *testing.T) {
	t.Parallel()
	m := regexp.MustCompile(`<span class="([^"]*)">添加配置</span>`).FindStringSubmatch(pane(t, readSite(t), "1"))
	if m == nil {
		t.Fatal("找不到「添加配置」按钮")
	}
	if regexp.MustCompile(`\bsm\b`).MatchString(m[1]) {
		t.Errorf("「添加配置」按钮带 sm 类: %q", m[1])
	}
}

func TestSite_MenubarIconSize(t *testing.T) {
	t.Parallel()
	rules := cssRules(readSite(t), ".am-mb-ic .ico")
	for _, want := range []string{"width:18px", "height:18px"} {
		if !anyRuleContains(rules, want) {
			t.Errorf(".am-mb-ic .ico 规则里没有 %s", want)
		}
	}
}

func TestSite_FaqDetails(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tags := regexp.MustCompile(`<details\b[^>]*>`).FindAllString(html, -1)
	if len(tags) != 7 {
		t.Fatalf("details 数量 = %d，want 7", len(tags))
	}
	openAttr := regexp.MustCompile(`\sopen\b`)
	for i, tag := range tags {
		got := openAttr.MatchString(tag)
		if want := i == 0; got != want {
			t.Errorf("第 %d 个 details 的 open = %v，want %v（%s）", i+1, got, want, tag)
		}
	}
}

func TestSite_NoExternalResources(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for _, banned := range []string{`<link rel="stylesheet"`, `<script src=`, `src="http`, `url(http`, `xmlns`, `src="//`, `href="//`, `@import`} {
		t.Run(banned, func(t *testing.T) {
			t.Parallel()
			if strings.Contains(html, banned) {
				t.Errorf("页面含禁用片段 %q", banned)
			}
		})
	}
}

func TestSite_NoStylesheetLinkInAnyAttributeOrder(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	if tag := regexp.MustCompile(`<link\b[^>]*stylesheet[^>]*>`).FindString(html); tag != "" {
		t.Errorf("页面含样式表 link: %s", tag)
	}
}

func TestSite_ScreenshotTabsDefaultState(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		name     string
		index    string
		selected string
		hidden   bool
	}{
		{"首次设置", "0", "false", true},
		{"主窗口", "1", "true", false},
		{"菜单栏菜单", "2", "false", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tab := regexp.MustCompile(`<button\b[^>]*role="tab"[^>]*data-tab="` + tt.index + `"[^>]*>`).FindString(html)
			if tab == "" {
				t.Fatalf("找不到 data-tab=%q 的页签", tt.index)
			}
			if !strings.Contains(tab, `aria-selected="`+tt.selected+`"`) {
				t.Errorf("页签 aria-selected 应为 %s: %s", tt.selected, tab)
			}
			pane := regexp.MustCompile(`<figure\b[^>]*data-pane="` + tt.index + `"[^>]*>`).FindString(html)
			if pane == "" {
				t.Fatalf("找不到 data-pane=%q 的面板", tt.index)
			}
			if got := regexp.MustCompile(`\shidden\b`).MatchString(pane); got != tt.hidden {
				t.Errorf("面板 hidden = %v，want %v: %s", got, tt.hidden, pane)
			}
		})
	}
}

func TestSite_ScreenshotsInert(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for _, index := range []string{"0", "1", "2"} {
		t.Run("面板"+index, func(t *testing.T) {
			t.Parallel()
			pane := regexp.MustCompile(`(?s)<figure\b[^>]*data-pane="` + index + `"[^>]*>.*?</figure>`).FindString(html)
			if pane == "" {
				t.Fatalf("找不到 data-pane=%q 的面板", index)
			}
			if !regexp.MustCompile(`\sinert\b`).MatchString(pane) {
				t.Errorf("面板 %s 里没有带 inert 的元素", index)
			}
		})
	}
}

func TestSite_TopbarLinks(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	start := strings.Index(html, `id="top"`)
	end := strings.Index(html, `id="hero"`)
	if start < 0 || end < start {
		t.Fatal("找不到 top 与 hero 区块")
	}
	bar := html[start:end]
	for _, link := range []string{`href="https://github.com/0xBB2B/codexx"`, `href="#top"`} {
		if !strings.Contains(bar, link) {
			t.Errorf("顶栏缺少链接 %s", link)
		}
	}
}

func TestSite_TopAnchorIsNotStickyHeader(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tag := regexp.MustCompile(`<(\w+)\b[^>]*\bid="top"[^>]*>`).FindStringSubmatch(html)
	if tag == nil {
		t.Fatal("找不到 id=\"top\" 的元素")
	}
	if tag[1] == "header" {
		t.Errorf("id=\"top\" 在吸顶的 header 上: %s", tag[0])
	}
	if top, header := strings.Index(html, `id="top"`), strings.Index(html, "<header"); top > header {
		t.Errorf("id=\"top\" 位置 %d 应在 <header 位置 %d 之前", top, header)
	}
}

func TestSite_StageDoesNotClipShadow(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for _, rule := range regexp.MustCompile(`\.stage\s*\{[^}]*\}`).FindAllString(html, -1) {
		if strings.Contains(strings.ReplaceAll(rule, " ", ""), "overflow:hidden") {
			t.Errorf(".stage 规则含 overflow:hidden: %s", rule)
		}
	}
}

func TestSite_LangSetInHeadScript(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	body := strings.Index(html, "<body")
	if body < 0 {
		t.Fatal("找不到 <body")
	}
	var found bool
	for _, script := range regexp.MustCompile(`(?s)<script\b[^>]*>.*?</script>`).FindAllString(html[:body], -1) {
		if strings.Contains(script, "modelbridge-site-lang") && strings.Contains(script, "document.documentElement.lang") {
			found = true
		}
	}
	if !found {
		t.Error("<body 之前没有同时引用 modelbridge-site-lang 并设置 document.documentElement.lang 的内联 script")
	}
}

func TestSite_NarrowScreenZoomOverridesLaterThan600(t *testing.T) {
	t.Parallel()
	html := strings.Join(strings.Fields(readSite(t)), "")
	narrow := regexp.MustCompile(`@media\(max-width:356px\)\{[^@]*?\.am\{[^}]*zoom:0?\.46`).FindStringIndex(html)
	if narrow == nil {
		t.Fatal("找不到 max-width:356px 媒体查询里把 .am 的 zoom 设为 .46 的规则")
	}
	mid := regexp.MustCompile(`@media\(max-width:600px\)\{\.am\{[^}]*zoom:`).FindStringIndex(html)
	if mid == nil {
		t.Fatal("找不到 max-width:600px 媒体查询里 .am 的 zoom 规则")
	}
	if narrow[0] < mid[0] {
		t.Errorf("356px 规则位置 %d 应在 600px 规则位置 %d 之后", narrow[0], mid[0])
	}
}

func cssRules(html, selector string) []string {
	var bodies []string
	for _, m := range regexp.MustCompile(regexp.QuoteMeta(selector)+`\s*\{([^}]*)\}`).FindAllStringSubmatch(html, -1) {
		bodies = append(bodies, strings.Join(strings.Fields(m[1]), ""))
	}
	return bodies
}

func anyRuleContains(rules []string, want string) bool {
	for _, r := range rules {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

func TestSite_TabsStayOnOneLine(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		name     string
		selector string
		want     string
	}{
		{"页签条允许换行", ".tabs", "flex-wrap:wrap"},
		{"页签条最宽等于内容区", ".tabs", "max-width:100%"},
		{"页签文字不折行", ".tabs button", "white-space:nowrap"},
		{"语言按钮文字不折行", ".lang button", "white-space:nowrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !anyRuleContains(cssRules(html, tt.selector), tt.want) {
				t.Errorf("%s 规则里没有 %s", tt.selector, tt.want)
			}
		})
	}
}

func TestSite_TabPaddingOnNarrowScreen(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	var found bool
	for _, block := range regexp.MustCompile(`@media\s*\(max-width:\s*600px\)\s*\{(?:[^{}]*\{[^{}]*\})*\s*\}`).FindAllString(html, -1) {
		if anyRuleContains(cssRules(block, ".tabs button"), "padding:012px") {
			found = true
		}
	}
	if !found {
		t.Error("max-width:600px 媒体查询里没有把 .tabs button 的 padding 设为 0 12px")
	}
}

func TestSite_LangButtonPaddingOnNarrowScreen(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	var found bool
	for _, block := range regexp.MustCompile(`@media\s*\(max-width:\s*400px\)\s*\{(?:[^{}]*\{[^{}]*\})*\s*\}`).FindAllString(html, -1) {
		if anyRuleContains(cssRules(block, ".lang button"), "padding:04px") {
			found = true
		}
	}
	if !found {
		t.Error("max-width:400px 媒体查询里没有把 .lang button 的 padding 设为 0 4px")
	}
}

func TestSite_EnglishTabLabels(t *testing.T) {
	t.Parallel()
	html := regexp.MustCompile(`(?s)<div class="tabs".*?</div>`).FindString(readSite(t))
	if html == "" {
		t.Fatal("找不到页签条")
	}
	for _, want := range []string{`<span data-l="en">Setup</span>`, `<span data-l="en">Main window</span>`, `<span data-l="en">Menu bar</span>`} {
		if !strings.Contains(html, want) {
			t.Errorf("页面缺少英文页签 %s", want)
		}
	}
	for _, old := range []string{"First-run setup", "Menu bar menu"} {
		if strings.Contains(html, old) {
			t.Errorf("页面仍含旧英文页签文字 %q", old)
		}
	}
}

func TestSite_PassesCheck(t *testing.T) {
	t.Parallel()
	got, err := Check(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("违规 %d 处: %v", len(got), got)
	}
}
