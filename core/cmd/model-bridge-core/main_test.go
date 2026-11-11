package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	tests := []struct {
		name      string
		ldflags   string
		wantPrint string
	}{
		{name: "未注入版本号打印开发版本", ldflags: "", wantPrint: "0.0.0-dev"},
		{name: "注入版本号后打印注入值", ldflags: "-X modelbridge/internal/version.value=1.3.0", wantPrint: "1.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "model-bridge-core")
			args := []string{"build", "-o", bin}
			if tt.ldflags != "" {
				args = append(args, "-ldflags", tt.ldflags)
			}
			args = append(args, ".")
			if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
				t.Fatalf("go build 失败: %v\n%s", err, out)
			}

			out, err := exec.Command(bin, "--version").Output()
			if err != nil {
				t.Fatalf("运行 --version 失败: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != tt.wantPrint {
				t.Errorf("--version 输出 = %q, want %q", got, tt.wantPrint)
			}
		})
	}
}
