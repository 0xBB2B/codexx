package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

type relayStub struct {
	*httptest.Server
	status atomic.Int32
	hits   atomic.Int32
}

func newRelayStub(t *testing.T, status int, body string) *relayStub {
	t.Helper()
	s := &relayStub{}
	s.status.Store(int32(status))
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func source(id, name, url string) catalog.RelaySource {
	return sourceMode(id, name, url, catalog.FetchRemote, nil)
}

func sourceMode(id, name, url string, mode catalog.RelayMode, models []catalog.RelayModel) catalog.RelaySource {
	return catalog.RelaySource{ID: id, Name: name, BaseURL: url, Key: relayKey, Mode: mode, Models: models}
}

func modelsBody(items ...string) string {
	return `{"data":[` + strings.Join(items, ",") + `]}`
}

func idItems(ids ...string) []string {
	items := make([]string, len(ids))
	for i, id := range ids {
		items[i] = `{"id":"` + id + `"}`
	}
	return items
}

func idStub(t *testing.T, ids ...string) *relayStub {
	t.Helper()
	return newRelayStub(t, 200, modelsBody(idItems(ids...)...))
}

func idSource(t *testing.T, id, name string, ids ...string) catalog.RelaySource {
	t.Helper()
	return source(id, name, idStub(t, ids...).URL)
}

func buildOne(t *testing.T, officialStdout string, items ...string) (*catalog.Result, []map[string]any) {
	t.Helper()
	codex, _ := writeFakeCodex(t, officialStdout, 0)
	srv := newRelayStub(t, 200, modelsBody(items...))
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", srv.URL)))
	return res, roundtrip(t, res.Models)
}

const relayKey = "sk-example-0000"

func buildOpts(t *testing.T, codex, dataDir string, relays ...catalog.RelaySource) catalog.BuildOptions {
	t.Helper()
	return catalog.BuildOptions{
		CodexPath:    codex,
		Relays:       relays,
		RelayTimeout: time.Second,
		DataDir:      dataDir,
	}
}

func build(t *testing.T, opts catalog.BuildOptions) *catalog.Result {
	t.Helper()
	res, err := catalog.Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return res
}

func slugs(t *testing.T, res *catalog.Result) []any {
	t.Helper()
	return slugsOf(roundtrip(t, res.Models))
}

func slugsOf(models []map[string]any) []any {
	var out []any
	for _, m := range models {
		out = append(out, m["slug"])
	}
	return out
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func catalogFileModels(t *testing.T, dataDir string) []map[string]any {
	t.Helper()
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFileT(t, filepath.Join(dataDir, "model-bridge-catalog.json"))), &file); err != nil {
		t.Fatal(err)
	}
	return file.Models
}

func bySlug(t *testing.T, models []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, m := range models {
		if m["slug"] == slug {
			return m
		}
	}
	t.Fatalf("目录里没有 %s", slug)
	return nil
}

func efforts(t *testing.T, e map[string]any) []any {
	t.Helper()
	var out []any
	levels, _ := e["supported_reasoning_levels"].([]any)
	for _, l := range levels {
		out = append(out, l.(map[string]any)["effort"])
	}
	return out
}

func relayResult(t *testing.T, res *catalog.Result, id string) catalog.RelayResult {
	t.Helper()
	for _, r := range res.Relays {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("Relays 里没有编号 %s: %+v", id, res.Relays)
	return catalog.RelayResult{}
}

func twoStubs(t *testing.T) (a, b *relayStub) {
	t.Helper()
	return newRelayStub(t, 200, `{"data":[{"id":"auto"},{"id":"deepseek-v3.2","reasoning":{"supported_efforts":["low","high"]}},{"id":"unknown-model-x"}]}`),
		newRelayStub(t, 200, `{"data":[{"id":"auto"},{"id":"gpt-a"}]}`)
}

func TestRelaySlug(t *testing.T) {
	t.Parallel()
	tests := []struct{ id, model, want string }{
		{"1", "auto", "relay-auto"},
		{"2", "auto", "relay2-auto"},
		{"10", "glm-5.3", "relay10-glm-5.3"},
		{"1", "gpt-a", "relay-gpt-a"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := catalog.RelaySlug(tt.id, tt.model); got != tt.want {
				t.Errorf("RelaySlug(%q,%q) = %q, want %q", tt.id, tt.model, got, tt.want)
			}
		})
	}
}

func TestBuild_TwoRelays_SlugsDisplayNamesOrderAndDescription(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()

	res := build(t, buildOpts(t, codex, dataDir,
		source("1", "主力", a.URL),
		source("2", "备用", b.URL)))

	models := roundtrip(t, res.Models)
	wantSlugs := []any{"gpt-a", "gpt-b", "relay-auto", "relay-deepseek-v3.2", "relay-unknown-model-x", "relay2-auto", "relay2-gpt-a"}
	if got := slugsOf(models); !reflect.DeepEqual(got, wantSlugs) {
		t.Fatalf("slugs = %v, want %v", got, wantSlugs)
	}
	display := map[string]string{
		"relay-auto":            "主力 · auto",
		"relay-deepseek-v3.2":   "主力 · deepseek-v3.2",
		"relay-unknown-model-x": "主力 · unknown-model-x",
		"relay2-auto":           "备用 · auto",
		"relay2-gpt-a":          "备用 · gpt-a",
	}
	for slug, want := range display {
		e := bySlug(t, models, slug)
		if e["display_name"] != want {
			t.Errorf("%s display_name = %v, want %s", slug, e["display_name"], want)
		}
		if e["description"] != "中转模型" {
			t.Errorf("%s description = %v, want 中转模型", slug, e["description"])
		}
	}
	for i, m := range models[2:] {
		if m["priority"] != float64(8+i) {
			t.Errorf("%v priority = %v, want %d（官方最大值 7 之后连续递增）", m["slug"], m["priority"], 8+i)
		}
	}

	fileModels := catalogFileModels(t, dataDir)
	if got := slugsOf(fileModels); !reflect.DeepEqual(got, wantSlugs) {
		t.Errorf("目录文件 slugs = %v, want %v", got, wantSlugs)
	}
	if strings.Contains(readFileT(t, filepath.Join(dataDir, "model-bridge-catalog.json")), "中台") {
		t.Error("目录文件里不应出现「中台」")
	}
}

func TestBuild_TwoRelays_SameModelNameFromBothRelaysKeepsBothEntries(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))
	models := roundtrip(t, res.Models)
	bySlug(t, models, "relay-auto")
	bySlug(t, models, "relay2-auto")
}

func TestBuild_RelayEntriesFollowConfigOrderThenAPIOrder(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	tests := []struct {
		name   string
		relays func(t *testing.T) []catalog.RelaySource
		want   []any
	}{
		{"两个配置分别返回 m2 m1 和 m3", func(t *testing.T) []catalog.RelaySource {
			return []catalog.RelaySource{idSource(t, "1", "甲", "m2", "m1"), idSource(t, "2", "乙", "m3")}
		}, []any{"gpt-a", "gpt-b", "relay-m2", "relay-m1", "relay2-m3"}},
		{"配置顺序与编号大小无关，同名官方模型不提前", func(t *testing.T) []catalog.RelaySource {
			return []catalog.RelaySource{
				idSource(t, "2", "乙", "gpt-b", "deepseek-v3.2", "gpt-a", "auto"),
				idSource(t, "1", "甲", "deepseek-v3.2", "auto"),
			}
		}, []any{"gpt-a", "gpt-b", "relay2-gpt-b", "relay2-deepseek-v3.2", "relay2-gpt-a", "relay2-auto", "relay-deepseek-v3.2", "relay-auto"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := build(t, buildOpts(t, codex, t.TempDir(), tt.relays(t)...))
			if got := slugs(t, res); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("slugs = %v, want %v", got, tt.want)
			}
			for i, m := range roundtrip(t, res.Models)[2:] {
				if m["priority"] != float64(8+i) {
					t.Errorf("%v priority = %v, want %d", m["slug"], m["priority"], 8+i)
				}
			}
		})
	}
}

func TestBuild_AnyRelayModelNameAppearsInCatalog(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	names := []string{"GPT-Y", "glm-5.3", "my-gpt-1", "unknown-model-x", "vendor/some:model_v2"}
	dataDir := t.TempDir()
	res := build(t, buildOpts(t, codex, dataDir, idSource(t, "1", "公司", names...)))
	models := roundtrip(t, res.Models)
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			e := bySlug(t, models, "relay-"+n)
			if want := "公司 · " + n; e["display_name"] != want {
				t.Errorf("display_name = %v, want %s", e["display_name"], want)
			}
			bySlug(t, catalogFileModels(t, dataDir), "relay-"+n)
			if _, ok := res.Routes["relay-"+n]; !ok {
				t.Errorf("Routes 里没有 relay-%s", n)
			}
		})
	}
	if got := relayResult(t, res, "1").ModelCount; got != len(names) {
		t.Errorf("ModelCount = %d, want %d", got, len(names))
	}
}

func TestBuild_Relays_ResultPerRelay(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	before := time.Now().Add(-time.Second)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))

	if len(res.Relays) != 2 {
		t.Fatalf("Relays = %+v", res.Relays)
	}
	tests := []struct {
		id    string
		count int
	}{
		{"1", 3},
		{"2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			r := relayResult(t, res, tt.id)
			if r.ModelCount != tt.count {
				t.Errorf("ModelCount = %d, want %d（接口返回的每个模型都算）", r.ModelCount, tt.count)
			}
			if r.Stale || r.Err != nil {
				t.Errorf("Stale = %v, Err = %v", r.Stale, r.Err)
			}
			if r.FetchedAt.Before(before) || r.FetchedAt.After(time.Now().Add(time.Second)) {
				t.Errorf("FetchedAt = %v", r.FetchedAt)
			}
		})
	}
}

func TestBuild_Routes(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))

	want := map[string]catalog.RelayRoute{
		"relay-auto":            {ID: "1", Model: "auto"},
		"relay-deepseek-v3.2":   {ID: "1", Model: "deepseek-v3.2"},
		"relay-unknown-model-x": {ID: "1", Model: "unknown-model-x"},
		"relay2-auto":           {ID: "2", Model: "auto"},
		"relay2-gpt-a":          {ID: "2", Model: "gpt-a"},
	}
	if !reflect.DeepEqual(res.Routes, want) {
		t.Errorf("Routes = %+v, want %+v", res.Routes, want)
	}
}

func TestBuild_RelayOnly_NoOfficialModels(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	opts := buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL))
	opts.RelayOnly = true
	res := build(t, opts)

	want := []any{"relay-auto", "relay-deepseek-v3.2", "relay-unknown-model-x", "relay2-auto", "relay2-gpt-a"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("Result.Models slugs = %v, want %v", got, want)
	}
	if got := slugsOf(catalogFileModels(t, dataDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("目录文件 slugs = %v, want %v", got, want)
	}
}

func TestBuild_RelayOnly_RelayEntriesAndResultMatchFullCatalog(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	mk := func(relayOnly bool) (*catalog.Result, []map[string]any) {
		dataDir := t.TempDir()
		opts := buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL))
		opts.RelayOnly = relayOnly
		return build(t, opts), catalogFileModels(t, dataDir)
	}
	only, onlyModels := mk(true)
	full, fullModels := mk(false)
	if !reflect.DeepEqual(onlyModels, fullModels[2:]) {
		t.Errorf("只用中转的条目与完整目录里的中转条目不同:\n got  %v\n want %v", onlyModels, fullModels[2:])
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"RelayModels", only.RelayModels, full.RelayModels},
		{"RelayShown", only.RelayShown, full.RelayShown},
		{"ReasoningLevels", only.ReasoningLevels, full.ReasoningLevels},
		{"Routes", only.Routes, full.Routes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("RelayOnly = %v, 完整目录 = %v", tt.got, tt.want)
			}
		})
	}
	if len(full.Routes) == 0 || len(full.ReasoningLevels) == 0 {
		t.Errorf("对照结果为空，用例无效: %v %v", full.Routes, full.ReasoningLevels)
	}
}

func TestBuild_RelayOnly_OfficialListFails_ReturnsError(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, "", 1)
	opts := buildOpts(t, codex, t.TempDir(), idSource(t, "1", "主力", "auto"))
	opts.RelayOnly = true
	_, err := catalog.Build(context.Background(), opts)
	if got := catalog.ErrorCode(err); got != "chatgpt_unsupported" {
		t.Errorf("code = %q (err=%v)", got, err)
	}
}

func TestBuild_RelayOnly_IgnoresAccountCache(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	cache := filepath.Join(t.TempDir(), "models_cache.json")
	if err := os.WriteFile(cache, []byte(`{"models":[{"slug":"gpt-a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, relayOnly := range []bool{true, false} {
		opts := buildOpts(t, codex, t.TempDir(), idSource(t, "1", "主力", "auto"))
		opts.ModelsCachePath = cache
		opts.RelayOnly = relayOnly
		want := []any{"gpt-a", "relay-auto"}
		if relayOnly {
			want = []any{"relay-auto"}
		}
		if got := slugs(t, build(t, opts)); !reflect.DeepEqual(got, want) {
			t.Errorf("RelayOnly=%v slugs = %v, want %v", relayOnly, got, want)
		}
	}
}

func seedBothCaches(t *testing.T, codex, dataDir string, a, b *relayStub) {
	t.Helper()
	build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL)))
	for _, name := range []string{"relay-models-1.json", "relay-models-2.json"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Fatalf("成功获取后应写出缓存 %s: %v", name, err)
		}
	}
}

func TestBuild_SecondRelayFails_WithCache_KeepsCachedModelsMarkedStale(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	seedBothCaches(t, codex, dataDir, a, b)
	seeded := relayResult(t, build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL))), "2")
	cacheBefore := readFileT(t, filepath.Join(dataDir, "relay-models-2.json"))
	b.status.Store(500)

	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL)))

	want := []any{"gpt-a", "gpt-b", "relay-auto", "relay-deepseek-v3.2", "relay-unknown-model-x", "relay2-auto", "relay2-gpt-a"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	r2 := relayResult(t, res, "2")
	if !r2.Stale || catalog.ErrorCode(r2.Err) != "relay_5xx" {
		t.Errorf("配置 2: Stale = %v, Err = %v", r2.Stale, r2.Err)
	}
	if r2.ModelCount != 2 {
		t.Errorf("配置 2 ModelCount = %d, want 2", r2.ModelCount)
	}
	if !r2.FetchedAt.Equal(seeded.FetchedAt) && r2.FetchedAt.Sub(seeded.FetchedAt).Abs() > time.Second {
		t.Errorf("没更新时 FetchedAt 应是缓存里的获取时间: %v vs %v", r2.FetchedAt, seeded.FetchedAt)
	}
	r1 := relayResult(t, res, "1")
	if r1.Stale || r1.Err != nil {
		t.Errorf("配置 1 不受影响: Stale = %v, Err = %v", r1.Stale, r1.Err)
	}
	if got := readFileT(t, filepath.Join(dataDir, "relay-models-2.json")); got != cacheBefore {
		t.Errorf("获取失败时不应覆盖缓存:\n got  %s\n want %s", got, cacheBefore)
	}
}

func TestBuild_SecondRelayFails_NoCache_NoModelsFirstUnaffected(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	b := newRelayStub(t, 401, "")
	dataDir := t.TempDir()

	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL)))

	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	r2 := relayResult(t, res, "2")
	if catalog.ErrorCode(r2.Err) != "relay_401" || r2.ModelCount != 0 || !r2.FetchedAt.IsZero() {
		t.Errorf("配置 2 = %+v", r2)
	}
	r1 := relayResult(t, res, "1")
	if r1.Err != nil || r1.Stale || r1.ModelCount != 1 {
		t.Errorf("配置 1 = %+v", r1)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "relay-models-2.json")); !os.IsNotExist(err) {
		t.Errorf("从未成功过不应产生缓存文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("目录文件没写出: %v", err)
	}
}

func TestBuild_FirstRelayFails_SecondStillBuilt(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a := newRelayStub(t, 500, secretBody)
	b := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay2-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if err := relayResult(t, res, "1").Err; err == nil || strings.Contains(err.Error(), secretBody) {
		t.Errorf("配置 1 Err = %v", err)
	}
}

func TestBuild_RelayFails_CorruptCache_TreatedAsNoCache(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models-1.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newRelayStub(t, 500, "")
	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if got := catalog.ErrorCode(relayResult(t, res, "1").Err); got != "relay_5xx" {
		t.Errorf("Err code = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("目录文件没写出: %v", err)
	}
}

func TestBuild_AllCachedModes_NoRequestToEitherRelay(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	seedBothCaches(t, codex, dataDir, a, b)
	a.hits.Store(0)
	b.hits.Store(0)

	res := build(t, buildOpts(t, codex, dataDir,
		sourceMode("1", "主力", a.URL, catalog.CacheOnly, nil),
		sourceMode("2", "备用", b.URL, catalog.UseGiven, []catalog.RelayModel{})))

	if a.hits.Load() != 0 || b.hits.Load() != 0 {
		t.Errorf("请求次数 a=%d b=%d, want 0", a.hits.Load(), b.hits.Load())
	}
	want := []any{"gpt-a", "gpt-b", "relay-auto", "relay-deepseek-v3.2", "relay-unknown-model-x"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v（配置 1 用缓存，配置 2 用传入的空列表）", got, want)
	}
	if r := relayResult(t, res, "1"); r.Err != nil || r.Stale {
		t.Errorf("配置 1 用缓存不应带错误: %+v", r)
	}
	if r := relayResult(t, res, "2"); r.Err != nil || r.Stale || r.FetchedAt.IsZero() || r.ModelCount != 0 {
		t.Errorf("配置 2 = %+v", r)
	}
}

func TestBuild_LegacyRelayModelsFile_ReadAsCacheOfID1(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	at := "2026-01-02T03:04:05Z"
	legacy := `{"fetched_at":"` + at + `","ids":["deepseek-v3.2","auto"]}`
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newRelayStub(t, 500, "")
	b := newRelayStub(t, 500, "")

	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", a.URL), source("2", "备用", b.URL)))

	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-deepseek-v3.2", "relay-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v（旧文件只当编号 1 的缓存，顺序按缓存里的模型顺序）", got, want)
	}
	r1 := relayResult(t, res, "1")
	wantAt, _ := time.Parse(time.RFC3339, at)
	if !r1.Stale || !r1.FetchedAt.Equal(wantAt) || r1.ModelCount != 2 {
		t.Errorf("配置 1 = %+v", r1)
	}
}

func TestBuild_NewCacheOfID1_WinsOverLegacyFile(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models.json"), []byte(`{"fetched_at":"2026-01-02T03:04:05Z","ids":["deepseek-v3.2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	build(t, buildOpts(t, codex, dataDir, source("1", "主力", ok.URL)))
	ok.status.Store(500)

	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", ok.URL)))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

func TestBuild_LegacyRelayModelsFile_NotUsedForOtherIDs(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models.json"), []byte(`{"fetched_at":"2026-01-02T03:04:05Z","ids":["auto"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := newRelayStub(t, 500, "")
	res := build(t, buildOpts(t, codex, dataDir, source("2", "备用", b.URL)))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

func TestBuild_DefaultModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ids1 []string
		ids2 []string
		want string
	}{
		{"两个配置都有 auto 取第一个", []string{"auto"}, []string{"auto"}, "relay-auto"},
		{"第一个没有 auto 取第二个", []string{"deepseek-v3.2"}, []string{"auto"}, "relay2-auto"},
		{"都没有 auto 为空", []string{"deepseek-v3.2"}, []string{"gpt-a"}, ""},
		{"第一个没有模型取第二个", []string{}, []string{"deepseek-v3.2", "auto"}, "relay2-auto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codex, _ := writeFakeCodex(t, officialJSON, 0)
			res := build(t, buildOpts(t, codex, t.TempDir(),
				idSource(t, "1", "甲", tt.ids1...), idSource(t, "2", "乙", tt.ids2...)))
			if res.DefaultModel != tt.want {
				t.Errorf("DefaultModel = %q, want %q", res.DefaultModel, tt.want)
			}
		})
	}
}

func TestBuild_DefaultModel_FirstRelayFailedWithoutCache_UsesSecond(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a := newRelayStub(t, 500, "")
	b := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))
	if res.DefaultModel != "relay2-auto" {
		t.Errorf("DefaultModel = %q, want relay2-auto", res.DefaultModel)
	}
}

func TestBuild_NoRelays_OnlyOfficial(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	res := build(t, buildOpts(t, codex, t.TempDir()))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if len(res.Relays) != 0 || len(res.Routes) != 0 || res.DefaultModel != "" {
		t.Errorf("Relays = %v, Routes = %v, DefaultModel = %q", res.Relays, res.Routes, res.DefaultModel)
	}
}

func TestBuild_OfficialErrors(t *testing.T) {
	t.Parallel()
	srv := newRelayStub(t, 200, `{"data":[]}`)
	t.Run("codex 不存在", func(t *testing.T) {
		t.Parallel()
		_, err := catalog.Build(context.Background(), buildOpts(t, filepath.Join(t.TempDir(), "nope"), t.TempDir(), source("1", "主力", srv.URL)))
		if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
	})
	t.Run("输出非法 JSON", func(t *testing.T) {
		t.Parallel()
		codex, _ := writeFakeCodex(t, "not json", 0)
		dataDir := t.TempDir()
		_, err := catalog.Build(context.Background(), buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
		if got := catalog.ErrorCode(err); got != "chatgpt_unsupported" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); !os.IsNotExist(err) {
			t.Errorf("官方清单失败时不应写出合并目录")
		}
	})
}

func TestBuild_SaveCacheFails_StillWritesCatalog(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "relay-models-1.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
	if res.StoreErr == nil {
		t.Error("StoreErr 应不为空")
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_DoesNotReadCodexAuth(t *testing.T) {
	origHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(origHome, "auth.json"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", origHome)
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := newRelayStub(t, 200, `{"data":[{"id":"auto"}]}`)
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", srv.URL)))
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

const sameNameOfficialJSON = `{"models":[
{"slug":"gpt-x","priority":3,"display_name":"X","description":"官方","base_instructions":"keep-x",
 "context_window":400000,"max_context_window":400000,
 "supported_reasoning_levels":[{"effort":"low","description":"快"},{"effort":"medium","description":"中"},{"effort":"xhigh","description":"深"}],
 "default_reasoning_level":"medium","input_modalities":["text","image"],"visibility":"list",
 "supports_search_tool":true,"web_search_tool_type":"text","upgrade":{"model":"gpt-z"},
 "availability_nux":{"message":"hi"},"model_messages":{"a":"b"},
 "service_tiers":["fast"],"additional_speed_tiers":["fast"],"experimental_supported_tools":["t"],
 "supported_in_api":false},
{"slug":"gpt-h","priority":5,"display_name":"H","visibility":"hide","context_window":200000,"max_context_window":200000,
 "supported_reasoning_levels":[{"effort":"high","description":"高"}],"default_reasoning_level":"high","input_modalities":["text"]},
{"slug":"gpt-y","priority":9,"display_name":"Y"},
{"slug":"gpt-m","priority":4,"display_name":"M","context_window":1000,"max_context_window":1000,"input_modalities":["text"],
 "supported_reasoning_levels":[{"effort":"high","description":"h"},{"effort":"max","description":"m"},{"effort":"ultra","description":"u"}],
 "default_reasoning_level":"max"}
]}`

func TestBuild_OfficialEntriesUnchangedWithRelayModels(t *testing.T) {
	t.Parallel()
	_, models := buildOne(t, sameNameOfficialJSON, idItems("gpt-x", "gpt-h", "auto")...)
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(sameNameOfficialJSON), &file); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models[:4], file.Models) {
		t.Errorf("官方条目被改动或顺序变化")
	}
}

func TestBuild_RelayModelsWithOfficialNames_AllListed(t *testing.T) {
	t.Parallel()
	_, models := buildOne(t, sameNameOfficialJSON, idItems("gpt-h", "unknown-model-x", "gpt-x")...)
	want := []any{"gpt-x", "gpt-h", "gpt-y", "gpt-m", "relay-gpt-h", "relay-unknown-model-x", "relay-gpt-x"}
	if got := slugsOf(models); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v（接口里的每个模型都出现；接口没有的官方名 gpt-y 不加中转条目）", got, want)
	}
}

func TestBuild_SameNameRelayEntry_AllParamsMissing_CopiesOfficialAndOverridesRelayFields(t *testing.T) {
	t.Parallel()
	_, models := buildOne(t, sameNameOfficialJSON, `{"id":"gpt-x"}`)
	official := bySlug(t, models, "gpt-x")
	e := bySlug(t, models, "relay-gpt-x")

	for _, k := range []string{"context_window", "max_context_window", "supported_reasoning_levels", "default_reasoning_level", "input_modalities", "visibility"} {
		if !reflect.DeepEqual(e[k], official[k]) {
			t.Errorf("%s = %#v, want 照搬官方 %#v", k, e[k], official[k])
		}
	}
	exact := map[string]any{
		"supports_search_tool":         false,
		"service_tiers":                []any{},
		"additional_speed_tiers":       []any{},
		"experimental_supported_tools": []any{},
		"supported_in_api":             true,
		"description":                  "中转模型",
		"display_name":                 "主力 · gpt-x",
	}
	for k, want := range exact {
		if !reflect.DeepEqual(e[k], want) {
			t.Errorf("%s = %#v, want %#v", k, e[k], want)
		}
	}
	for _, k := range []string{"web_search_tool_type", "upgrade", "availability_nux", "model_messages"} {
		if v, ok := e[k]; ok {
			t.Errorf("%s = %#v，应不写这个字段", k, v)
		}
	}
}

func TestBuild_RefreshOneRelay_TargetFails_OnlyTargetContacted(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	seedBothCaches(t, codex, dataDir, a, b)
	a.status.Store(500)
	a.hits.Store(0)
	b.hits.Store(0)

	res := build(t, buildOpts(t, codex, dataDir,
		sourceMode("1", "主力", a.URL, catalog.FetchRemote, nil),
		sourceMode("2", "备用", b.URL, catalog.CacheOnly, nil)))

	if a.hits.Load() == 0 || b.hits.Load() != 0 {
		t.Errorf("请求次数 a=%d b=%d，只应请求配置 1", a.hits.Load(), b.hits.Load())
	}
	r1 := relayResult(t, res, "1")
	if !r1.Stale || catalog.ErrorCode(r1.Err) != "relay_5xx" || r1.ModelCount != 3 {
		t.Errorf("配置 1 = %+v", r1)
	}
	r2 := relayResult(t, res, "2")
	if r2.Stale || r2.Err != nil || r2.ModelCount != 2 {
		t.Errorf("配置 2 = %+v", r2)
	}
	want := []any{"gpt-a", "gpt-b", "relay-auto", "relay-deepseek-v3.2", "relay-unknown-model-x", "relay2-auto", "relay2-gpt-a"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

func TestBuild_AllCacheOnly_NoRequests(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	seedBothCaches(t, codex, dataDir, a, b)
	a.hits.Store(0)
	b.hits.Store(0)

	res := build(t, buildOpts(t, codex, dataDir,
		sourceMode("1", "主力", a.URL, catalog.CacheOnly, nil),
		sourceMode("2", "备用", b.URL, catalog.CacheOnly, nil)))

	if a.hits.Load() != 0 || b.hits.Load() != 0 {
		t.Errorf("请求次数 a=%d b=%d, want 0", a.hits.Load(), b.hits.Load())
	}
	for id, count := range map[string]int{"1": 3, "2": 2} {
		r := relayResult(t, res, id)
		if r.Stale || r.Err != nil || r.FetchedAt.IsZero() || r.ModelCount != count {
			t.Errorf("配置 %s = %+v, want ModelCount %d", id, r, count)
		}
	}
}

func TestBuild_CacheOnly_NoCache_NoModelsNoError(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, _ := twoStubs(t)
	res := build(t, buildOpts(t, codex, t.TempDir(), sourceMode("1", "主力", a.URL, catalog.CacheOnly, nil)))
	if a.hits.Load() != 0 {
		t.Errorf("请求次数 = %d, want 0", a.hits.Load())
	}
	r := relayResult(t, res, "1")
	if r.Err != nil || r.ModelCount != 0 || !r.FetchedAt.IsZero() {
		t.Errorf("配置 1 = %+v", r)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

func TestBuild_UseGivenEmptySlice_NoRequestWritesCacheZeroModels(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, _ := twoStubs(t)
	dataDir := t.TempDir()

	res := build(t, buildOpts(t, codex, dataDir, sourceMode("1", "主力", a.URL, catalog.UseGiven, []catalog.RelayModel{})))

	if a.hits.Load() != 0 {
		t.Errorf("请求次数 = %d, want 0", a.hits.Load())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "relay-models-1.json")); err != nil {
		t.Errorf("空列表也应写缓存: %v", err)
	}
	r := relayResult(t, res, "1")
	if r.ModelCount != 0 || r.Err != nil || r.FetchedAt.IsZero() {
		t.Errorf("配置 1 = %+v", r)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
}

func TestBuild_CacheAndCatalogFilesDoNotContainKeyOrHost(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	a, b := twoStubs(t)
	dataDir := t.TempDir()
	build(t, buildOpts(t, codex, dataDir,
		sourceMode("1", "主力", a.URL, catalog.FetchRemote, nil),
		sourceMode("2", "备用", b.URL, catalog.FetchRemote, nil)))

	files, err := filepath.Glob(filepath.Join(dataDir, "*.json"))
	if err != nil || len(files) != 3 {
		t.Fatalf("数据目录里的 json = %v, err=%v, want 两个缓存加一个目录文件", files, err)
	}
	hosts := []string{strings.TrimPrefix(a.URL, "http://"), strings.TrimPrefix(b.URL, "http://"), "127.0.0.1"}
	for _, f := range files {
		content := readFileT(t, f)
		for _, secret := range append([]string{relayKey}, hosts...) {
			if strings.Contains(content, secret) {
				t.Errorf("%s 里出现了 %q", filepath.Base(f), secret)
			}
		}
	}
}

func TestBuild_FetchRemoteRelaysAreFetchedConcurrently(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	var relays []catalog.RelaySource
	for _, id := range []string{"1", "2", "3"} {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		relays = append(relays, sourceMode(id, "配置"+id, srv.URL, catalog.FetchRemote, nil))
	}
	opts := buildOpts(t, codex, t.TempDir(), relays...)
	opts.RelayTimeout = time.Second

	start := time.Now()
	res := build(t, opts)
	if elapsed := time.Since(start); elapsed >= 2500*time.Millisecond {
		t.Errorf("Build 耗时 %v，3 个配置应并发获取（约一个超时），不是相加", elapsed)
	}
	for _, id := range []string{"1", "2", "3"} {
		if relayResult(t, res, id).Err == nil {
			t.Errorf("配置 %s 应因超时带错误", id)
		}
	}
}

func TestBuild_CacheOnly_UnusableCacheFile_CarriesError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{"缓存 JSON 解码失败", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"缓存文件读取失败", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			codex, _ := writeFakeCodex(t, officialJSON, 0)
			a, _ := twoStubs(t)
			dataDir := t.TempDir()
			tc.setup(t, filepath.Join(dataDir, "relay-models-1.json"))

			res := build(t, buildOpts(t, codex, dataDir, sourceMode("1", "主力", a.URL, catalog.CacheOnly, nil)))

			if a.hits.Load() != 0 {
				t.Errorf("请求次数 = %d, want 0", a.hits.Load())
			}
			if r := relayResult(t, res, "1"); r.Err == nil {
				t.Errorf("缓存不可用时配置 1 应带错误, 得到 %+v", r)
			}
		})
	}
}
