package catalog_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

const officialJSON = `{"models":[{"slug":"gpt-a","priority":3,"display_name":"A","base_instructions":"x"},{"slug":"gpt-b","priority":7,"display_name":"B"}]}`

func writeFakeCodex(t *testing.T, stdout string, exitCode int) (script, record string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	if err := os.MkdirAll(record, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(out, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\n" +
		"env > '" + record + "/env.txt'\n" +
		"printf '%s' \"$CODEX_HOME\" > '" + record + "/home.txt'\n" +
		"ls -A \"$CODEX_HOME\" > '" + record + "/home_ls.txt'\n" +
		"pwd -P > '" + record + "/cwd.txt'\n" +
		"(cd \"$CODEX_HOME\" && pwd -P) > '" + record + "/home_resolved.txt'\n" +
		"echo \"$@\" > '" + record + "/args.txt'\n" +
		"cat '" + out + "'\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	script = filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, record
}

func readRecord(t *testing.T, record, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(record, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadOfficial_ParsesModelsInOrder(t *testing.T) {
	script, record := writeFakeCodex(t, officialJSON, 0)
	models, err := catalog.LoadOfficial(context.Background(), script)
	if err != nil {
		t.Fatalf("LoadOfficial: %v", err)
	}
	if len(models) != 2 || models[0]["slug"] != "gpt-a" || models[1]["slug"] != "gpt-b" {
		t.Fatalf("models = %v", models)
	}
	if models[0]["base_instructions"] != "x" {
		t.Errorf("其他字段丢失: %v", models[0])
	}
	if got := strings.TrimSpace(readRecord(t, record, "args.txt")); got != "debug models --bundled" {
		t.Errorf("args = %q", got)
	}
}

func TestLoadOfficial_IsolatedEnvironment(t *testing.T) {
	origHome := t.TempDir()
	auth := filepath.Join(origHome, "auth.json")
	if err := os.WriteFile(auth, []byte(`{"token":"placeholder"}`), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", origHome)
	t.Setenv("OPENAI_API_KEY", "sk-example-0000")
	t.Setenv("CODEX_API_KEY", "sk-example-1111")

	script, record := writeFakeCodex(t, officialJSON, 0)
	if _, err := catalog.LoadOfficial(context.Background(), script); err != nil {
		t.Fatalf("LoadOfficial: %v", err)
	}

	env := readRecord(t, record, "env.txt")
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, "OPENAI_API_KEY=") || strings.HasPrefix(line, "CODEX_API_KEY=") {
			t.Errorf("环境变量泄漏: %s", strings.SplitN(line, "=", 2)[0])
		}
	}
	home := readRecord(t, record, "home.txt")
	if home == "" || home == origHome {
		t.Fatalf("CODEX_HOME = %q，应为新的临时目录", home)
	}
	if ls := strings.TrimSpace(readRecord(t, record, "home_ls.txt")); ls != "" {
		t.Errorf("CODEX_HOME 不是空目录: %q", ls)
	}
	wantCwd := strings.TrimSpace(readRecord(t, record, "home_resolved.txt"))
	if cwd := strings.TrimSpace(readRecord(t, record, "cwd.txt")); cwd != wantCwd {
		t.Errorf("cwd = %q, want %q", cwd, wantCwd)
	}
	if strings.Contains(env, "auth.json") {
		t.Errorf("环境里出现 auth.json")
	}
}

func TestLoadOfficial_Errors(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		exit     int
		wantCode string
	}{
		{"退出码非零", officialJSON, 1, "chatgpt_unsupported"},
		{"输出不是 JSON", "not json", 0, "chatgpt_unsupported"},
		{"没有 models 字段", `{"other":1}`, 0, "chatgpt_unsupported"},
		{"models 为空", `{"models":[]}`, 0, "chatgpt_unsupported"},
		{"条目缺 priority", `{"models":[{"slug":"a"}]}`, 0, "chatgpt_unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, _ := writeFakeCodex(t, tt.stdout, tt.exit)
			_, err := catalog.LoadOfficial(context.Background(), script)
			if got := catalog.ErrorCode(err); got != tt.wantCode {
				t.Fatalf("code = %q (err=%v), want %q", got, err, tt.wantCode)
			}
		})
	}
}

func TestLoadOfficial_NotFound(t *testing.T) {
	t.Parallel()
	_, err := catalog.LoadOfficial(context.Background(), filepath.Join(t.TempDir(), "no-such-codex"))
	if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
		t.Fatalf("code = %q (err=%v)", got, err)
	}
}

func TestLoadOfficial_ContextCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	script := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := catalog.LoadOfficial(ctx, script)
	if got := catalog.ErrorCode(err); got != "chatgpt_unsupported" {
		t.Fatalf("code = %q (err=%v)", got, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("超时没有生效")
	}
}

func TestOfficialTimeout_Is30Seconds(t *testing.T) {
	t.Parallel()
	if catalog.OfficialTimeout != 30*time.Second {
		t.Errorf("OfficialTimeout = %v", catalog.OfficialTimeout)
	}
}

func mkExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDarwinRoots(t *testing.T) {
	t.Parallel()
	got := catalog.DefaultDarwinRoots("/Users/example")
	want := []string{"/Applications", "/Users/example/Applications"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("roots = %v, want %v", got, want)
	}
}

func TestFindCodexDarwin(t *testing.T) {
	t.Parallel()
	const rel = "ChatGPT.app/Contents/Resources/codex-cli/bin/codex"
	t.Run("按顺序取第一个存在的", func(t *testing.T) {
		t.Parallel()
		a, b := t.TempDir(), t.TempDir()
		mkExec(t, filepath.Join(a, rel))
		mkExec(t, filepath.Join(b, rel))
		got, err := catalog.FindCodexDarwin([]string{a, b})
		if err != nil || got != filepath.Join(a, rel) {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("第一个没有则找第二个", func(t *testing.T) {
		t.Parallel()
		a, b := t.TempDir(), t.TempDir()
		mkExec(t, filepath.Join(b, rel))
		got, err := catalog.FindCodexDarwin([]string{a, b})
		if err != nil || got != filepath.Join(b, rel) {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("都没有", func(t *testing.T) {
		t.Parallel()
		_, err := catalog.FindCodexDarwin([]string{t.TempDir()})
		if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
			t.Fatalf("code = %q", got)
		}
	})
}

func TestFindCodexWindows(t *testing.T) {
	t.Parallel()
	t.Run("优先 codex-cli/bin", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		want := filepath.Join(dir, "resources", "codex-cli", "bin", "codex.exe")
		mkExec(t, want)
		mkExec(t, filepath.Join(dir, "resources", "codex.exe"))
		got, err := catalog.FindCodexWindows(dir)
		if err != nil || got != want {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("退而求 resources/codex.exe", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		want := filepath.Join(dir, "resources", "codex.exe")
		mkExec(t, want)
		got, err := catalog.FindCodexWindows(dir)
		if err != nil || got != want {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("都没有", func(t *testing.T) {
		t.Parallel()
		_, err := catalog.FindCodexWindows(t.TempDir())
		if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
			t.Fatalf("code = %q", got)
		}
	})
}

func TestErrorCode_NonCodedError(t *testing.T) {
	t.Parallel()
	if got := catalog.ErrorCode(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte("x"), &m); catalog.ErrorCode(err) != "" {
		t.Errorf("普通错误应返回空串")
	}
}

func TestOfficialEnv_DropsKeyVarsCaseInsensitively(t *testing.T) {
	t.Setenv("openai_api_key", "x")
	t.Setenv("Codex_Api_Key", "x")
	t.Setenv("codex_home", "/orig")
	t.Setenv("KEEP_ME", "1")
	env := catalog.OfficialEnv("/new-home")
	var keep, home bool
	for _, kv := range env {
		name, val, _ := strings.Cut(kv, "=")
		switch {
		case strings.EqualFold(name, "OPENAI_API_KEY"), strings.EqualFold(name, "CODEX_API_KEY"):
			t.Errorf("环境变量未去掉: %s", name)
		case strings.EqualFold(name, "CODEX_HOME"):
			if val != "/new-home" {
				t.Errorf("%s = %q", name, val)
			}
			home = true
		case name == "KEEP_ME":
			keep = true
		}
	}
	if !keep || !home {
		t.Errorf("keep=%v home=%v", keep, home)
	}
}
