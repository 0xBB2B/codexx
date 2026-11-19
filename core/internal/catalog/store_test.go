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

const fullAPIModel = `{"id":"model-a","context_length":1048576,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low","high","max"]}}`

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(readFileT(t, path)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStore_CacheFileHoldsModelsAndParamsAfterFetch(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a := newRelayStub(t, 200, modelsBody(fullAPIModel, `{"id":"auto"}`, `{"id":"m-empty","reasoning":{"supported_efforts":[]}}`))
	b := idStub(t, "glm-5.3")
	dataDir := t.TempDir()
	oldFormat := `{"fetched_at":"2026-01-02T03:04:05Z","ids":["stale-model"]}`
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models-1.json"), []byte(oldFormat), 0o644); err != nil {
		t.Fatal(err)
	}
	build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL)))

	file := readJSONMap(t, filepath.Join(dataDir, "relay-models-1.json"))
	at, _ := file["fetched_at"].(string)
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Errorf("fetched_at 不是 RFC3339: %q", at)
	}
	if _, ok := file["ids"]; ok {
		t.Errorf("新格式不应再有 ids: %v", file)
	}
	want := []any{
		map[string]any{"id": "model-a", "context_length": float64(1048576), "input_modalities": []any{"text"}, "reasoning_efforts": []any{"low", "high", "max"}},
		map[string]any{"id": "auto", "context_length": float64(0), "input_modalities": nil, "reasoning_efforts": nil},
		map[string]any{"id": "m-empty", "context_length": float64(0), "input_modalities": nil, "reasoning_efforts": []any{}},
	}
	if !reflect.DeepEqual(file["models"], want) {
		t.Errorf("models = %#v, want %#v", file["models"], want)
	}
	if file2 := readJSONMap(t, filepath.Join(dataDir, "relay-models-2.json")); len(file2["models"].([]any)) != 1 {
		t.Errorf("编号 2 的缓存 = %v", file2)
	}
}

func TestStore_FetchFailsAfterSuccess_UsesCachedParams(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, sameNameOfficialJSON, 0)
	srv := newRelayStub(t, 200, modelsBody(
		fullAPIModel,
		`{"id":"gpt-x","context_length":1050000,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low","high"]}}`,
		`{"id":"auto","context_length":500}`,
		`{"id":"m-empty","context_length":2000,"reasoning":{"supported_efforts":[]}}`))
	dataDir := t.TempDir()
	first := build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
	cacheBefore := readFileT(t, filepath.Join(dataDir, "relay-models-1.json"))
	srv.status.Store(500)

	second := build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))

	if r := relayResult(t, second, "1"); !r.Stale || catalog.ErrorCode(r.Err) != "relay_5xx" {
		t.Errorf("配置 1 = %+v", r)
	}
	if !reflect.DeepEqual(second.Models, first.Models) {
		t.Errorf("获取失败后的目录应和成功那次一致（含接口参数）:\n got  %v\n want %v", roundtrip(t, second.Models), roundtrip(t, first.Models))
	}
	if !reflect.DeepEqual(second.Routes, first.Routes) || !reflect.DeepEqual(second.ReasoningLevels, first.ReasoningLevels) {
		t.Errorf("Routes/ReasoningLevels 与成功那次不一致: %+v %v", second.Routes, second.ReasoningLevels)
	}
	e := bySlug(t, roundtrip(t, second.Models), "relay-gpt-x")
	if e["context_window"] != float64(1050000) {
		t.Errorf("缓存的接口参数没用上: context_window = %v", e["context_window"])
	}
	if got := readFileT(t, filepath.Join(dataDir, "relay-models-1.json")); got != cacheBefore {
		t.Errorf("获取失败时不应覆盖缓存")
	}
}

func TestStore_NewCache_KeepsMissingAndEmptyApart(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, sameNameOfficialJSON, 0)
	dataDir := t.TempDir()
	cache := `{"fetched_at":"2026-01-02T03:04:05Z","models":[
{"id":"gpt-x","context_length":0,"input_modalities":null,"reasoning_efforts":null},
{"id":"gpt-h","context_length":0,"input_modalities":null,"reasoning_efforts":[]},
{"id":"model-a","context_length":1048576,"input_modalities":["text"],"reasoning_efforts":["low","high","max"]}]}`
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models-1.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	res := build(t, buildOpts(t, codex, dataDir, sourceMode("1", "主力", "", catalog.CacheOnly, nil)))
	models := roundtrip(t, res.Models)

	assertEntry(t, bySlug(t, models, "relay-gpt-x"), entryWant{400000, []any{"text", "image"}, []any{"low", "medium", "xhigh"}, "medium", nil, false})
	assertEntry(t, bySlug(t, models, "relay-gpt-h"), entryWant{200000, []any{"text"}, nil, nil, false, false})
	assertEntry(t, bySlug(t, models, "relay-model-a"), entryWant{1048576, []any{"text"}, []any{"low", "high", "ultra"}, "high", true, true})
	if !res.Routes["relay-model-a"].UltraToMax || res.Routes["relay-gpt-x"].UltraToMax {
		t.Errorf("Routes = %+v", res.Routes)
	}
	r := relayResult(t, res, "1")
	if want, _ := time.Parse(time.RFC3339, "2026-01-02T03:04:05Z"); r.Err != nil || !r.FetchedAt.Equal(want) {
		t.Errorf("配置 1 = %+v", r)
	}
}

func TestStore_OldFormatCache_AllModelsKeptWithAllParamsMissing(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, sameNameOfficialJSON, 0)
	failing := newRelayStub(t, 500, "")
	tests := []struct {
		name string
		file string
	}{
		{"编号 1 的新文件名里是旧格式", "relay-models-1.json"},
		{"编号 1 的旧版单文件", "relay-models.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dataDir := t.TempDir()
			old := `{"fetched_at":"2026-01-02T03:04:05Z","ids":["model-a","gpt-x","auto"]}`
			if err := os.WriteFile(filepath.Join(dataDir, tt.file), []byte(old), 0o644); err != nil {
				t.Fatal(err)
			}

			res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", failing.URL)))
			models := roundtrip(t, res.Models)

			if got, want := slugsOf(models[4:]), []any{"relay-model-a", "relay-gpt-x", "relay-auto"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("中转条目 = %v, want %v", got, want)
			}
			if r := relayResult(t, res, "1"); !r.Stale || r.ModelCount != 3 {
				t.Errorf("配置 1 = %+v", r)
			}
			assertEntry(t, bySlug(t, models, "relay-model-a"), entryWant{400000, []any{"text", "image"}, nil, nil, false, false})
			assertEntry(t, bySlug(t, models, "relay-gpt-x"), entryWant{400000, []any{"text", "image"}, []any{"low", "medium", "xhigh"}, "medium", nil, false})
			assertEntry(t, bySlug(t, models, "relay-auto"), entryWant{1000000, []any{"text", "image"}, nil, nil, false, false})
		})
	}
}

func TestStore_RepeatedFetchLeavesNoTempFiles(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := idStub(t, "a")
	dataDir := t.TempDir()
	for range 2 {
		build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
	}
	entries, _ := os.ReadDir(dataDir)
	if len(entries) != 2 {
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
