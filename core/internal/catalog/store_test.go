package catalog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

func TestStore_RelayModels_RoundTripPerID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := catalog.NewStore(dir)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for id, ids := range map[string][]string{"1": {"auto", "deepseek-v3.2"}, "2": {"glm-5.3"}} {
		if err := s.SaveRelayModels(id, ids, at); err != nil {
			t.Fatalf("SaveRelayModels(%s): %v", id, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "relay-models-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		FetchedAt string   `json:"fetched_at"`
		IDs       []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339, file.FetchedAt); err != nil {
		t.Errorf("fetched_at 不是 RFC3339: %q", file.FetchedAt)
	}
	if !reflect.DeepEqual(file.IDs, []string{"auto", "deepseek-v3.2"}) {
		t.Errorf("ids = %v", file.IDs)
	}

	for id, want := range map[string][]string{"1": {"auto", "deepseek-v3.2"}, "2": {"glm-5.3"}} {
		ids, fetchedAt, ok, err := s.LoadRelayModels(id)
		if err != nil || !ok || !reflect.DeepEqual(ids, want) || !fetchedAt.Equal(at) {
			t.Errorf("Load(%s) = %v, %v, %v, %v", id, ids, fetchedAt, ok, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "relay-models-2.json")); err != nil {
		t.Errorf("编号 2 的缓存没写出: %v", err)
	}
}

func TestStore_LoadRelayModels_NoRecord(t *testing.T) {
	t.Parallel()
	ids, _, ok, err := catalog.NewStore(t.TempDir()).LoadRelayModels("1")
	if err != nil || ok || len(ids) != 0 {
		t.Errorf("Load = %v, %v, %v，没有记录时应返回 ok=false 且无错误", ids, ok, err)
	}
}

func TestStore_LoadRelayModels_LegacyFile(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	legacy := `{"fetched_at":"2026-01-02T03:04:05Z","ids":["auto"]}`
	tests := []struct {
		name    string
		id      string
		prepare func(dir string) error
		wantOK  bool
		wantIDs []string
	}{
		{"编号 1 没有新文件时读旧文件", "1", func(string) error { return nil }, true, []string{"auto"}},
		{"编号 1 有新文件时新文件优先", "1", func(dir string) error {
			return catalog.NewStore(dir).SaveRelayModels("1", []string{"glm-5.3"}, at)
		}, true, []string{"glm-5.3"}},
		{"其他编号不读旧文件", "2", func(string) error { return nil }, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "relay-models.json"), []byte(legacy), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := tt.prepare(dir); err != nil {
				t.Fatal(err)
			}
			ids, _, ok, err := catalog.NewStore(dir).LoadRelayModels(tt.id)
			if err != nil || ok != tt.wantOK || !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Errorf("Load(%s) = %v, %v, %v", tt.id, ids, ok, err)
			}
		})
	}
}

func TestStore_LoadRelayModels_CorruptFileReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relay-models-1.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := catalog.NewStore(dir).LoadRelayModels("1"); err == nil {
		t.Error("新文件损坏应返回错误")
	}
}

func TestStore_SaveRelayModels_OverwritesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := catalog.NewStore(dir)
	now := time.Now()
	if err := s.SaveRelayModels("1", []string{"a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRelayModels("1", []string{"b"}, now); err != nil {
		t.Fatal(err)
	}
	ids, _, _, _ := s.LoadRelayModels("1")
	if !reflect.DeepEqual(ids, []string{"b"}) {
		t.Errorf("ids = %v", ids)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "relay-models-1.json" {
		t.Errorf("目录里有多余的临时文件: %v", entries)
	}
}

func TestStore_WriteCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := catalog.NewStore(dir)
	models := []catalog.Entry{{"slug": "gpt-a", "priority": 1}, {"slug": "auto", "priority": 2}}
	for i := 0; i < 2; i++ {
		if err := s.WriteCatalog(models); err != nil {
			t.Fatalf("WriteCatalog: %v", err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "model-bridge-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Models) != 2 || file.Models[0]["slug"] != "gpt-a" || file.Models[1]["slug"] != "auto" {
		t.Errorf("models = %v", file.Models)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("目录里有多余的临时文件: %v", entries)
	}
}

func TestStore_WriteFailsWhenDirMissing(t *testing.T) {
	t.Parallel()
	s := catalog.NewStore(filepath.Join(t.TempDir(), "missing"))
	if err := s.WriteCatalog(nil); err == nil {
		t.Error("数据目录不存在时应报错")
	}
}
