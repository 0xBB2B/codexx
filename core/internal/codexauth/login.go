package codexauth

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type Result string

const (
	LoggedIn    Result = "logged_in"
	NotLoggedIn Result = "not_logged_in"
	Failed      Result = "failed"
)

var Timeout = 10 * time.Second

func Detect(ctx context.Context, codexPath string) Result {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, codexPath, "login", "status")
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		return LoggedIn
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return Failed
	}
	text := strings.TrimSpace(string(out))
	// 在 ChatGPT 里退出登录后，Codex 的登录文件会留空，此时报的是这句而不是 Not logged in
	if text == "Not logged in" || strings.HasPrefix(text, "Error checking login status: failed to deserialize CLI auth from file:") {
		return NotLoggedIn
	}
	return Failed
}
