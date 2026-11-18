package catalog

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const OfficialTimeout = 30 * time.Second

type Entry map[string]any

func DefaultDarwinRoots(home string) []string {
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

func FindCodexDarwin(roots []string) (string, error) {
	for _, root := range roots {
		p := filepath.Join(root, "ChatGPT.app", "Contents", "Resources", "codex-cli", "bin", "codex")
		if isFile(p) {
			return p, nil
		}
	}
	return "", newErr("chatgpt_not_found", "未找到 ChatGPT 自带的 codex")
}

func FindCodexWindows(installDir string) (string, error) {
	for _, p := range []string{
		filepath.Join(installDir, "resources", "codex-cli", "bin", "codex.exe"),
		filepath.Join(installDir, "resources", "codex.exe"),
	} {
		if isFile(p) {
			return p, nil
		}
	}
	return "", newErr("chatgpt_not_found", "未找到 ChatGPT 自带的 codex")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func LoadOfficial(ctx context.Context, codexPath string) ([]Entry, error) {
	if !isFile(codexPath) {
		return nil, newErr("chatgpt_not_found", "codex 程序不存在")
	}
	ctx, cancel := context.WithTimeout(ctx, OfficialTimeout)
	defer cancel()

	home, err := os.MkdirTemp("", "model-bridge-codex-")
	if err != nil {
		return nil, newErr("chatgpt_unsupported", "无法创建临时目录")
	}
	defer os.RemoveAll(home)

	cmd := exec.CommandContext(ctx, codexPath, "debug", "models", "--bundled")
	cmd.Dir = home
	cmd.Env = officialEnv(home)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return nil, newErr("chatgpt_unsupported", "codex 执行失败")
	}

	var file struct {
		Models []Entry `json:"models"`
	}
	if err := json.Unmarshal(out, &file); err != nil || len(file.Models) == 0 {
		return nil, newErr("chatgpt_unsupported", "codex 输出不是有效的模型清单")
	}
	for _, m := range file.Models {
		if _, ok := m["priority"].(float64); !ok {
			return nil, newErr("chatgpt_unsupported", "官方模型条目缺少 priority")
		}
	}
	return file.Models, nil
}

func officialEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(name, "OPENAI_API_KEY") || strings.EqualFold(name, "CODEX_API_KEY") || strings.EqualFold(name, "CODEX_HOME") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "CODEX_HOME="+home)
}
