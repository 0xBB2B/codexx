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
		{"英文中台标注", "Relay models are labelled “中台” (relay) in the list."},
		{"英文 relay 说明", "This page calls your own model service the “relay”."},
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
