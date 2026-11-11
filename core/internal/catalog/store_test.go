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

func TestStore_RelayModels_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := catalog.NewStore(dir)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := s.SaveRelayModels([]string{"auto", "deepseek-v3.2"}, at); err != nil {
		t.Fatalf("SaveRelayModels: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "relay-models.json"))
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

	ids, fetchedAt, ok, err := s.LoadRelayModels()
	if !fetchedAt.Equal(at) {
		t.Errorf("fetchedAt = %v, want %v", fetchedAt, at)
	}
	if err != nil || !ok || !reflect.DeepEqual(ids, []string{"auto", "deepseek-v3.2"}) {
		t.Errorf("Load = %v, %v, %v", ids, ok, err)
	}
}

func TestStore_LoadRelayModels_NoRecord(t *testing.T) {
	t.Parallel()
	ids, _, ok, err := catalog.NewStore(t.TempDir()).LoadRelayModels()
	if err != nil || ok || len(ids) != 0 {
		t.Errorf("Load = %v, %v, %v，没有记录时应返回 ok=false 且无错误", ids, ok, err)
	}
}

func TestStore_SaveRelayModels_OverwritesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := catalog.NewStore(dir)
	now := time.Now()
	if err := s.SaveRelayModels([]string{"a"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRelayModels([]string{"b"}, now); err != nil {
		t.Fatal(err)
	}
	ids, _, _, _ := s.LoadRelayModels()
	if !reflect.DeepEqual(ids, []string{"b"}) {
		t.Errorf("ids = %v", ids)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "relay-models.json" {
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
