package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

func TestSite_FirstRunNameLabel(t *testing.T) {
	t.Parallel()
	if !regexp.MustCompile(`>名字\s*<`).MatchString(readSite(t)) {
		t.Error("页面缺少「名字」栏")
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
	narrow := regexp.MustCompile(`@media\(max-width:356px\)\{[^@]*?\.am\{[^}]*zoom:0?\.37`).FindStringIndex(html)
	if narrow == nil {
		t.Fatal("找不到 max-width:356px 媒体查询里把 .am 的 zoom 设为 .37 的规则")
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

func TestSite_FirstRunMockupMatchesApp(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	start := strings.Index(html, `class="am-intro"`)
	if start < 0 {
		t.Fatal("找不到首次设置示意图")
	}
	end := strings.Index(html[start:], "</figure>")
	if end < 0 {
		t.Fatal("首次设置示意图没有闭合")
	}
	block := html[start : start+end]

	if !strings.Contains(block, "<p>填写中转服务提供的 Base URL 和 API Key。") {
		t.Error("说明文字应以「填写中转服务提供的 Base URL 和 API Key。」开头")
	}
	if !regexp.MustCompile(`am-label">名字\s*<span class="am-req">必填</span>`).MatchString(block) {
		t.Error("「名字」标签后应有「必填」")
	}
	if !strings.Contains(block, `<div class="am-help">1~16 个字，不能重名，不能含「·」。</div>`) {
		t.Error("名字下面的帮助文字应为「1~16 个字，不能重名，不能含「·」。」")
	}
}

func cssDeclarations(html string, match func(selector string) bool) []map[string]string {
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`(--[\w-]+):\s*([^;}]+)`).FindAllStringSubmatch(html, -1) {
		vars[m[1]] = strings.TrimSpace(m[2])
	}
	resolve := func(v string) string {
		return regexp.MustCompile(`var\((--[\w-]+)\)`).ReplaceAllStringFunc(strings.ToLower(v), func(ref string) string {
			return vars[regexp.MustCompile(`--[\w-]+`).FindString(ref)]
		})
	}
	var out []map[string]string
	for _, m := range regexp.MustCompile(`([^{}@;]+)\{([^{}]*)\}`).FindAllStringSubmatch(html, -1) {
		hit := false
		for _, sel := range strings.Split(m[1], ",") {
			if match(strings.Join(strings.Fields(sel), " ")) {
				hit = true
			}
		}
		if !hit {
			continue
		}
		decls := map[string]string{}
		for _, d := range strings.Split(m[2], ";") {
			name, value, ok := strings.Cut(d, ":")
			if ok {
				decls[strings.TrimSpace(name)] = strings.Join(strings.Fields(resolve(strings.TrimSpace(value))), " ")
			}
		}
		out = append(out, decls)
	}
	return out
}

func declared(rules []map[string]string, name string) string {
	var got string
	for _, r := range rules {
		if v, ok := r[name]; ok {
			got = v
		}
	}
	return got
}

func TestSite_TextLinksHoverAndActiveColors(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	links := []struct {
		name string
		base string
	}{
		{"顶栏 GitHub 链接", ".gh"},
		{"页脚链接", ".foot-r a"},
	}
	states := []struct {
		pseudo string
		want   string
	}{
		{":hover", "#115e59"},
		{":active", "#172b3a"},
	}
	for _, l := range links {
		for _, st := range states {
			t.Run(l.name+st.pseudo, func(t *testing.T) {
				t.Parallel()
				rules := cssDeclarations(html, func(sel string) bool { return sel == l.base+st.pseudo })
				if got := declared(rules, "color"); got != st.want {
					t.Errorf("%s%s 的 color = %q，want %q", l.base, st.pseudo, got, st.want)
				}
			})
		}
	}
}

func TestSite_TextLinksDefaultColorUnchanged(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	rules := cssDeclarations(html, func(sel string) bool { return sel == "a" })
	if got := declared(rules, "color"); got != "#0f766e" {
		t.Errorf("a 的默认 color = %q，want #0f766e", got)
	}
}

func TestSite_PressedLooksSelected(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		name     string
		active   func(sel string) bool
		selected func(sel string) bool
		bg       string
		fg       string
	}{
		{
			"语言切换",
			func(sel string) bool {
				return strings.HasPrefix(sel, ".lang button") && strings.HasSuffix(sel, ":active")
			},
			func(sel string) bool { return sel == `.lang button[aria-pressed="true"]` },
			"#e7f2f0", "#0f766e",
		},
		{
			"页签",
			func(sel string) bool {
				return strings.HasPrefix(sel, ".tabs button") && strings.HasSuffix(sel, ":active")
			},
			func(sel string) bool { return sel == `.tabs button[aria-selected="true"]` },
			"#0f766e", "#fff",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			active := cssDeclarations(html, tt.active)
			selected := cssDeclarations(html, tt.selected)
			if len(active) == 0 {
				t.Fatal("没有 :active 规则")
			}
			for _, prop := range []string{"background", "color"} {
				if a, s := declared(active, prop), declared(selected, prop); a == "" || a != s {
					t.Errorf(":active 的 %s = %q，选中样式的 %s = %q，应一致", prop, a, prop, s)
				}
			}
			if got := declared(active, "background"); got != tt.bg {
				t.Errorf(":active 的 background = %q，want %s", got, tt.bg)
			}
			if got := declared(active, "color"); got != tt.fg {
				t.Errorf(":active 的 color = %q，want %s", got, tt.fg)
			}
		})
	}
}

func TestSite_FaqSecondAnswer(t *testing.T) {
	t.Parallel()
	second := regexp.MustCompile(`(?s)<details\b[^>]*>.*?</details>`).FindAllString(readSite(t), -1)
	if len(second) < 2 {
		t.Fatalf("details 数量 = %d", len(second))
	}
	answer := second[1]
	tests := []struct {
		name string
		text string
	}{
		{"中文回答", `<span data-l="zh">不会，只发给官方服务。请求先到本机的助手，再转给官方服务，官方凭据也只发往官方服务。</span>`},
		{"英文回答不变", `<span data-l="en">It goes to the official service only. The request reaches ModelBridge on your machine, which passes official-model requests to the official service. Official credentials go there and nowhere else.</span>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(answer, tt.text) {
				t.Errorf("第 2 问缺少 %s", tt.text)
			}
		})
	}
	if strings.Contains(answer, "助手再把官方模型的请求转给官方服务") {
		t.Error("第 2 问仍是旧的中文回答")
	}
}

func sectionOf(t *testing.T, html, id string) string {
	t.Helper()
	start := strings.Index(html, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("找不到区块 %q", id)
	}
	end := strings.Index(html[start:], "</section>")
	if end < 0 {
		t.Fatalf("区块 %q 没有闭合", id)
	}
	return html[start : start+end]
}

const disabledButton = `<button class="btn btn-1" type="button" disabled>`

func TestSite_Unreleased_HeroHasOnlyDisabledButton(t *testing.T) {
	t.Parallel()
	hero := sectionOf(t, readSite(t), "hero")
	if n := strings.Count(hero, disabledButton); n != 1 {
		t.Fatalf("首屏禁用按钮 %s 出现 %d 次，want 1", disabledButton, n)
	}
	for _, want := range []string{"暂未发布", "Not released yet"} {
		if !strings.Contains(hero, want) {
			t.Errorf("首屏缺少 %q", want)
		}
	}
	if re := regexp.MustCompile(`<a\b[^>]*href="[^"]*/releases[^"]*"`); re.MatchString(hero) {
		t.Errorf("首屏仍有指向 /releases 的链接: %s", re.FindString(hero))
	}
	if regexp.MustCompile(`<a\b[^>]*class="[^"]*\bbtn-1\b`).MatchString(hero) {
		t.Error("首屏仍有链接形式的主按钮")
	}
	btn, src := strings.Index(hero, disabledButton), strings.Index(hero, "查看源码")
	if src < 0 || src < btn {
		t.Errorf("「查看源码」应在禁用按钮之后: 按钮 %d，查看源码 %d", btn, src)
	}
}

func TestSite_Unreleased_DownloadSection(t *testing.T) {
	t.Parallel()
	dl := sectionOf(t, readSite(t), "download")
	cards := regexp.MustCompile(`(?s)<article class="get">.*?</article>`).FindAllString(dl, -1)
	if len(cards) != 2 {
		t.Fatalf("下载卡片 = %d 张，want 2", len(cards))
	}
	for i, card := range cards {
		if n := strings.Count(card, disabledButton); n != 1 {
			t.Errorf("第 %d 张卡片禁用按钮 %d 个，want 1", i+1, n)
		}
		if !strings.Contains(card, "暂未发布") || !strings.Contains(card, "Not released yet") {
			t.Errorf("第 %d 张卡片缺少「暂未发布」中英文", i+1)
		}
		if strings.Contains(card, "<a ") {
			t.Errorf("第 %d 张卡片里不应有链接", i+1)
		}
	}
	for _, want := range []string{"正式版还没有发布。", "No release has been published yet."} {
		if !strings.Contains(dl, want) {
			t.Errorf("下载区缺少说明 %q", want)
		}
	}
	if !strings.Contains(dl, `href="https://github.com/0xBB2B/codexx/releases"`) || !strings.Contains(dl, "查看所有版本") {
		t.Error("下载区缺少指向 /releases 的「查看所有版本」链接")
	}
	if !strings.Contains(dl, "All releases") {
		t.Error("下载区缺少英文「All releases」")
	}
}

func TestSite_NoInstallerFileNameShown(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	if re := regexp.MustCompile(`model-bridge-[^"\s]*-(?:macos\.dmg|windows-setup\.exe)`); re.MatchString(html) {
		t.Errorf("页面出现安装包文件名: %s", re.FindString(html))
	}
	for _, banned := range []string{"文件名", "File name"} {
		if strings.Contains(html, banned) {
			t.Errorf("页面仍含 %q", banned)
		}
	}
}

func TestSite_StepOneText(t *testing.T) {
	t.Parallel()
	start := sectionOf(t, readSite(t), "start")
	for _, want := range []string{
		"先装好 ChatGPT 桌面版，再用本页的下载按钮下载安装包。安装包没有开发者签名，第一次打开要手动放行，步骤写在「下载」一节。",
		"Install the ChatGPT desktop app first, then get the installer with a download button on this page. The installer is not code-signed, so you approve it by hand the first time. The steps are in the Download section.",
	} {
		if !strings.Contains(start, want) {
			t.Errorf("三步上手缺少第 1 步文字 %q", want)
		}
	}
}

func TestSite_DisabledButtonColors(t *testing.T) {
	t.Parallel()
	var found bool
	for _, m := range regexp.MustCompile(`\{([^{}]*)\}`).FindAllStringSubmatch(readSite(t), -1) {
		body := strings.ToLower(strings.Join(strings.Fields(m[1]), ""))
		if strings.Contains(body, "background:#e9edf1") && strings.Contains(body, "color:#586878") {
			found = true
		}
	}
	if !found {
		t.Error("没有同时设置 background:#e9edf1 和 color:#586878 的 CSS 规则")
	}
}

func TestSite_SitebuildOutputPassesCheck(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "sitebuild")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "../sitebuild"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建 sitebuild 失败: %v\n%s", err, out)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	if out, err := exec.Command(bin, siteDir, outDir, "1.2.3").CombinedOutput(); err != nil {
		t.Fatalf("运行 sitebuild 失败: %v\n%s", err, out)
	}
	got, err := Check(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("sitebuild 的输出有 %d 处违规: %v", len(got), got)
	}
	html, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "releases/download/v1.2.3/model-bridge-1.2.3-macos.dmg") {
		t.Error("输出里没有 macOS 直链，检查通过没有意义")
	}
}

func TestSite_DisabledButtonStaysPutOnHoverAndActive(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for _, pseudo := range []string{":hover", ":active"} {
		t.Run(pseudo, func(t *testing.T) {
			t.Parallel()
			rules := cssDeclarations(html, func(sel string) bool {
				return strings.Contains(sel, "disabled") && strings.HasSuffix(sel, pseudo)
			})
			if len(rules) == 0 {
				t.Fatalf("没有禁用按钮的 %s 规则", pseudo)
			}
			bg := declared(rules, "background")
			if bg == "" {
				bg = declared(rules, "background-color")
			}
			if bg != "#e9edf1" {
				t.Errorf("background = %q，want #e9edf1", bg)
			}
			if got := declared(rules, "color"); got != "#586878" {
				t.Errorf("color = %q，want #586878", got)
			}
			if got := declared(rules, "transform"); got != "none" {
				t.Errorf("transform = %q，want none", got)
			}
		})
	}
}

func TestSite_AllReleasesLinkHoverAndActiveColors(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	for pseudo, want := range map[string]string{":hover": "#115e59", ":active": "#172b3a"} {
		t.Run(pseudo, func(t *testing.T) {
			t.Parallel()
			rules := cssDeclarations(html, func(sel string) bool { return sel == ".more a"+pseudo })
			if got := declared(rules, "color"); got != want {
				t.Errorf(".more a%s 的 color = %q，want %q", pseudo, got, want)
			}
		})
	}
}

func TestSite_ButtonGrowsWhenTextWraps(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	rules := cssDeclarations(html, func(sel string) bool { return sel == ".btn" })
	if got := declared(rules, "min-height"); got != "48px" {
		t.Errorf(".btn 的 min-height = %q，want 48px", got)
	}
	if got := declared(rules, "white-space"); got == "nowrap" {
		t.Error(".btn 不能设 white-space:nowrap，文字要能折行")
	}
	for _, r := range cssDeclarations(html, func(sel string) bool {
		fields := strings.Fields(sel)
		return len(fields) > 0 && strings.HasPrefix(fields[len(fields)-1], ".btn")
	}) {
		if h, ok := r["height"]; ok {
			t.Errorf("按钮规则不能用固定 height（%s），应改用 min-height", h)
		}
	}
}

func sideValues(rules []map[string]string, prop string) (left, right string) {
	if v := declared(rules, prop); v != "" {
		f := strings.Fields(v)
		switch len(f) {
		case 1:
			return f[0], f[0]
		case 2:
			return f[1], f[1]
		case 3:
			return f[1], f[1]
		case 4:
			return f[3], f[1]
		}
	}
	return declared(rules, prop+"-left"), declared(rules, prop+"-right")
}

func brandBackground(rules []map[string]string) string {
	if bg := declared(rules, "background"); bg != "" {
		return bg
	}
	return declared(rules, "background-color")
}

func TestSite_BrandLinkStateColors(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	tests := []struct {
		pseudo string
		want   string
	}{
		{":hover", "#f5f7fa"},
		{":active", "#e9edf1"},
	}
	for _, tt := range tests {
		t.Run(tt.pseudo, func(t *testing.T) {
			t.Parallel()
			rules := cssDeclarations(html, func(sel string) bool { return sel == ".brand"+tt.pseudo })
			if len(rules) == 0 {
				t.Fatalf("没有 .brand%s 规则", tt.pseudo)
			}
			if got := brandBackground(rules); got != tt.want {
				t.Errorf(".brand%s 的背景 = %q，want %s", tt.pseudo, got, tt.want)
			}
			if got := declared(rules, "color"); got != "" {
				t.Errorf(".brand%s 不应改 color，实际 %q", tt.pseudo, got)
			}
		})
	}
}

func TestSite_BrandLinkBox(t *testing.T) {
	t.Parallel()
	rules := cssDeclarations(readSite(t), func(sel string) bool { return sel == ".brand" })
	if got := declared(rules, "min-height"); got != "44px" {
		t.Errorf(".brand 的 min-height = %q，want 44px", got)
	}
	if got := declared(rules, "border-radius"); got != "8px" {
		t.Errorf(".brand 的 border-radius = %q，want 8px", got)
	}
	if l, r := sideValues(rules, "padding"); l != "8px" || r != "8px" {
		t.Errorf(".brand 左右 padding = %q/%q，want 8px/8px", l, r)
	}
	if l, r := sideValues(rules, "margin"); l != "-8px" || r != "-8px" {
		t.Errorf(".brand 左右 margin = %q/%q，want -8px/-8px", l, r)
	}
}

func TestSite_BrandLinkNarrowScreen(t *testing.T) {
	t.Parallel()
	var found bool
	for _, block := range regexp.MustCompile(`@media\s*\(max-width:\s*400px\)\s*\{(?:[^{}]*\{[^{}]*\})*\s*\}`).FindAllString(readSite(t), -1) {
		rules := cssDeclarations(block, func(sel string) bool { return sel == ".brand" })
		pl, pr := sideValues(rules, "padding")
		ml, mr := sideValues(rules, "margin")
		if pl == "4px" && pr == "4px" && ml == "-4px" && mr == "-4px" {
			found = true
		}
	}
	if !found {
		t.Error("max-width:400px 媒体查询里没有把 .brand 的左右 padding 设为 4px、左右 margin 设为 -4px")
	}
}

func TestSite_AppFigureWidth(t *testing.T) {
	t.Parallel()
	html := readSite(t)
	if got := regexp.MustCompile(`\.am-(window|scene)\{width:(\d+)px;`).FindAllStringSubmatch(html, -1); len(got) != 2 || got[0][2] != "760" || got[1][2] != "760" {
		t.Errorf("界面图容器宽度 = %v，want .am-window 和 .am-scene 都是 760px", got)
	}
	got := regexp.MustCompile(`@media \(max-width:\d+px\)\{\.am\{zoom:\.\d+\}\}`).FindAllString(html, -1)
	want := []string{
		`@media (max-width:808px){.am{zoom:.65}}`,
		`@media (max-width:600px){.am{zoom:.42}}`,
		`@media (max-width:356px){.am{zoom:.37}}`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("界面图缩放规则 = %q，want %q", got, want)
	}
}
