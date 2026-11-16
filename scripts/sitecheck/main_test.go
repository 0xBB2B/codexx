package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sitecheck-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "sitecheck")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic("go build 失败: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runCLI(t *testing.T, args ...string) (stdout string, exit int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
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

func TestMain_CleanDirectoryExitsZero(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"index.html": "https://relay.example.com/v1 sk-example-0000"})
	out, code := runCLI(t, root)
	if code != 0 {
		t.Errorf("退出码 = %d, 期望 0", code)
	}
	if !strings.Contains(out, "检查通过") {
		t.Errorf("stdout 缺少“检查通过”: %q", out)
	}
}

func TestMain_ViolationsExitOne(t *testing.T) {
	t.Parallel()
	root := writeSite(t, map[string]string{"x.html": "https://leak.example.org/secret\n"})
	out, code := runCLI(t, root)
	if code != 1 {
		t.Errorf("退出码 = %d, 期望 1", code)
	}
	if !strings.Contains(out, "x.html:1: 网址不在白名单内") {
		t.Errorf("stdout 缺少违规行: %q", out)
	}
	for _, s := range []string{"leak.example.org", "secret"} {
		if strings.Contains(out, s) {
			t.Errorf("stdout 含违规原文 %q: %q", s, out)
		}
	}
}

func TestMain_MissingDirectoryExitsTwo(t *testing.T) {
	t.Parallel()
	_, code := runCLI(t, filepath.Join(t.TempDir(), "不存在"))
	if code != 2 {
		t.Errorf("退出码 = %d, 期望 2", code)
	}
}

func TestMain_NoArgsExitsTwo(t *testing.T) {
	t.Parallel()
	_, code := runCLI(t)
	if code != 2 {
		t.Errorf("退出码 = %d, 期望 2", code)
	}
}
