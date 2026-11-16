package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	reasonURL = "网址不在白名单内"
	reasonKey = "Key 不是示例 Key"
)

func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheck_SingleFile(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []Violation
	}{
		{"白名单内容全部通过", "https://github.com/0xBB2B/codexx\nhttps://github.com/0xBB2B/codexx/releases\nhttps://relay.example.com/v1\nhttp://127.0.0.1:47311\nsk-example-0000\n", nil},
		{"主机大小写不同也通过", "HTTPS://GitHub.com/0xBB2B/codexx", nil},
		{"github 仓库带问号", "https://github.com/0xBB2B/codexx?tab=readme", nil},
		{"github 仓库带井号", "https://github.com/0xBB2B/codexx#install", nil},
		{"github 主机带端口", "https://github.com:443/0xBB2B/codexx", nil},
		{"127.0.0.1 任意路径", "http://127.0.0.1/a/b?c=d", nil},
		{"relay 带端口和路径", "https://relay.example.com:8443/any/path", nil},
		{"仓库根带斜杠", "https://github.com/0xBB2B/codexx/", nil},
		{"仓库下文件路径", "https://github.com/0xBB2B/codexx/blob/main/README.md", nil},
		{"仓库下 releases/latest", "https://github.com/0xBB2B/codexx/releases/latest", nil},
		{"仓库斜杠后带查询", "https://github.com/0xBB2B/codexx/?a", nil},
		{"仓库名前缀相同且带子路径", "https://github.com/0xBB2B/codexx-fork/x", []Violation{{"index.html", 1, reasonURL}}},
		{"网址后紧跟中文句号", "访问 https://relay.example.com。", nil},
		{"网址在括号内后接句点", "(see https://relay.example.com).", nil},
		{"markdown 链接后接句点", "[x](https://github.com/0xBB2B/codexx).", nil},
		{"网址后紧跟中文逗号", "https://github.com/0xBB2B/codexx，然后", nil},
		{"网址后紧跟分号", "https://relay.example.com/v1;", nil},
		{"括号内违规网址后接句点仍违规", "(https://other.example.org).", []Violation{{"index.html", 1, reasonURL}}},
		{"主机含非 ASCII 字母", "https://ëvil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"主机为中文", "https://中文.com", []Violation{{"index.html", 1, reasonURL}}},
		{"白名单主机后接中文句号再接域名", `<a href="https://relay.example.com。evil.org/">`, []Violation{{"index.html", 1, reasonURL}}},
		{"全角句点拼接域名", "https://relay.example.com．evil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"半角日文句点拼接域名", "https://relay.example.com｡evil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"非标点中文后接 @ 绕过", "https://relay.example.com中@evil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"127.0.0.1 后接带音标字母绕过", "https://127.0.0.1é@evil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"路径含 .. 穿越", "https://github.com/0xBB2B/codexx/../../evil", []Violation{{"index.html", 1, reasonURL}}},
		{"路径含编码的 .. 穿越", "https://github.com/0xBB2B/codexx/%2e%2e/%2e%2e/evil", []Violation{{"index.html", 1, reasonURL}}},
		{"路径含 . 段", "https://github.com/0xBB2B/codexx/./x", []Violation{{"index.html", 1, reasonURL}}},
		{"中文句号结尾且前有中文", "访问 https://relay.example.com。", nil},
		{"中文逗号后接中文", "https://github.com/0xBB2B/codexx，然后", nil},
		{"全角右括号后接中文句号", "见 https://relay.example.com/v1）。", nil},
		{"顿号分隔两个白名单网址", "https://relay.example.com、https://github.com/0xBB2B/codexx", nil},
		{"127.0.0.1 路径含 .. 段不限", "http://127.0.0.1/a/../b", nil},
		{"relay 路径含 . 段不限", "https://relay.example.com/a/./b", nil},
		{"其他主机违规且行号正确", "a\nb\nsee https://other.example.org/ here", []Violation{{"index.html", 3, reasonURL}}},
		{"github 其他用户", "https://github.com/someone/else", []Violation{{"index.html", 1, reasonURL}}},
		{"github 仓库名前缀相同", "https://github.com/0xBB2B/codexx-fork", []Violation{{"index.html", 1, reasonURL}}},
		{"github 路径大小写不同", "https://github.com/0xbb2b/codexx", []Violation{{"index.html", 1, reasonURL}}},
		{"github 根路径", "https://github.com/", []Violation{{"index.html", 1, reasonURL}}},
		{"w3.org 命名空间违规", `<svg xmlns="http://www.w3.org/2000/svg">`, []Violation{{"index.html", 1, reasonURL}}},
		{"主机名带白名单后缀的伪装域名", "https://relay.example.com.evil.org/", []Violation{{"index.html", 1, reasonURL}}},
		{"非示例 Key", "sk-abc123", []Violation{{"index.html", 1, reasonKey}}},
		{"示例 Key 后多一位", "sk-example-00001", []Violation{{"index.html", 1, reasonKey}}},
		{"sk- 前是字母不算 Key", "task-list", nil},
		{"sk- 前是数字不算 Key", "disk-0 and 9sk-abc", nil},
		{"sk- 后无字符不算 Key", "sk- and sk-", nil},
		{"sk- 前是标点算 Key", `"sk-abc"`, []Violation{{"index.html", 1, reasonKey}}},
		{"行首 Key", "sk-abc", []Violation{{"index.html", 1, reasonKey}}},
		{"空文件", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writeSite(t, map[string]string{"index.html": tt.body})
			got, err := Check(root)
			if err != nil {
				t.Fatalf("Check 返回错误: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("违规数 = %d, 期望 %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("第 %d 条 = %+v, 期望 %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCheck_SameLineMultipleViolations(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"a.html": "ok\nhttps://other.example.org/ sk-abc123\n"})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("违规数 = %d, 期望 2: %+v", len(got), got)
	}
	reasons := map[string]bool{}
	for _, v := range got {
		if v.File != "a.html" || v.Line != 2 {
			t.Errorf("位置错误: %+v", v)
		}
		reasons[v.Reason] = true
	}
	if !reasons[reasonURL] || !reasons[reasonKey] {
		t.Errorf("原因不全: %+v", got)
	}
}

func TestCheck_SameLineTwoURLs(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"a.html": "https://a.example.org/ https://b.example.org/"})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("违规数 = %d, 期望 2: %+v", len(got), got)
	}
}

func TestCheck_SubdirectoryPath(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"sub/x.html": "a\nb\nhttps://other.example.org/\n"})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Violation{{"sub/x.html", 3, reasonURL}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCheck_SortedByFileThenLine(t *testing.T) {
	t.Parallel()
	bad := "https://other.example.org/"
	root := writeSite(t, map[string]string{
		"b.html":     bad,
		"a.html":     "x\n" + bad + "\n" + bad,
		"sub/c.html": bad,
		"a2.html":    bad,
	})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Violation{
		{"a.html", 2, reasonURL},
		{"a.html", 3, reasonURL},
		{"a2.html", 1, reasonURL},
		{"b.html", 1, reasonURL},
		{"sub/c.html", 1, reasonURL},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCheck_BinaryFileSkipped(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{
		"logo.bin": "\x00https://other.example.org/ sk-abc123",
		"ok.html":  "fine",
	})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("二进制文件应被跳过, got %+v", got)
	}
}

func TestCheck_NonHTMLTextFilesChecked(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"app.js": "fetch('https://other.example.org/')"})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "app.js" {
		t.Errorf("got %+v", got)
	}
}

func TestCheck_ReasonOmitsOffendingText(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"a.html": "https://leak.example.org/secret sk-leakvalue99"})
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("违规数 = %d, 期望 2", len(got))
	}
	for _, v := range got {
		for _, s := range []string{"leak.example.org", "secret", "sk-leakvalue99", "leakvalue"} {
			if strings.Contains(v.Reason, s) {
				t.Errorf("Reason 含违规原文 %q: %q", s, v.Reason)
			}
		}
	}
}

func TestCheck_MissingDirectory(t *testing.T) {
	t.Parallel()
	_, err := Check(filepath.Join(t.TempDir(), "不存在"))
	if err == nil {
		t.Error("目录不存在应返回 error")
	}
}
