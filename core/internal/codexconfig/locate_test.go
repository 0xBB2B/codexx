package codexconfig

import (
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "custom-codex")

	tests := []struct {
		name      string
		codexHome string
		want      string
	}{
		{"未设置 CODEX_HOME 时用用户目录下的 .codex", "", filepath.Join(home, ".codex", "config.toml")},
		{"设置 CODEX_HOME 时优先使用", codexHome, filepath.Join(codexHome, "config.toml")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CODEX_HOME", tt.codexHome)

			got, err := ConfigPath()
			if err != nil {
				t.Fatalf("ConfigPath 返回错误: %v", err)
			}
			if got != tt.want {
				t.Fatalf("路径 = %q, 期望 %q", got, tt.want)
			}
		})
	}
}
