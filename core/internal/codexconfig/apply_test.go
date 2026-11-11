package codexconfig

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func fullInput(c Config) EnableInput {
	return EnableInput{CatalogPath: catalogPath(c), HasAutoModel: true, Efforts: []string{"none", "high"}}
}

func requireCode(t *testing.T, err error, code string, keys []string) {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("期望 *Error, 得到 %v", err)
	}
	if ce.Code != code {
		t.Fatalf("错误码 = %q, 期望 %q", ce.Code, code)
	}
	if keys != nil && !reflect.DeepEqual(ce.Keys, keys) {
		t.Fatalf("键名 = %v, 期望 %v", ce.Keys, keys)
	}
}

func TestRestore_RoundTripByteExact(t *testing.T) {
	for _, s := range samples {
		for _, auto := range []bool{true, false} {
			t.Run(s.name, func(t *testing.T) {
				c, cfg := newFixture(t, s)
				in := fullInput(c)
				in.HasAutoModel = auto
				if _, err := Enable(c, in); err != nil {
					t.Fatal(err)
				}
				if err := Restore(c); err != nil {
					t.Fatalf("Restore 错误: %v", err)
				}
				if s.missing {
					if _, err := os.Stat(cfg); !os.IsNotExist(err) {
						t.Fatalf("原来没有配置文件, 还原后应删除: %v", err)
					}
				} else if got := readFile(t, cfg); got != s.content {
					t.Fatalf("还原后 = %q, 期望 %q", got, s.content)
				}
				if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
					t.Fatalf("access-state.json 应被删除: %v", err)
				}
			})
		}
	}
}

func TestRestore_KeepsUnrelatedEditsMadeWhileEnabled(t *testing.T) {
	orig := "# c\na = 1\n\n[mcp_servers.x]\ncommand = \"foo\"\n"
	c, cfg := newFixture(t, sample{content: orig})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	extra := "extra = \"relay.example.com\"\n"
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(extra); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); got != orig+extra {
		t.Fatalf("还原后 = %q", got)
	}
}

func TestRestore_RestoresOriginalTakenOverValues(t *testing.T) {
	orig := "# 头\nopenai_base_url = \"" + relayURL + "\"\nmodel_provider = \"relay\"\n\n[model_providers.relay]\nbase_url = \"" + relayURL + "\"\n"
	c, cfg := newFixture(t, sample{content: orig})
	taken, err := Enable(c, fullInput(c))
	if err != nil || len(taken) != 2 {
		t.Fatalf("Enable = %v, %v", taken, err)
	}
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("还原后 = %q", got)
	}
}

func handEdit(t *testing.T, cfg, from, to string) {
	t.Helper()
	text := readFile(t, cfg)
	if !strings.Contains(text, from) {
		t.Fatalf("找不到要手改的内容 %q: %q", from, text)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(text, from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRestore_BlockedByHandEditedItem(t *testing.T) {
	orig := "# 头\nopenai_base_url = \"" + relayURL + "\"\nsandbox = \"read-only\"\n\n[mcp_servers.x]\ncommand = \"foo\"\n"
	c, cfg := newFixture(t, sample{content: orig})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	handEdit(t, cfg, proxyURL, "http://proxy.example.org/v1")

	err := Restore(c)
	requireCode(t, err, CodeRestoreBlocked, []string{"openai_base_url"})

	wantPartial := strings.Replace(orig, relayURL, "http://proxy.example.org/v1", 1)
	if got := readFile(t, cfg); got != wantPartial {
		t.Fatalf("部分还原后 = %q, 期望 %q", got, wantPartial)
	}
	if got := entryKeys(readState(t, c)); !reflect.DeepEqual(got, []string{"openai_base_url"}) {
		t.Fatalf("state 应只保留未还原项: %v", got)
	}

	handEdit(t, cfg, "http://proxy.example.org/v1", proxyURL)
	if err := Restore(c); err != nil {
		t.Fatalf("改回后还原错误: %v", err)
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("最终 = %q, 期望 %q", got, orig)
	}
	if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
		t.Fatalf("access-state.json 应被删除: %v", err)
	}
}

func TestRestore_BlockedWhenAbsentItemReappears(t *testing.T) {
	c, cfg := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	handEdit(t, cfg, "a = 1\n", "a = 1\nmodel_provider = \"relay\"\n")
	requireCode(t, Restore(c), CodeRestoreBlocked, []string{"model_provider"})
	if _, ok := findLine(readFile(t, cfg), "model_provider"); !ok {
		t.Fatal("手加的 model_provider 不应被删")
	}
	if _, ok := findLine(readFile(t, cfg), "openai_base_url"); ok {
		t.Fatal("其余项应照常还原")
	}
}

func TestRestoreAll_BlockedChangesNothing(t *testing.T) {
	orig := "openai_base_url = \"" + relayURL + "\"\nsandbox = \"x\"\n\n[t]\nb = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	handEdit(t, cfg, proxyURL, "http://proxy.example.org/v1")
	beforeCfg := readFile(t, cfg)
	beforeState := readFile(t, statePath(c))

	requireCode(t, RestoreAll(c), CodeRestoreBlocked, []string{"openai_base_url"})

	if got := readFile(t, cfg); got != beforeCfg {
		t.Fatalf("配置文件被改动: %q", got)
	}
	if got := readFile(t, statePath(c)); got != beforeState {
		t.Fatalf("state 被改动: %q", got)
	}
}

func TestRestoreAll_NoMismatchRestoresEverything(t *testing.T) {
	orig := "# c\na = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if err := RestoreAll(c); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("还原后 = %q", got)
	}
	if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
		t.Fatal("access-state.json 应被删除")
	}
}

func TestEnable_MissingConfigCreatedAndEmptyBackup(t *testing.T) {
	c, cfg := newFixture(t, sample{missing: true})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); !strings.Contains(got, proxyURL) {
		t.Fatalf("配置文件应被创建并写入接入项: %q", got)
	}
	if got := readFile(t, cfg+".before-model-bridge"); got != "" {
		t.Fatalf("备份应为空: %q", got)
	}
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatal("还原后配置文件应被删除")
	}
}

func TestEnable_ReplaceFailureKeepsOriginal(t *testing.T) {
	orig := "# c\nopenai_base_url = \"" + relayURL + "\"\n\n[t]\nb = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	boom := errors.New("替换失败")
	var tmpDir, tmpContent string
	c.Replace = func(tmp, dst string) error {
		tmpDir = filepath.Dir(tmp)
		tmpContent = readFile(t, tmp)
		return boom
	}
	_, err := Enable(c, fullInput(c))
	if !errors.Is(err, boom) {
		t.Fatalf("应返回替换步骤的错误, 得到 %v", err)
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("原文件被改动: %q", got)
	}
	if tmpDir != filepath.Dir(cfg) {
		t.Fatalf("临时文件目录 = %q, 期望与配置同目录", tmpDir)
	}
	if !strings.Contains(tmpContent, proxyURL) {
		t.Fatalf("替换前临时文件应已写好: %q", tmpContent)
	}
}

func TestRestore_ReplaceFailureKeepsFile(t *testing.T) {
	c, cfg := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	enabled := readFile(t, cfg)
	boom := errors.New("替换失败")
	c.Replace = func(tmp, dst string) error { return boom }
	if err := Restore(c); !errors.Is(err, boom) {
		t.Fatalf("得到 %v", err)
	}
	if got := readFile(t, cfg); got != enabled {
		t.Fatalf("文件被改动: %q", got)
	}
	if _, err := os.Stat(statePath(c)); err != nil {
		t.Fatalf("还原失败时 state 应保留: %v", err)
	}
}

func TestEnable_ReadOnlyDirKeepsOriginal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("无法用只读目录模拟失败")
	}
	orig := "a = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	dir := filepath.Dir(cfg)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if _, err := Enable(c, fullInput(c)); err == nil {
		t.Fatal("父目录只读时应返回错误")
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("原文件被改动: %q", got)
	}
}

func TestEnable_InvalidTomlReturnsConfigInvalidAndWritesNothing(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"值缺失", "a = \nb = 2\n"},
		{"表头未闭合", "a = 1\n[t\nb = 2\n"},
		{"重复键", "a = 1\na = 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{content: tt.content})
			replaced := false
			c.Replace = func(tmp, dst string) error { replaced = true; return os.Rename(tmp, dst) }
			_, err := Enable(c, fullInput(c))
			requireCode(t, err, CodeConfigInvalid, nil)
			if replaced {
				t.Fatal("不应走到替换步骤")
			}
			if got := readFile(t, cfg); got != tt.content {
				t.Fatalf("原文件被改动: %q", got)
			}
			if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
				t.Fatal("不应写 access-state.json")
			}
		})
	}
}

var rawFormSamples = []sample{
	{name: "被移除的键带行尾注释", content: "a = 1\nmodel_provider = \"relay\" # 中转\nb = 2\n\n[t]\nx = 1\n"},
	{name: "整份文件 CRLF", content: "# c\r\nopenai_base_url = \"" + relayURL + "\"\r\nmodel_provider = \"relay\"\r\nmodel = \"gpt-x\"\r\n\r\n[desktop]\r\ntheme = \"dark\"\r\n\r\n[t]\r\nx = 1\r\n"},
	{name: "多行带注释的数组", content: "[desktop]\nenabled-reasoning-efforts = [\n  \"low\", # l\n  \"medium\",\n] # 尾注\ntheme = \"dark\"\n"},
	{name: "单引号写法", content: "model = 'x'\nopenai_base_url = '" + relayURL + "'\n"},
	{name: "转义写法", content: "model = \"caf\\u00e9\"\nmodel_catalog_json = \"C:\\\\old\\\\c.json\"\n"},
	{name: "只有一行且无结尾换行", content: "model_provider = \"relay\""},
	{name: "移除的键在文件末尾且无结尾换行", content: "a = 1\nmodel_provider = \"relay\""},
}

func TestRestore_RoundTripByteExact_OriginalSpelling(t *testing.T) {
	for _, s := range rawFormSamples {
		for _, auto := range []bool{true, false} {
			t.Run(s.name, func(t *testing.T) {
				c, cfg := newFixture(t, s)
				in := fullInput(c)
				in.HasAutoModel = auto
				if _, err := Enable(c, in); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(s.content, "\r\n") {
					if got := readFile(t, cfg); strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
						t.Fatalf("CRLF 文件里出现了单独的 LF: %q", got)
					}
				}
				if err := Restore(c); err != nil {
					t.Fatalf("Restore 错误: %v", err)
				}
				if got := readFile(t, cfg); got != s.content {
					t.Fatalf("还原后 = %q, 期望 %q", got, s.content)
				}
			})
		}
	}
}

func TestEnable_StateSaveFailureKeepsConfigAndLeavesNoState(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("无法用只读目录模拟失败")
	}
	orig := "a = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	if err := os.Chmod(c.DataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(c.DataDir, 0o755) })

	if _, err := Enable(c, fullInput(c)); err == nil {
		t.Fatal("数据目录只读时应返回错误")
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("配置被改动: %q", got)
	}
	if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
		t.Fatalf("不应留下状态文件: %v", err)
	}
}

func TestEnable_ReplaceFailureRestoresPreviousState(t *testing.T) {
	c, _ := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, statePath(c))
	c.Replace = func(tmp, dst string) error { return errors.New("替换失败") }
	if _, err := Enable(c, fullInput(c)); err == nil {
		t.Fatal("应返回错误")
	}
	if got := readFile(t, statePath(c)); got != before {
		t.Fatalf("状态文件应恢复成旧内容: %q", got)
	}
}

func TestEnable_Twice_HandEditedItemIsTakenOver(t *testing.T) {
	orig := "a = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	handEdit(t, cfg, proxyURL, "http://mine")
	got, err := Enable(c, fullInput(c))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"openai_base_url"}) {
		t.Fatalf("接管键 = %v", got)
	}
	if l, _ := findLine(readFile(t, cfg), "openai_base_url"); !strings.Contains(l, proxyURL) {
		t.Fatalf("第二次开启后应写回代理地址: %q", l)
	}
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if l, ok := findLine(readFile(t, cfg), "openai_base_url"); !ok || !strings.Contains(l, `"http://mine"`) {
		t.Fatalf("openai_base_url 应变回 http://mine: %q", l)
	}
}

func TestEnable_SymlinkedConfigKeepsLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("符号链接需要特权")
	}
	c, cfg := newFixture(t, sample{missing: true})
	target := filepath.Join(t.TempDir(), "real.toml")
	orig := "a = 1\n"
	if err := os.WriteFile(target, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("链接应保持不变: %v %v", fi, err)
	}
	if got := readFile(t, target); !strings.Contains(got, proxyURL) {
		t.Fatalf("目标文件应已改写: %q", got)
	}
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("还原后链接应保持不变: %v %v", fi, err)
	}
	if got := readFile(t, target); got != orig {
		t.Fatalf("还原后 = %q", got)
	}
}

func TestEnable_KeepsFileModeAndLeavesNoTempFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("权限位在 Windows 上无意义")
	}
	c, cfg := newFixture(t, sample{content: "a = 1\n"})
	if err := os.Chmod(cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(cfg); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("权限 = %v, %v", fi, err)
	}
	ents, err := os.ReadDir(filepath.Dir(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if n := e.Name(); n != "config.toml" && n != "config.toml.before-model-bridge" {
			t.Fatalf("多出文件 %s", n)
		}
	}
}

func TestEnable_NewFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("权限位在 Windows 上无意义")
	}
	c, cfg := newFixture(t, sample{missing: true})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(cfg); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("权限 = %v, %v", fi, err)
	}
}

func TestEnable_ConfigChangedDuringWriteReturnsError(t *testing.T) {
	orig := "a = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	changed := orig + "codex = true\n"
	c.beforeReplace = func() {
		if err := os.WriteFile(cfg, []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Enable(c, fullInput(c))
	requireCode(t, err, CodeConfigChanged, nil)
	if got := readFile(t, cfg); got != changed {
		t.Fatalf("不应覆盖别人写入的内容: %q", got)
	}
	if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
		t.Fatalf("不应留下状态文件: %v", err)
	}
}

func TestRestore_ConfigChangedDuringWriteReturnsError(t *testing.T) {
	c, cfg := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	c.beforeReplace = func() {
		f, _ := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("codex = true\n")
		f.Close()
	}
	requireCode(t, Restore(c), CodeConfigChanged, nil)
	if _, err := os.Stat(statePath(c)); err != nil {
		t.Fatalf("状态文件应保留: %v", err)
	}
}

func TestEnable_WrongValueTypeReturnsConfigInvalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
	}{
		{"efforts 是字符串", "[desktop]\nenabled-reasoning-efforts = \"low\"\n", "desktop.enabled-reasoning-efforts"},
		{"openai_base_url 是数字", "openai_base_url = 1\n", "openai_base_url"},
		{"model_provider 是数组", "model_provider = [\"a\"]\n", "model_provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{content: tt.content})
			_, err := Enable(c, fullInput(c))
			requireCode(t, err, CodeConfigInvalid, []string{tt.key})
			if got := readFile(t, cfg); got != tt.content {
				t.Fatalf("原文件被改动: %q", got)
			}
			if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
				t.Fatal("不应写 access-state.json")
			}
		})
	}
}

func TestEnable_DanglingSymlinkConfigKeepsLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("符号链接需要特权")
	}
	tests := []struct {
		name string
		link func(cfg string) (linkTarget, realPath string)
	}{
		{"绝对路径目标", func(cfg string) (string, string) {
			p := filepath.Join(t.TempDir(), "real.toml")
			return p, p
		}},
		{"相对路径目标", func(cfg string) (string, string) {
			return "real.toml", filepath.Join(filepath.Dir(cfg), "real.toml")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{missing: true})
			linkTarget, realPath := tt.link(cfg)
			if err := os.Symlink(linkTarget, cfg); err != nil {
				t.Fatal(err)
			}
			if _, err := Enable(c, fullInput(c)); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("链接应保持不变: %v %v", fi, err)
			}
			if got := readFile(t, realPath); !strings.Contains(got, proxyURL) {
				t.Fatalf("目标文件应被创建并写入接入项: %q", got)
			}
			if err := Restore(c); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("还原后链接应仍然存在: %v %v", fi, err)
			}
			if _, err := os.Stat(realPath); !os.IsNotExist(err) {
				t.Fatalf("原来不存在的目标文件应被删除: %v", err)
			}
		})
	}
}

func TestEnable_SymlinkedConfigBackupNextToLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("符号链接需要特权")
	}
	c, cfg := newFixture(t, sample{missing: true})
	target := filepath.Join(t.TempDir(), "real.toml")
	orig := "a = 1\n"
	if err := os.WriteFile(target, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg+".before-model-bridge"); got != orig {
		t.Fatalf("备份 = %q", got)
	}
	if _, err := os.Stat(target + ".before-model-bridge"); !os.IsNotExist(err) {
		t.Fatalf("链接目标旁不应有备份: %v", err)
	}
}

func TestEnable_AutoModelDroppedOnSecondEnable(t *testing.T) {
	t.Run("当前值是本工具写入的值时还原成原值", func(t *testing.T) {
		c, cfg := newFixture(t, sample{content: "model = 'gpt-x' # 我的\n"})
		if _, err := Enable(c, fullInput(c)); err != nil {
			t.Fatal(err)
		}
		in := fullInput(c)
		in.HasAutoModel = false
		taken, err := Enable(c, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(taken) != 0 {
			t.Fatalf("不应报接管: %v", taken)
		}
		if l, _ := findLine(readFile(t, cfg), "model"); l != "model = 'gpt-x' # 我的" {
			t.Fatalf("model 行 = %q", l)
		}
		if _, ok := readState(t, c).Entries["model"]; ok {
			t.Fatal("state 里应去掉 model")
		}
	})
	t.Run("原来没有 model 时删掉", func(t *testing.T) {
		c, cfg := newFixture(t, sample{content: "a = 1\n"})
		if _, err := Enable(c, fullInput(c)); err != nil {
			t.Fatal(err)
		}
		in := fullInput(c)
		in.HasAutoModel = false
		if _, err := Enable(c, in); err != nil {
			t.Fatal(err)
		}
		if _, ok := findLine(readFile(t, cfg), "model"); ok {
			t.Fatal("model 应被删掉")
		}
		if _, ok := readState(t, c).Entries["model"]; ok {
			t.Fatal("state 里应去掉 model")
		}
	})
	t.Run("用户手改过时保留并计入接管", func(t *testing.T) {
		c, cfg := newFixture(t, sample{content: "model = \"gpt-x\"\n"})
		if _, err := Enable(c, fullInput(c)); err != nil {
			t.Fatal(err)
		}
		handEdit(t, cfg, `model = "auto"`, `model = "mine"`)
		in := fullInput(c)
		in.HasAutoModel = false
		taken, err := Enable(c, in)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(taken, []string{"model"}) {
			t.Fatalf("接管键 = %v", taken)
		}
		if l, _ := findLine(readFile(t, cfg), "model"); l != `model = "mine"` {
			t.Fatalf("model 行 = %q", l)
		}
		if _, ok := readState(t, c).Entries["model"]; ok {
			t.Fatal("state 里应去掉 model")
		}
	})
}

func TestRestore_ConfigChangedBeforeDeleteReturnsError(t *testing.T) {
	c, cfg := newFixture(t, sample{missing: true})
	if _, err := Enable(c, fullInput(c)); err != nil {
		t.Fatal(err)
	}
	hooked := false
	c.beforeReplace = func() {
		hooked = true
		f, _ := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("codex = true\n")
		f.Close()
	}
	requireCode(t, Restore(c), CodeConfigChanged, nil)
	if !hooked {
		t.Fatal("删除前应调用 beforeReplace")
	}
	if got := readFile(t, cfg); !strings.Contains(got, "codex = true") {
		t.Fatalf("文件不应被删除: %q", got)
	}
	if _, err := os.Stat(statePath(c)); err != nil {
		t.Fatalf("状态文件应保留: %v", err)
	}
}

func TestEnable_RollbackFailureIsJoined(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("无法用只读目录模拟失败")
	}
	boom := errors.New("替换失败")
	tests := []struct {
		name  string
		prior bool
	}{
		{"有旧状态时恢复旧状态失败", true},
		{"没有旧状态时删除新状态失败", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFixture(t, sample{content: "a = 1\n"})
			if tt.prior {
				if _, err := Enable(c, fullInput(c)); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { os.Chmod(c.DataDir, 0o755) })
			c.Replace = func(tmp, dst string) error {
				os.Chmod(c.DataDir, 0o500)
				return boom
			}
			_, err := Enable(c, fullInput(c))
			if !errors.Is(err, boom) {
				t.Fatalf("应包含替换错误: %v", err)
			}
			if !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("应包含回滚错误: %v", err)
			}
		})
	}
}

func TestEnable_DesktopNotPlainTableReturnsConfigInvalidBeforeBackup(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"desktop 是字符串", "desktop = \"x\"\n"},
		{"desktop 是行内表", "desktop = { theme = \"dark\" }\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, cfg := newFixture(t, sample{content: tt.content})
			_, err := Enable(c, fullInput(c))
			requireCode(t, err, CodeConfigInvalid, []string{"desktop.enabled-reasoning-efforts"})
			if got := readFile(t, cfg); got != tt.content {
				t.Fatalf("原文件被改动: %q", got)
			}
			if _, err := os.Stat(cfg + ".before-model-bridge"); !os.IsNotExist(err) {
				t.Fatalf("不应写备份: %v", err)
			}
			if _, err := os.Stat(statePath(c)); !os.IsNotExist(err) {
				t.Fatal("不应写 access-state.json")
			}
		})
	}
}
