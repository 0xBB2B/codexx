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
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

func relayServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func buildOpts(t *testing.T, codex, relayURL, dataDir string) catalog.BuildOptions {
	t.Helper()
	return catalog.BuildOptions{
		CodexPath:    codex,
		RelayBaseURL: relayURL,
		RelayKey:     "sk-example-0000",
		RelayTimeout: time.Second,
		DataDir:      dataDir,
		Params:       mergeParams,
	}
}

func slugs(t *testing.T, res *catalog.Result) []any {
	t.Helper()
	var out []any
	for _, m := range roundtrip(t, res.Models) {
		out = append(out, m["slug"])
	}
	return out
}

func TestBuild_RelaySuccess_MergesSavesRecordAndWritesCatalog(t *testing.T) {
	origHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(origHome, "auth.json"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", origHome)

	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 200, `{"data":[{"id":"auto"},{"id":"deepseek-v3.2"},{"id":"unknown-model-x"}]}`)
	dataDir := t.TempDir()

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.RelayErr != nil {
		t.Errorf("RelayErr = %v", res.RelayErr)
	}
	want := []any{"gpt-a", "gpt-b", "relay-auto", "relay-deepseek-v3.2"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	m := roundtrip(t, res.Models)
	if m[2]["priority"] != float64(8) || m[3]["priority"] != float64(9) {
		t.Errorf("priority = %v, %v", m[2]["priority"], m[3]["priority"])
	}

	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
	ids, _, ok, err := catalog.NewStore(dataDir).LoadRelayModels()
	if err != nil || !ok {
		t.Fatalf("上次成功记录没写出: %v %v", ok, err)
	}
	for _, id := range []string{"auto", "deepseek-v3.2"} {
		found := false
		for _, g := range ids {
			if g == id {
				found = true
			}
		}
		if !found {
			t.Errorf("记录里缺 %s: %v", id, ids)
		}
	}
}

func TestBuild_RelayFails_UsesLastSuccessfulRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := catalog.NewStore(dataDir).SaveRelayModels([]string{"deepseek-v3.2", "unknown-model-x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, secretBody)

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_5xx" {
		t.Errorf("RelayErr code = %q", got)
	}
	want := []any{"gpt-a", "gpt-b", "relay-deepseek-v3.2"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	ids, _, _, _ := catalog.NewStore(dataDir).LoadRelayModels()
	if !reflect.DeepEqual(ids, []string{"deepseek-v3.2", "unknown-model-x"}) {
		t.Errorf("失败时不应覆盖上次记录: %v", ids)
	}
}

func TestBuild_RelayFails_NoRecord_OnlyOfficial(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	srv := relayServer(t, 401, "")

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_401" {
		t.Errorf("RelayErr code = %q", got)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "relay-models.json")); !os.IsNotExist(err) {
		t.Errorf("从未成功过不应产生记录文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_OfficialErrors(t *testing.T) {
	srv := relayServer(t, 200, `{"data":[]}`)
	t.Run("codex 不存在", func(t *testing.T) {
		_, err := catalog.Build(context.Background(), buildOpts(t, filepath.Join(t.TempDir(), "nope"), srv.URL, t.TempDir()))
		if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
	})
	t.Run("输出非法 JSON", func(t *testing.T) {
		codex, _ := writeFakeCodex(t, "not json", 0)
		dataDir := t.TempDir()
		_, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
		if got := catalog.ErrorCode(err); got != "chatgpt_unsupported" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); !os.IsNotExist(err) {
			t.Errorf("官方清单失败时不应写出合并目录")
		}
	})
}

func TestBuild_RelaySuccess_FillsFetchedAt(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 200, `{"data":[{"id":"auto"}]}`)
	before := time.Now().Add(-time.Second)
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if res.RelayFetchedAt.Before(before) || res.RelayFetchedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
}

func TestBuild_RelayFails_FetchedAtFromRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := catalog.NewStore(dataDir).SaveRelayModels([]string{"auto"}, at); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if !res.RelayFetchedAt.Equal(at) {
		t.Errorf("RelayFetchedAt = %v, want %v", res.RelayFetchedAt, at)
	}
}

func TestBuild_RelayFails_NoRecord_FetchedAtZero(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if !res.RelayFetchedAt.IsZero() {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
}

func TestBuild_RelayFails_CorruptRecord_TreatedAsNoRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_5xx" {
		t.Errorf("RelayErr code = %q", got)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_RelaySuccess_SaveFails_StillWritesCatalog(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "relay-models.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 200, `{"data":[{"id":"auto"}]}`)
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
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

func TestBuild_PrefetchedRelayIDs_SkipsRequest(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	dataDir := t.TempDir()
	opts := buildOpts(t, codex, srv.URL, dataDir)
	opts.RelayIDs = []string{"auto"}

	before := time.Now().Add(-time.Second)
	res, err := catalog.Build(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 || res.RelayErr != nil {
		t.Errorf("hits = %d, RelayErr = %v", hits, res.RelayErr)
	}
	if res.RelayFetchedAt.Before(before) {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
	if !reflect.DeepEqual(slugs(t, res)[len(slugs(t, res))-1], "relay-auto") {
		t.Errorf("slugs = %v", slugs(t, res))
	}
	if !strings.Contains(readFileT(t, filepath.Join(dataDir, "model-bridge-catalog.json")), `"relay-auto"`) {
		t.Error("合并目录里没有传入的模型")
	}
	if !strings.Contains(readFileT(t, filepath.Join(dataDir, "relay-models.json")), `"auto"`) {
		t.Error("没有保存 relay-models.json")
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const sameNameOfficialJSON = `{"models":[
{"slug":"gpt-x","priority":3,"display_name":"X","description":"官方","base_instructions":"keep-x",
 "context_window":400000,"max_context_window":500000,
 "supported_reasoning_levels":[{"effort":"low","description":"快"},{"effort":"medium","description":"中"},{"effort":"xhigh","description":"深"}],
 "default_reasoning_level":"medium","input_modalities":["text","image"],"visibility":"list",
 "supports_search_tool":true,"web_search_tool_type":"text","upgrade":{"model":"gpt-z"},
 "availability_nux":{"message":"hi"},"model_messages":{"a":"b"},
 "service_tiers":["fast"],"additional_speed_tiers":["fast"],"experimental_supported_tools":["t"],
 "supported_in_api":false},
{"slug":"gpt-h","priority":5,"display_name":"H","visibility":"hide","context_window":200000,"max_context_window":200000,
 "supported_reasoning_levels":[{"effort":"high","description":"高"}],"default_reasoning_level":"high","input_modalities":["text"]},
{"slug":"gpt-y","priority":9,"display_name":"Y"}
]}`

func sameNameParams() []catalog.Param {
	return []catalog.Param{
		{ID: "auto", ContextWindow: 1000000, InputModalities: []string{"text", "image"}, ReasoningLevels: []string{}, Source: "s"},
		{ID: "glm-5.3", ContextWindow: 128000, InputModalities: []string{"text"}, ReasoningLevels: []string{"high"}, DefaultReasoningLevel: strp("high"), Source: "s"},
		{ID: "gpt-x", ContextWindow: 1, InputModalities: []string{"audio"}, ReasoningLevels: []string{"minimal"}, DefaultReasoningLevel: strp("minimal"), Source: "s"},
	}
}

func buildSameName(t *testing.T, ids []string) (*catalog.Result, []map[string]any) {
	t.Helper()
	codex, _ := writeFakeCodex(t, sameNameOfficialJSON, 0)
	opts := buildOpts(t, codex, "", t.TempDir())
	opts.Params = sameNameParams()
	opts.RelayIDs = ids
	res, err := catalog.Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return res, roundtrip(t, res.Models)
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

func TestBuild_RelayEntriesUsePrefixedSlugAndSuffixedDisplayName(t *testing.T) {
	_, models := buildSameName(t, []string{"glm-5.3", "auto", "gpt-x"})
	tests := []struct{ slug, display string }{
		{"relay-glm-5.3", "中台 · glm-5.3"},
		{"relay-auto", "中台 · auto"},
		{"relay-gpt-x", "中台 · gpt-x"},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := bySlug(t, models, tt.slug)["display_name"]; got != tt.display {
				t.Errorf("display_name = %v, want %s", got, tt.display)
			}
		})
	}
}

func TestBuild_SameNameRelayModels_OnlyOfficialOrParamsListed(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-h", "unknown-model-x", "gpt-x"})
	var got []any
	for _, m := range models {
		got = append(got, m["slug"])
	}
	want := []any{"gpt-x", "gpt-h", "gpt-y", "relay-gpt-x", "relay-gpt-h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v（两边都没有的不出现；中台没有的官方名 gpt-y 不加中台条目）", got, want)
	}
}

func TestBuild_RelayOrder_ParamsTableThenOfficialSameName(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-h", "glm-5.3", "gpt-x", "auto"})
	var got []any
	for _, m := range models[3:] {
		got = append(got, m["slug"])
	}
	want := []any{"relay-auto", "relay-glm-5.3", "relay-gpt-x", "relay-gpt-h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("中台条目顺序 = %v, want %v", got, want)
	}
	for i, m := range models[3:] {
		if m["priority"] != float64(10+i) {
			t.Errorf("%v priority = %v, want %d（官方最大值 9 之后连续递增）", m["slug"], m["priority"], 10+i)
		}
	}
}

func TestBuild_OfficialEntriesUnchangedWithSameNameRelay(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-x", "gpt-h", "auto"})
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(sameNameOfficialJSON), &file); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models[:3], file.Models) {
		t.Errorf("官方条目被改动或顺序变化")
	}
}

func TestBuild_SameNameRelayEntry_CopiesOfficialParamsAndOverridesRelayFields(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-x"})
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
		"description":                  "中台模型",
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

func TestBuild_SameNameRelayEntry_HiddenOfficialStaysHidden(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-h"})
	if got := bySlug(t, models, "relay-gpt-h")["visibility"]; got != "hide" {
		t.Errorf("visibility = %v，官方是 hide 时中台同名条目也应 hide", got)
	}
}

func TestBuild_NameInBothParamsAndOfficial_OfficialParamsWin(t *testing.T) {
	_, models := buildSameName(t, []string{"gpt-x"})
	e := bySlug(t, models, "relay-gpt-x")
	if e["context_window"] != float64(400000) || !reflect.DeepEqual(e["input_modalities"], []any{"text", "image"}) || e["default_reasoning_level"] != "medium" {
		t.Errorf("参数表里也有 gpt-x，但应用官方参数: context_window=%v input_modalities=%v default=%v", e["context_window"], e["input_modalities"], e["default_reasoning_level"])
	}
}

func TestBuild_ParamsOnlyRelayEntry_KeepsParamsTableValues(t *testing.T) {
	_, models := buildSameName(t, []string{"glm-5.3"})
	e := bySlug(t, models, "relay-glm-5.3")
	if e["context_window"] != float64(128000) || e["default_reasoning_level"] != "high" || e["description"] != "中台模型" {
		t.Errorf("参数表模型的参数不对: %v", e)
	}
}

func TestBuild_ReasoningLevelsIncludeSameNameOfficialLevels(t *testing.T) {
	res, _ := buildSameName(t, []string{"gpt-h", "gpt-x", "glm-5.3"})
	want := []string{"high", "low", "medium", "xhigh"}
	if !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v（参数表 glm-5.3 的 high，再是同名 gpt-x 官方的 low/medium/xhigh，gpt-h 的 high 去重）", res.ReasoningLevels, want)
	}
}

func TestBuild_RelayHasOnlyUnknownModels_OnlyOfficial(t *testing.T) {
	res, models := buildSameName(t, []string{"unknown-model-x"})
	if len(models) != 3 || len(res.ReasoningLevels) != 0 {
		t.Errorf("models = %d, levels = %v", len(models), res.ReasoningLevels)
	}
}

func buildWithParams(t *testing.T, officialJSON string, params []catalog.Param, ids []string) (*catalog.Result, []map[string]any) {
	t.Helper()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	opts := buildOpts(t, codex, "", t.TempDir())
	opts.Params = params
	opts.RelayIDs = ids
	res, err := catalog.Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return res, roundtrip(t, res.Models)
}

func efforts(t *testing.T, e map[string]any) []any {
	t.Helper()
	var out []any
	for _, l := range e["supported_reasoning_levels"].([]any) {
		out = append(out, l.(map[string]any)["effort"])
	}
	return out
}

func TestBuild_ParamsTableMaxLevelWrittenAsUltra(t *testing.T) {
	params := []catalog.Param{
		{ID: "m-max", ContextWindow: 1, InputModalities: []string{"text"}, ReasoningLevels: []string{"low", "high", "max"}, DefaultReasoningLevel: strp("max"), Source: "s"},
		{ID: "m-high", ContextWindow: 1, InputModalities: []string{"text"}, ReasoningLevels: []string{"none", "high", "max"}, DefaultReasoningLevel: strp("high"), Source: "s"},
	}
	res, models := buildWithParams(t, officialJSON, params, []string{"m-max", "m-high"})

	a := bySlug(t, models, "relay-m-max")
	if got, want := efforts(t, a), []any{"low", "high", "ultra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("efforts = %v, want %v", got, want)
	}
	if a["default_reasoning_level"] != "ultra" {
		t.Errorf("default_reasoning_level = %v, want ultra", a["default_reasoning_level"])
	}
	b := bySlug(t, models, "relay-m-high")
	if got, want := efforts(t, b), []any{"none", "high", "ultra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("efforts = %v, want %v", got, want)
	}
	if b["default_reasoning_level"] != "high" {
		t.Errorf("default_reasoning_level = %v，非 max 的默认等级不变", b["default_reasoning_level"])
	}
	if want := []string{"low", "high", "ultra", "none"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v（写 Codex 配置用 ultra，不含 max）", res.ReasoningLevels, want)
	}
}

func TestBuild_SameNameRelayKeepsOfficialMaxAndUltra(t *testing.T) {
	const official = `{"models":[{"slug":"gpt-x","priority":3,"supported_reasoning_levels":[{"effort":"high","description":"h"},{"effort":"max","description":"m"},{"effort":"ultra","description":"u"}],"default_reasoning_level":"max"}]}`
	res, models := buildWithParams(t, official, nil, []string{"gpt-x"})
	e := bySlug(t, models, "relay-gpt-x")
	if got, want := efforts(t, e), []any{"high", "max", "ultra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("efforts = %v, want %v（同名照搬官方，不替换）", got, want)
	}
	if e["default_reasoning_level"] != "max" {
		t.Errorf("default_reasoning_level = %v, want max", e["default_reasoning_level"])
	}
	if want := []string{"high", "max", "ultra"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v", res.ReasoningLevels, want)
	}
}

func TestBuild_DisplayName_GptPrefixedNamesUseLeadingLabel(t *testing.T) {
	params := []catalog.Param{
		{ID: "GPT-Y", ContextWindow: 1, InputModalities: []string{"text"}, Source: "s"},
		{ID: "glm-5.3", ContextWindow: 1, InputModalities: []string{"text"}, Source: "s"},
		{ID: "my-gpt-1", ContextWindow: 1, InputModalities: []string{"text"}, Source: "s"},
	}
	_, models := buildWithParams(t, officialJSON, params, []string{"GPT-Y", "glm-5.3", "my-gpt-1"})
	tests := []struct{ slug, display string }{
		{"relay-GPT-Y", "中台 · GPT-Y"},
		{"relay-glm-5.3", "中台 · glm-5.3"},
		{"relay-my-gpt-1", "中台 · my-gpt-1"},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := bySlug(t, models, tt.slug)["display_name"]; got != tt.display {
				t.Errorf("display_name = %v, want %s", got, tt.display)
			}
		})
	}
}
