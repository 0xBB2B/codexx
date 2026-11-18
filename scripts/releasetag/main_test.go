package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "releasetag-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "releasetag")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic("go build 失败: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "tester")
	git(t, dir, "config", "user.email", "tester@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")
	git(t, dir, "config", "tag.gpgsign", "false")
	git(t, dir, "commit", "--allow-empty", "-m", "first")
	git(t, dir, "commit", "--allow-empty", "-m", "second")
	return dir
}

func run(t *testing.T, dir string, args ...string) (stdout string, exit int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &ee):
		return out.String(), ee.ExitCode()
	default:
		t.Fatalf("无法运行命令: %v", err)
		return "", -1
	}
}

func TestReleaseTag_Success(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		at   string
		want string
	}{
		{name: "正式版", tag: "v1.2.3", at: "HEAD", want: "version=1.2.3\nprerelease=false\n"},
		{name: "多位数字", tag: "v10.20.30", at: "HEAD", want: "version=10.20.30\nprerelease=false\n"},
		{name: "带点的预发布后缀", tag: "v1.2.3-beta.1", at: "HEAD", want: "version=1.2.3-beta.1\nprerelease=true\n"},
		{name: "字母数字后缀", tag: "v0.0.1-rc1", at: "HEAD", want: "version=0.0.1-rc1\nprerelease=true\n"},
		{name: "标签在基线祖先提交上", tag: "v1.2.3", at: "HEAD~1", want: "version=1.2.3\nprerelease=false\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t)
			git(t, dir, "tag", tt.tag, tt.at)
			out, code := run(t, dir, tt.tag, "main")
			if code != 0 {
				t.Fatalf("退出码 = %d, 期望 0", code)
			}
			if out != tt.want {
				t.Errorf("stdout = %q, 期望 %q", out, tt.want)
			}
		})
	}
}

func TestReleaseTag_RemoteBaseRef(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	git(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	git(t, dir, "tag", "v1.2.3")
	out, code := run(t, dir, "v1.2.3", "origin/main")
	if code != 0 || out != "version=1.2.3\nprerelease=false\n" {
		t.Errorf("code=%d stdout=%q", code, out)
	}
}

func TestReleaseTag_BadFormatFails(t *testing.T) {
	tags := []struct{ name, tag string }{
		{"只有两段", "v1.2"},
		{"没有 v", "1.2.3"},
		{"大写 V", "V1.2.3"},
		{"不是版本号", "vtest"},
		{"带构建元数据", "v1.2.3+build5"},
		{"后缀为空", "v1.2.3-"},
		{"后缀含下划线", "v1.2.3-beta_1"},
		{"四段", "v1.2.3.4"},
		{"中间段为空", "v1..3"},
		{"非数字段", "v1.2.a"},
	}
	for _, tt := range tags {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t)
			if tt.tag != "v1..3" { // git 不允许创建含 ".." 的标签
				git(t, dir, "tag", tt.tag)
			}
			out, code := run(t, dir, tt.tag, "main")
			if code == 0 {
				t.Error("退出码 = 0, 期望非 0")
			}
			if out != "" {
				t.Errorf("stdout = %q, 期望空", out)
			}
		})
	}
}

func TestReleaseTag_Failures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		args  []string
	}{
		{
			name: "标签所指提交在其他分支上",
			setup: func(t *testing.T, dir string) {
				git(t, dir, "checkout", "-b", "feature")
				git(t, dir, "commit", "--allow-empty", "-m", "feature work")
				git(t, dir, "tag", "v9.9.9")
				git(t, dir, "checkout", "main")
			},
			args: []string{"v9.9.9", "main"},
		},
		{
			name:  "标签不存在",
			setup: func(t *testing.T, dir string) {},
			args:  []string{"v1.2.3", "main"},
		},
		{
			name:  "基线引用不存在",
			setup: func(t *testing.T, dir string) { git(t, dir, "tag", "v1.2.3") },
			args:  []string{"v1.2.3", "no-such-branch"},
		},
		{name: "0 个参数", setup: func(t *testing.T, dir string) {}, args: nil},
		{
			name:  "1 个参数",
			setup: func(t *testing.T, dir string) { git(t, dir, "tag", "v1.2.3") },
			args:  []string{"v1.2.3"},
		},
		{
			name:  "3 个参数",
			setup: func(t *testing.T, dir string) { git(t, dir, "tag", "v1.2.3") },
			args:  []string{"v1.2.3", "main", "extra"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t)
			tt.setup(t, dir)
			out, code := run(t, dir, tt.args...)
			if code == 0 {
				t.Error("退出码 = 0, 期望非 0")
			}
			if out != "" {
				t.Errorf("stdout = %q, 期望空", out)
			}
		})
	}
}
