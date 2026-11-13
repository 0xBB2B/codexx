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
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(string(out)) == "Not logged in" {
		return NotLoggedIn
	}
	return Failed
}
