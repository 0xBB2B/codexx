package control_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"modelbridge/internal/control"
)

func TestLoadSettings(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    bool
	}{
		{"文件不存在时转发关闭", nil, false},
		{"forwarding 为 true", ptr(`{"forwarding":true}`), true},
		{"forwarding 为 false", ptr(`{"forwarding":false}`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != nil {
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(*tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := control.LoadSettings(dir)
			if err != nil {
				t.Fatalf("LoadSettings: %v", err)
			}
			if got.Forwarding != tt.want {
				t.Errorf("Forwarding = %v, want %v", got.Forwarding, tt.want)
			}
		})
	}
}

func TestSaveSettings(t *testing.T) {
	for _, want := range []bool{true, false} {
		dir := t.TempDir()
		if err := control.SaveSettings(dir, control.Settings{Forwarding: want}); err != nil {
			t.Fatalf("SaveSettings: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var onDisk map[string]any
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("settings.json 不是合法 JSON: %v", err)
		}
		if onDisk["forwarding"] != want {
			t.Errorf("磁盘上 forwarding = %v, want %v", onDisk["forwarding"], want)
		}
		got, err := control.LoadSettings(dir)
		if err != nil || got.Forwarding != want {
			t.Errorf("读回 = %v, %v, want %v", got, err, want)
		}
	}
}

func TestLoadSettings_CorruptFileMeansForwardingOff(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"forwarding":tr`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := control.LoadSettings(dir)
	if err != nil || got.Forwarding {
		t.Errorf("LoadSettings = %+v, %v, want 转发关闭且不报错", got, err)
	}
}

func TestSaveSettings_ReplacesFileAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skip("不支持符号链接")
	}
	if err := control.SaveSettings(dir, control.Settings{Forwarding: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "orig" {
		t.Errorf("临时文件改名应整体替换, 不该写穿链接: %q", b)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("settings.json 应是普通文件: %v %v", fi, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("不该留下临时文件: %v", entries)
	}
}
