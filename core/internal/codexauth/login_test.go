package codexauth_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"modelbridge/internal/codexauth"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	script := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestDetect_ExitCodeAndOutput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want codexauth.Result
	}{
		{"exit 0 即已登录", "echo 'Logged in using ChatGPT'\nexit 0", codexauth.LoggedIn},
		{"exit 0 无输出也是已登录", "exit 0", codexauth.LoggedIn},
		{"stderr 输出 Not logged in 且 exit 1", "echo 'Not logged in' >&2\nexit 1", codexauth.NotLoggedIn},
		{"stdout 输出带尾随换行且 exit 1", "printf 'Not logged in\\n'\nexit 1", codexauth.NotLoggedIn},
		{"首尾空白被去掉", "printf '  Not logged in \\n\\n'\nexit 1", codexauth.NotLoggedIn},
		{"exit 1 但输出是错误信息", "echo 'Error checking login status: boom' >&2\nexit 1", codexauth.Failed},
		{"exit 1 且输出为空", "exit 1", codexauth.Failed},
		{"exit 1 输出只是包含 Not logged in", "echo 'Not logged in to anything else' >&2\nexit 1", codexauth.Failed},
		{"exit 2", "echo 'Not logged in' >&2\nexit 2", codexauth.Failed},
		{"exit 127", "exit 127", codexauth.Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := codexauth.Detect(context.Background(), writeScript(t, tt.body))
			if got != tt.want {
				t.Fatalf("Detect = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetect_MissingPath(t *testing.T) {
	got := codexauth.Detect(context.Background(), filepath.Join(t.TempDir(), "no-such-codex"))
	if got != codexauth.Failed {
		t.Fatalf("Detect = %q, want %q", got, codexauth.Failed)
	}
}

func TestDetect_TimeoutKillsProcess(t *testing.T) {
	script := writeScript(t, "exec sleep 5")
	old := codexauth.Timeout
	codexauth.Timeout = 200 * time.Millisecond
	t.Cleanup(func() { codexauth.Timeout = old })

	start := time.Now()
	got := codexauth.Detect(context.Background(), script)
	elapsed := time.Since(start)

	if got != codexauth.Failed {
		t.Fatalf("Detect = %q, want %q", got, codexauth.Failed)
	}
	if elapsed < 150*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("elapsed = %v, want about Timeout (200ms) and well under 5s", elapsed)
	}
}

func TestTimeout_DefaultIsTenSeconds(t *testing.T) {
	if codexauth.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want 10s", codexauth.Timeout)
	}
}

func TestDetect_ArgsAndEnvInherited(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	envFile := filepath.Join(dir, "env")
	t.Setenv("CODEX_HOME", "/placeholder/codex-home")
	script := writeScript(t,
		"for a in \"$@\"; do echo \"$a\"; done > '"+argsFile+"'\n"+
			"printf '%s' \"$CODEX_HOME\" > '"+envFile+"'\nexit 0")

	if got := codexauth.Detect(context.Background(), script); got != codexauth.LoggedIn {
		t.Fatalf("Detect = %q, want %q", got, codexauth.LoggedIn)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "login\nstatus\n" {
		t.Fatalf("args = %q, want %q", args, "login\nstatus\n")
	}
	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(env) != "/placeholder/codex-home" {
		t.Fatalf("CODEX_HOME = %q, want %q", env, "/placeholder/codex-home")
	}
}

func TestDetect_OutputNotReturned(t *testing.T) {
	script := writeScript(t, "echo 'sk-SECRET-MARKER'\necho 'sk-SECRET-MARKER' >&2\nexit 0")
	got := codexauth.Detect(context.Background(), script)
	var r codexauth.Result = got
	switch r {
	case codexauth.LoggedIn, codexauth.NotLoggedIn, codexauth.Failed:
	default:
		t.Fatalf("Detect = %q, not one of the three results", r)
	}
	if r != codexauth.LoggedIn {
		t.Fatalf("Detect = %q, want %q", r, codexauth.LoggedIn)
	}
}

func TestResultConstants(t *testing.T) {
	if codexauth.LoggedIn != "logged_in" || codexauth.NotLoggedIn != "not_logged_in" || codexauth.Failed != "failed" {
		t.Fatalf("constants = %q %q %q", codexauth.LoggedIn, codexauth.NotLoggedIn, codexauth.Failed)
	}
}
