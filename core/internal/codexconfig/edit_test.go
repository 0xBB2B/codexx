package codexconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	proxyURL = "http://127.0.0.1:47311/v1"
	relayURL = "https://relay.example.com/v1"
)

var managedKeys = map[string]bool{
	"openai_base_url":                   true,
	"model_catalog_json":                true,
	"model":                             true,
	"model_provider":                    true,
	"enabled-reasoning-efforts":         true,
	"desktop.enabled-reasoning-efforts": true,
}

type sample struct {
	name    string
	missing bool
	content string
}

var samples = []sample{
	{name: "文件不存在", missing: true},
	{name: "空文件", content: ""},
	{name: "带注释和其他表", content: "# 我的配置\n\n# 推理强度\nmodel_reasoning_effort = \"high\"\n\n\n[mcp_servers.x]\ncommand = \"foo\"  # 保留\n\n# 结尾注释\n"},
	{name: "已有 openai_base_url", content: "# 头\nopenai_base_url = \"" + relayURL + "\"\nsandbox = \"read-only\"\n\n[mcp_servers.x]\ncommand = \"foo\"\n"},
	{name: "已有 model_provider", content: "model_provider = \"relay\"\nmodel = \"gpt-x\"\n\n[model_providers.relay]\nbase_url = \"" + relayURL + "\"\n"},
	{name: "三项都已有", content: "openai_base_url = \"" + relayURL + "\"\nmodel_catalog_json = \"/old/catalog.json\"\nmodel_provider = \"relay\"\n\n[tui]\nnotifications = true\n"},
	{name: "有 desktop 表", content: "approval = \"never\"\n\n[desktop]\nenabled-reasoning-efforts = [\"low\", \"high\"]\ntheme = \"dark\"\n\n[mcp_servers.x]\ncommand = \"foo\"\n"},
	{name: "只有根上点号写法", content: "# c\ndesktop.x = 1\nother = 2\n\n[tui]\nnotifications = true\n"},
	{name: "无结尾换行", content: "a = 1\n[t]\nb = 2"},
	{name: "只有根键且无结尾换行", content: "a = 1"},
}

func newFixture(t *testing.T, s sample) (Config, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if !s.missing {
		if err := os.WriteFile(cfgPath, []byte(s.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Config{ConfigPath: cfgPath, DataDir: t.TempDir()}, cfgPath
}

func catalogPath(c Config) string {
	return filepath.Join(c.DataDir, "merged-catalog.json")
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", p, err)
	}
	return string(b)
}

func lineKey(line string) string {
	l := strings.TrimSpace(line)
	if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "[") {
		return ""
	}
	k, _, ok := strings.Cut(l, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(k)
}

func withoutManaged(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if managedKeys[lineKey(l)] {
			continue
		}
		out = append(out, l)
	}
	return out
}

func findLine(text, key string) (string, bool) {
	for _, l := range strings.Split(text, "\n") {
		if lineKey(l) == key {
			return l, true
		}
	}
	return "", false
}

func lineIndex(text, key string) int {
	for i, l := range strings.Split(text, "\n") {
		if lineKey(l) == key {
			return i
		}
	}
	return -1
}

var headerRe = regexp.MustCompile(`^\s*\[`)

func firstHeaderIndex(text string) int {
	for i, l := range strings.Split(text, "\n") {
		if headerRe.MatchString(l) {
			return i
		}
	}
	return -1
}

var quotedRe = regexp.MustCompile(`"([^"]*)"`)

func efforts(t *testing.T, text string) []string {
	t.Helper()
	l, ok := findLine(text, "enabled-reasoning-efforts")
	if !ok {
		l, ok = findLine(text, "desktop.enabled-reasoning-efforts")
	}
	if !ok {
		return nil
	}
	_, val, _ := strings.Cut(l, "=")
	var out []string
	for _, m := range quotedRe.FindAllStringSubmatch(val, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestEnable_OnlyManagedLinesChange(t *testing.T) {
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			c, cfg := newFixture(t, s)
			in := EnableInput{CatalogPath: catalogPath(c), HasAutoModel: true, Efforts: []string{"none", "high"}}
			if _, err := Enable(c, in); err != nil {
				t.Fatalf("Enable 返回错误: %v", err)
			}
			got := readFile(t, cfg)
			if want, have := withoutManaged(s.content), withoutManaged(got); !reflect.DeepEqual(want, have) {
				t.Fatalf("非接入项内容发生变化\n原文: %q\n现在: %q", s.content, got)
			}
			if l, ok := findLine(got, "openai_base_url"); !ok || !strings.Contains(l, `"`+proxyURL+`"`) {
				t.Fatalf("openai_base_url 行 = %q", l)
			}
			if l, ok := findLine(got, "model_catalog_json"); !ok || !strings.Contains(l, `"`+in.CatalogPath+`"`) {
				t.Fatalf("model_catalog_json 行 = %q", l)
			}
			if l, ok := findLine(got, "model"); !ok || !strings.Contains(l, `"auto"`) {
				t.Fatalf("model 行 = %q", l)
			}
			if _, ok := findLine(got, "model_provider"); ok {
				t.Fatalf("model_provider 应被移除: %q", got)
			}
		})
	}
}

func TestEnable_NewRootKeysBeforeFirstTable(t *testing.T) {
	s := sample{content: "# c\nfoo = 1\n\n[mcp_servers.x]\ncommand = \"foo\"\n"}
	c, cfg := newFixture(t, s)
	if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c), HasAutoModel: true}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, cfg)
	h := firstHeaderIndex(got)
	for _, k := range []string{"openai_base_url", "model_catalog_json", "model"} {
		if i := lineIndex(got, k); i < 0 || i > h {
			t.Fatalf("%s 在第 %d 行, 第一个表头在第 %d 行", k, i, h)
		}
	}
}

func TestEnable_ModelKey(t *testing.T) {
	tests := []struct {
		name      string
		hasAuto   bool
		wantModel string
	}{
		{"有 auto 时写入 auto", true, `"auto"`},
		{"无 auto 时保持用户原值", false, `"gpt-x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{content: "model = \"gpt-x\"\n"})
			if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c), HasAutoModel: tt.hasAuto}); err != nil {
				t.Fatal(err)
			}
			l, ok := findLine(readFile(t, cfg), "model")
			if !ok || !strings.Contains(l, tt.wantModel) {
				t.Fatalf("model 行 = %q", l)
			}
		})
	}
}

func TestEnable_NoModelLineWhenNoAutoAndNoneOriginally(t *testing.T) {
	c, cfg := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := findLine(readFile(t, cfg), "model"); ok {
		t.Fatal("没有 auto 模型时不应写 model")
	}
}

func TestEnable_Efforts(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		need       []string
		want       []string
		wantDotted bool
	}{
		{"已有列表时在末尾追加缺的等级", "[desktop]\nenabled-reasoning-efforts = [\"low\",\"high\"]\n", []string{"none", "high"}, []string{"low", "high", "none"}, false},
		{"已有列表且已包含全部等级时不变", "[desktop]\nenabled-reasoning-efforts = [\"low\",\"high\"]\n", []string{"high"}, []string{"low", "high"}, false},
		{"desktop 表里没有该键时新建", "[desktop]\ntheme = \"dark\"\n", []string{"a", "b"}, []string{"a", "b"}, false},
		{"没有 desktop 表时用根上点号写法", "foo = 1\n\n[tui]\nx = 1\n", []string{"a"}, []string{"a"}, true},
		{"根上只有点号写法的其他键时新增点号键", "desktop.x = 1\n\n[tui]\nx = 1\n", []string{"a"}, []string{"a"}, true},
		{"需要的等级为空时不写", "foo = 1\n", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{content: tt.content})
			if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c), Efforts: tt.need}); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, cfg)
			if have := efforts(t, got); !reflect.DeepEqual(have, tt.want) {
				t.Fatalf("等级 = %v, 期望 %v\n文件: %q", have, tt.want, got)
			}
			if tt.wantDotted {
				i := lineIndex(got, "desktop.enabled-reasoning-efforts")
				h := firstHeaderIndex(got)
				if i < 0 || (h >= 0 && i > h) {
					t.Fatalf("点号键应在第一个表头之前: %q", got)
				}
			}
			if tt.name == "desktop 表里没有该键时新建" {
				lines := strings.Split(got, "\n")
				i := lineIndex(got, "enabled-reasoning-efforts")
				d := -1
				for n, l := range lines {
					if strings.TrimSpace(l) == "[desktop]" {
						d = n
					}
				}
				if d < 0 || i <= d {
					t.Fatalf("新键应在 [desktop] 表内: %q", got)
				}
			}
		})
	}
}

func TestEnable_TakenOverKeys(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"没有任何旧值", "a = 1\n", nil},
		{"只有 openai_base_url", "openai_base_url = \"" + relayURL + "\"\n", []string{"openai_base_url"}},
		{"三项都有", "openai_base_url = \"" + relayURL + "\"\nmodel_catalog_json = \"/x.json\"\nmodel_provider = \"relay\"\n", []string{"model_catalog_json", "model_provider", "openai_base_url"}},
		{"model 有旧值不算接管", "model = \"gpt-x\"\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFixture(t, sample{content: tt.content})
			got, err := Enable(c, EnableInput{CatalogPath: catalogPath(c), HasAutoModel: true})
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(got)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("接管键 = %v, 期望 %v", got, tt.want)
			}
		})
	}
}
