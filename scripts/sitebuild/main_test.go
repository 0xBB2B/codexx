package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const siteDir = "../../site"

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sitebuild-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "sitebuild")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic("go build 失败: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, args ...string) int {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		t.Fatalf("无法运行命令: %v", err)
		return -1
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func build(t *testing.T, version string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	if code := run(t, siteDir, out, version); code != 0 {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	return readFile(t, filepath.Join(out, "index.html"))
}

func section(t *testing.T, html, id string) string {
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

func downloadURLs(version string) (mac, win string) {
	base := "https://github.com/0xBB2B/codexx/releases/download/v" + version + "/model-bridge-" + version
	return base + "-macos.dmg", base + "-windows-setup.exe"
}

func TestSiteBuild_EmptyVersion_IndexUnchanged(t *testing.T) {
	t.Parallel()
	got := build(t, "")
	if want := readFile(t, siteDir+"/index.html"); got != want {
		t.Error("版本号为空时，输出的 index.html 应与源文件完全一致")
	}
}

func TestSiteBuild_CopiesWholeSourceDir(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(filepath.Join(src, "assets", "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(readFile(t, siteDir+"/index.html")), 0o644); err != nil {
		t.Fatal(err)
	}
	extras := map[string]string{"robots.txt": "a", "assets/img/x.svg": "<svg/>", "assets/b.txt": "b"}
	for name, body := range extras {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	if code := run(t, src, out, "1.2.3"); code != 0 {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	for name, body := range extras {
		if got := readFile(t, filepath.Join(out, name)); got != body {
			t.Errorf("%s 内容 = %q, want %q", name, got, body)
		}
	}
}

func TestSiteBuild_SourceDirUntouched(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := readFile(t, siteDir+"/index.html")
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if code := run(t, src, out, "1.2.3"); code != 0 {
		t.Fatalf("退出码 = %d, 期望 0", code)
	}
	if got := readFile(t, filepath.Join(src, "index.html")); got != orig {
		t.Error("源目录里的 index.html 被改动")
	}
}

func TestSiteBuild_Released_ButtonsAndLinks(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"1.2.3", "10.20.30"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			html := build(t, version)
			mac, win := downloadURLs(version)
			tests := []struct {
				name string
				text string
			}{
				{"中文 macOS 按钮", "下载 macOS 版 v" + version},
				{"中文 Windows 按钮", "下载 Windows 版 v" + version},
				{"英文 macOS 按钮", "Download for macOS v" + version},
				{"英文 Windows 按钮", "Download for Windows v" + version},
				{"macOS 直链", `href="` + mac + `"`},
				{"Windows 直链", `href="` + win + `"`},
			}
			for _, tt := range tests {
				if n := strings.Count(html, tt.text); n != 2 {
					t.Errorf("%s: %q 出现 %d 次，want 2（首屏和下载区各一次）", tt.name, tt.text, n)
				}
			}
			for _, name := range []string{"hero", "download"} {
				sec := section(t, html, name)
				for _, u := range []string{mac, win} {
					if !strings.Contains(sec, `href="`+u+`"`) {
						t.Errorf("区块 %s 缺少直链 %s", name, u)
					}
				}
			}
		})
	}
}

func TestSiteBuild_Released_DownloadNote(t *testing.T) {
	t.Parallel()
	dl := section(t, build(t, "1.2.3"), "download")
	for _, want := range []string{
		"最新正式版是 v1.2.3，点按钮直接下载安装包。",
		"The latest release is v1.2.3. The buttons download the installer directly.",
	} {
		if !strings.Contains(dl, want) {
			t.Errorf("下载区缺少说明 %q", want)
		}
	}
}

func TestSiteBuild_Released_NoUnreleasedState(t *testing.T) {
	t.Parallel()
	html := build(t, "1.2.3")
	for _, banned := range []string{"暂未发布", "Not released yet", "正式版还没有发布", "No release has been published yet"} {
		if strings.Contains(html, banned) {
			t.Errorf("已发布的页面仍含 %q", banned)
		}
	}
	if tag := regexp.MustCompile(`<button\b[^>]*\bdisabled\b[^>]*>`).FindString(html); tag != "" {
		t.Errorf("已发布的页面仍有禁用按钮: %s", tag)
	}
}

func TestSiteBuild_Released_AllReleasesLink(t *testing.T) {
	t.Parallel()
	dl := section(t, build(t, "1.2.3"), "download")
	if !strings.Contains(dl, `href="https://github.com/0xBB2B/codexx/releases"`) {
		t.Error("下载区缺少指向 /releases 的「查看所有版本」链接")
	}
	if !strings.Contains(dl, "查看所有版本") {
		t.Error("下载区缺少「查看所有版本」")
	}
}

func TestSiteBuild_Released_FileNameRowStaysAbsent(t *testing.T) {
	t.Parallel()
	html := build(t, "1.2.3")
	if strings.Contains(html, "文件名") || strings.Contains(html, "File name") {
		t.Error("已发布的页面出现了「文件名」一行")
	}
	if strings.Contains(html, ">model-bridge-") || strings.Contains(html, "<code>model-bridge-") {
		t.Error("页面里把文件名当作文字显示")
	}
}

func TestSiteBuild_InvalidVersion_Rejected(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v1.2.3", "1.2.3-rc.1", "1.2.3-beta", "1.2", "abc", "1.2.3 ", " 1.2.3", "1.2.3+build", "1.2.x"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			out := filepath.Join(t.TempDir(), "out")
			if code := run(t, siteDir, out, version); code == 0 {
				t.Error("退出码 = 0, 期望非 0")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("版本号无效时不应写输出目录: %v", err)
			}
		})
	}
}

func TestSiteBuild_WrongArgCount_Rejected(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "out")
	tests := []struct {
		name string
		args []string
	}{
		{"没有参数", nil},
		{"一个参数", []string{siteDir}},
		{"两个参数", []string{siteDir, out}},
		{"四个参数", []string{siteDir, out, "1.2.3", "0xBB2B/codexx"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if code := run(t, tt.args...); code == 0 {
				t.Error("退出码 = 0, 期望非 0")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("参数个数不对时不应写输出目录: %v", err)
			}
		})
	}
}

func TestSiteBuild_Released_HeroButtonOrder(t *testing.T) {
	t.Parallel()
	hero := section(t, build(t, "1.2.3"), "hero")
	prev := -1
	for _, text := range []string{"下载 macOS 版 v1.2.3", "下载 Windows 版 v1.2.3", "查看源码"} {
		pos := strings.Index(hero, text)
		if pos < 0 {
			t.Fatalf("首屏缺少 %q", text)
		}
		if pos <= prev {
			t.Errorf("首屏 %q 的位置 %d 不在前一个按钮之后 %d", text, pos, prev)
		}
		prev = pos
	}
}
