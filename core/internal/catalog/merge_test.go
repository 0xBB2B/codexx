package catalog_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"modelbridge/internal/catalog"
)

func roundtrip(t *testing.T, v any) []map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 第一个官方条目是 gpt-x（上下文 400000、输入 text+image、等级 low/medium/xhigh、默认 medium）
const paramsOfficialJSON = sameNameOfficialJSON

type entryWant struct {
	context    float64
	modalities []any
	efforts    []any
	def        any
	updates    any
	ultraToMax bool
}

func assertEntry(t *testing.T, e map[string]any, want entryWant) {
	t.Helper()
	if e["context_window"] != want.context || e["max_context_window"] != want.context {
		t.Errorf("context_window = %v, max_context_window = %v, want %v", e["context_window"], e["max_context_window"], want.context)
	}
	if !reflect.DeepEqual(e["input_modalities"], want.modalities) {
		t.Errorf("input_modalities = %#v, want %#v", e["input_modalities"], want.modalities)
	}
	got := efforts(t, e)
	if (len(got) != 0 || len(want.efforts) != 0) && !reflect.DeepEqual(got, want.efforts) {
		t.Errorf("efforts = %v, want %v", got, want.efforts)
	}
	if def, ok := e["default_reasoning_level"]; want.def == nil {
		if ok {
			t.Errorf("default_reasoning_level = %#v，应不写这个键", def)
		}
	} else if def != want.def {
		t.Errorf("default_reasoning_level = %#v, want %v", def, want.def)
	}
	if want.updates != nil && e["supports_reasoning_effort_updates"] != want.updates {
		t.Errorf("supports_reasoning_effort_updates = %v, want %v", e["supports_reasoning_effort_updates"], want.updates)
	}
}

func TestRelayEntry_ParamsFromAPIAndFallback(t *testing.T) {
	t.Parallel()
	textImage := []any{"text", "image"}
	text := []any{"text"}
	officialLevels := []any{"low", "medium", "xhigh"}
	apiFull := func(id, efforts string) string {
		return `{"id":"` + id + `","context_length":1048576,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":` + efforts + `}}`
	}
	noLevels := entryWant{context: 400000, modalities: textImage, updates: false}
	tests := []struct {
		name string
		item string
		slug string
		want entryWant
	}{
		{"不同名 接口给全参数 max 写成 ultra", apiFull("model-a", `["low","high","max"]`), "relay-model-a",
			entryWant{1048576, text, []any{"low", "high", "ultra"}, "high", true, true}},
		{"同名 接口值覆盖官方值", `{"id":"gpt-x","context_length":1050000,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low","high"]}}`, "relay-gpt-x",
			entryWant{1050000, text, []any{"low", "high"}, "high", true, false}},
		{"丢弃不认识的等级", apiFull("model-a", `["low","turbo","medium"]`), "relay-model-a",
			entryWant{1048576, text, []any{"low", "medium"}, "medium", true, false}},
		{"没有 high 时取最高", apiFull("model-a", `["none","low","medium","xhigh"]`), "relay-model-a",
			entryWant{1048576, text, []any{"none", "low", "medium", "xhigh"}, "xhigh", true, false}},
		{"只有 max 时默认也写成 ultra", apiFull("model-a", `["max"]`), "relay-model-a",
			entryWant{1048576, text, []any{"ultra"}, "ultra", true, true}},
		{"等级顺序按接口", apiFull("model-a", `["xhigh","low","high"]`), "relay-model-a",
			entryWant{1048576, text, []any{"xhigh", "low", "high"}, "high", true, false}},
		{"等级只有 minimal 和 low 取 low", apiFull("model-a", `["minimal","low"]`), "relay-model-a",
			entryWant{1048576, text, []any{"minimal", "low"}, "low", true, false}},
		{"接口给空等级列表", apiFull("model-a", `[]`), "relay-model-a",
			entryWant{1048576, text, nil, nil, false, false}},
		{"接口等级全被丢弃", apiFull("model-a", `["turbo"]`), "relay-model-a",
			entryWant{1048576, text, nil, nil, false, false}},
		{"同名 接口给空等级列表时不用官方等级", apiFull("gpt-x", `[]`), "relay-gpt-x",
			entryWant{1048576, text, nil, nil, false, false}},
		{"同名 接口等级全被丢弃时不用官方等级", apiFull("gpt-x", `["turbo"]`), "relay-gpt-x",
			entryWant{1048576, text, nil, nil, false, false}},

		{"同名 全缺参数用官方值", `{"id":"gpt-x"}`, "relay-gpt-x",
			entryWant{400000, textImage, officialLevels, "medium", nil, false}},
		{"同名 context_length 为 0", `{"id":"gpt-x","context_length":0,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-gpt-x",
			entryWant{400000, text, []any{"high"}, "high", true, false}},
		{"同名 context_length 为负数", `{"id":"gpt-x","context_length":-1,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-gpt-x",
			entryWant{400000, text, []any{"high"}, "high", true, false}},
		{"同名 context_length 为 null", `{"id":"gpt-x","context_length":null,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-gpt-x",
			entryWant{400000, text, []any{"high"}, "high", true, false}},
		{"同名 input_modalities 为空列表", `{"id":"gpt-x","context_length":2000,"architecture":{"input_modalities":[]},"reasoning":{"supported_efforts":["high"]}}`, "relay-gpt-x",
			entryWant{2000, textImage, []any{"high"}, "high", true, false}},
		{"同名 只缺等级时用官方等级和默认等级", `{"id":"gpt-x","context_length":2000,"architecture":{"input_modalities":["text"]}}`, "relay-gpt-x",
			entryWant{2000, text, officialLevels, "medium", nil, false}},
		{"同名 reasoning 里没有 supported_efforts", `{"id":"gpt-x","context_length":2000,"architecture":{"input_modalities":["text"]},"reasoning":{}}`, "relay-gpt-x",
			entryWant{2000, text, officialLevels, "medium", nil, false}},
		{"同名 全缺时官方的 max 与 ultra 原样", `{"id":"gpt-m"}`, "relay-gpt-m",
			entryWant{1000, text, []any{"high", "max", "ultra"}, "max", nil, false}},

		{"不同名 全缺参数用官方第一个条目", `{"id":"model-a"}`, "relay-model-a", noLevels},
		{"不同名 context_length 为 0", `{"id":"model-a","context_length":0,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-model-a",
			entryWant{400000, text, []any{"high"}, "high", true, false}},
		{"不同名 context_length 为负数", `{"id":"model-a","context_length":-7,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-model-a",
			entryWant{400000, text, []any{"high"}, "high", true, false}},
		{"不同名 input_modalities 为空列表", `{"id":"model-a","context_length":2000,"architecture":{"input_modalities":[]},"reasoning":{"supported_efforts":["high"]}}`, "relay-model-a",
			entryWant{2000, textImage, []any{"high"}, "high", true, false}},
		{"不同名 丢弃 Codex 不认识的输入类型", `{"id":"model-a","context_length":2000,"architecture":{"input_modalities":["text","file","audio","video"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-model-a",
			entryWant{2000, []any{"text", "audio"}, []any{"high"}, "high", true, false}},
		{"不同名 输入类型全不认识时用官方第一个条目", `{"id":"model-a","context_length":2000,"architecture":{"input_modalities":["file"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-model-a",
			entryWant{2000, textImage, []any{"high"}, "high", true, false}},
		{"同名 输入类型全不认识时用官方值", `{"id":"gpt-x","context_length":2000,"architecture":{"input_modalities":["file","video"]},"reasoning":{"supported_efforts":["high"]}}`, "relay-gpt-x",
			entryWant{2000, textImage, []any{"high"}, "high", true, false}},
		{"不同名 只缺等级时没有等级", `{"id":"model-a","context_length":2000,"architecture":{"input_modalities":["text"]}}`, "relay-model-a",
			entryWant{2000, text, nil, nil, false, false}},

		{"auto 接口给 500 也用固定值", `{"id":"auto","context_length":500,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low","max"]}}`, "relay-auto",
			entryWant{1000000, textImage, nil, nil, false, false}},
		{"auto 接口什么都不给", `{"id":"auto"}`, "relay-auto",
			entryWant{1000000, textImage, nil, nil, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, models := buildOne(t, paramsOfficialJSON, tt.item)
			assertEntry(t, bySlug(t, models, tt.slug), tt.want)
			if got := res.Routes[tt.slug].UltraToMax; got != tt.want.ultraToMax {
				t.Errorf("UltraToMax = %v, want %v", got, tt.want.ultraToMax)
			}
		})
	}
}

func TestRelayEntry_ReasoningLevelsFollowAPIAndAreWrittenAsUltra(t *testing.T) {
	t.Parallel()
	res, _ := buildOne(t, paramsOfficialJSON,
		`{"id":"model-a","reasoning":{"supported_efforts":["low","high","max"]}}`)
	if want := []string{"low", "high", "ultra"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v（写 Codex 配置用 ultra，不含 max）", res.ReasoningLevels, want)
	}
}

func TestRelayEntry_ReasoningLevelsUnionInFirstSeenOrder(t *testing.T) {
	t.Parallel()
	res, _ := buildOne(t, paramsOfficialJSON,
		`{"id":"m1","reasoning":{"supported_efforts":["low","high"]}}`,
		`{"id":"m2","reasoning":{"supported_efforts":["high","xhigh"]}}`,
		`{"id":"m3"}`,
		`{"id":"auto","reasoning":{"supported_efforts":["minimal"]}}`)
	if want := []string{"low", "high", "xhigh"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v（按首次出现顺序去重，没有等级的和 auto 不贡献）", res.ReasoningLevels, want)
	}
}

func TestRelayEntry_ReasoningLevelsIncludeSameNameOfficialLevels(t *testing.T) {
	t.Parallel()
	res, _ := buildOne(t, paramsOfficialJSON,
		`{"id":"gpt-h"}`, `{"id":"gpt-x"}`, `{"id":"glm-5.3","reasoning":{"supported_efforts":["high"]}}`)
	if want := []string{"high", "low", "medium", "xhigh"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v", res.ReasoningLevels, want)
	}
}

func TestRelayEntry_NoRelayModelsHasNoLevels(t *testing.T) {
	t.Parallel()
	res, models := buildOne(t, paramsOfficialJSON)
	if len(models) != 4 || len(res.ReasoningLevels) != 0 || len(res.Routes) != 0 {
		t.Errorf("models = %d, levels = %v, routes = %v", len(models), res.ReasoningLevels, res.Routes)
	}
}

func TestRelayEntry_OverridesRelayFieldsAndKeepsBase(t *testing.T) {
	t.Parallel()
	const official = `{"models":[
{"slug":"gpt-a","display_name":"GPT A","priority":3,"base_instructions":"keep-me","shell_type":"shell_command",
 "truncation_policy":{"mode":"tokens","limit":10000},"supports_search_tool":true,"web_search_tool_type":"text",
 "upgrade":{"model":"gpt-z"},"availability_nux":{"message":"hi"},"model_messages":{"a":"b"},
 "service_tiers":["fast"],"additional_speed_tiers":["fast"],"experimental_supported_tools":["t"],
 "visibility":"hide","supported_in_api":false,"context_window":1,"max_context_window":1,
 "input_modalities":["text"],"description":"官方"},
{"slug":"gpt-b","priority":7}]}`
	_, models := buildOne(t, official,
		`{"id":"deepseek-v3.2","context_length":128000,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low","high"]}}`)
	e := bySlug(t, models, "relay-deepseek-v3.2")

	exact := map[string]any{
		"slug": "relay-deepseek-v3.2", "display_name": "主力 · deepseek-v3.2", "description": "中转模型",
		"visibility": "list", "supported_in_api": true,
		"context_window": float64(128000), "max_context_window": float64(128000),
		"input_modalities":                  []any{"text"},
		"default_reasoning_level":           "high",
		"supports_reasoning_effort_updates": true,
		"supports_search_tool":              false,
		"experimental_supported_tools":      []any{},
		"service_tiers":                     []any{},
		"additional_speed_tiers":            []any{},
		"supported_reasoning_levels": []any{
			map[string]any{"effort": "low", "description": "low"},
			map[string]any{"effort": "high", "description": "high"},
		},
		"base_instructions": "keep-me", "shell_type": "shell_command",
		"truncation_policy": map[string]any{"mode": "tokens", "limit": float64(10000)},
	}
	for k, want := range exact {
		got, ok := e[k]
		if !ok {
			t.Errorf("缺字段 %s", k)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", k, got, want)
		}
	}
	for _, k := range []string{"web_search_tool_type", "upgrade", "availability_nux", "model_messages"} {
		if v, ok := e[k]; ok {
			t.Errorf("%s = %#v，应不写这个字段，交给 Codex 用默认值", k, v)
		}
	}
}

func TestRelayEntry_PriorityContinuesFromOfficialMax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		official      string
		officialCount int
		want          []float64
	}{
		{"官方最大值为正数", `{"models":[{"slug":"a","priority":3},{"slug":"b","priority":7},{"slug":"c","priority":5}]}`, 3, []float64{8, 9}},
		{"官方优先级全是负数", `{"models":[{"slug":"a","priority":-5},{"slug":"b","priority":-3}]}`, 2, []float64{-2, -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, models := buildOne(t, tt.official, `{"id":"m1"}`, `{"id":"m2"}`)
			relay := models[tt.officialCount:]
			if len(relay) != 2 || relay[0]["priority"] != tt.want[0] || relay[1]["priority"] != tt.want[1] {
				t.Errorf("中转条目 = %v, want priority %v", relay, tt.want)
			}
		})
	}
}

func TestRelayEntry_EmptyOfficialIsError(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, `{"models":[]}`, 0)
	srv := idStub(t, "m1")
	if _, err := catalog.Build(context.Background(), buildOpts(t, codex, t.TempDir(), source("1", "主力", srv.URL))); err == nil {
		t.Fatal("没有官方条目就没有底板，应报错")
	}
}

func TestRelayEntry_HiddenOfficialSameNameStaysHiddenAndNotShown(t *testing.T) {
	t.Parallel()
	res, models := buildOne(t, paramsOfficialJSON,
		`{"id":"gpt-h","context_length":300000}`, `{"id":"gpt-x"}`, `{"id":"gpt-y"}`, `{"id":"model-a"}`)

	tests := []struct {
		slug       string
		visibility string
		shown      bool
	}{
		{"relay-gpt-h", "hide", false},
		{"relay-gpt-x", "list", true},
		{"relay-gpt-y", "list", true},
		{"relay-model-a", "list", true},
	}
	shown := map[string]bool{}
	for _, s := range res.RelayShown {
		shown[s] = true
	}
	inModels := map[string]bool{}
	for _, s := range res.RelayModels {
		inModels[s] = true
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := bySlug(t, models, tt.slug)["visibility"]; got != tt.visibility {
				t.Errorf("visibility = %v, want %s", got, tt.visibility)
			}
			if _, ok := res.Routes[tt.slug]; !ok {
				t.Errorf("Routes 里没有 %s", tt.slug)
			}
			if !inModels[tt.slug] {
				t.Errorf("RelayModels 里没有 %s", tt.slug)
			}
			if shown[tt.slug] != tt.shown {
				t.Errorf("在 RelayShown 里 = %v, want %v", shown[tt.slug], tt.shown)
			}
		})
	}
	if got := relayResult(t, res, "1").ModelCount; got != 3 {
		t.Errorf("ModelCount = %d, want 3（隐藏的 gpt-h 不计）", got)
	}
	if got := bySlug(t, models, "relay-gpt-h")["context_window"]; got != float64(300000) {
		t.Errorf("隐藏条目的上下文 = %v，仍应用接口值 300000", got)
	}
}

func TestRelayEntry_RoutesRelayModelsAndShownAcrossConfigs(t *testing.T) {
	t.Parallel()
	codex, _ := writeFakeCodex(t, paramsOfficialJSON, 0)
	a := newRelayStub(t, 200, modelsBody(
		`{"id":"auto"}`,
		`{"id":"gpt-h"}`,
		`{"id":"gpt-x","reasoning":{"supported_efforts":["low","max"]}}`))
	b := newRelayStub(t, 200, modelsBody(
		`{"id":"deepseek-v3.2","reasoning":{"supported_efforts":["high"]}}`,
		`{"id":"gpt-y"}`))
	res := build(t, buildOpts(t, codex, t.TempDir(), source("1", "主力", a.URL), source("2", "备用", b.URL)))
	models := roundtrip(t, res.Models)

	wantSlugs := []any{"gpt-x", "gpt-h", "gpt-y", "gpt-m", "relay-auto", "relay-gpt-h", "relay-gpt-x", "relay2-deepseek-v3.2", "relay2-gpt-y"}
	if got := slugsOf(models); !reflect.DeepEqual(got, wantSlugs) {
		t.Fatalf("slugs = %v, want %v", got, wantSlugs)
	}
	for i, m := range models[4:] {
		if m["priority"] != float64(10+i) {
			t.Errorf("%v priority = %v, want %d", m["slug"], m["priority"], 10+i)
		}
	}
	if got := bySlug(t, models, "relay-gpt-x")["display_name"]; got != "主力 · gpt-x" {
		t.Errorf("display_name = %v", got)
	}
	wantRoutes := map[string]catalog.RelayRoute{
		"relay-auto":           {ID: "1", Model: "auto"},
		"relay-gpt-h":          {ID: "1", Model: "gpt-h"},
		"relay-gpt-x":          {ID: "1", Model: "gpt-x", UltraToMax: true},
		"relay2-deepseek-v3.2": {ID: "2", Model: "deepseek-v3.2"},
		"relay2-gpt-y":         {ID: "2", Model: "gpt-y"},
	}
	if !reflect.DeepEqual(res.Routes, wantRoutes) {
		t.Errorf("Routes = %+v, want %+v", res.Routes, wantRoutes)
	}
	wantAll := []string{"relay-auto", "relay-gpt-h", "relay-gpt-x", "relay2-deepseek-v3.2", "relay2-gpt-y"}
	if !reflect.DeepEqual(res.RelayModels, wantAll) {
		t.Errorf("RelayModels = %v, want %v", res.RelayModels, wantAll)
	}
	if want := []string{"relay-auto", "relay-gpt-x", "relay2-deepseek-v3.2", "relay2-gpt-y"}; !reflect.DeepEqual(res.RelayShown, want) {
		t.Errorf("RelayShown = %v, want %v", res.RelayShown, want)
	}
}

func TestRelayEntry_SameNameFromOfficialUsesNoUltraRewrite(t *testing.T) {
	t.Parallel()
	const official = `{"models":[{"slug":"gpt-x","priority":3,"supported_reasoning_levels":[{"effort":"high","description":"h"},{"effort":"max","description":"m"},{"effort":"ultra","description":"u"}],"default_reasoning_level":"max"}]}`
	res, models := buildOne(t, official, `{"id":"gpt-x"}`)
	e := bySlug(t, models, "relay-gpt-x")
	if got, want := efforts(t, e), []any{"high", "max", "ultra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("efforts = %v, want %v（取自官方，原样）", got, want)
	}
	if e["default_reasoning_level"] != "max" {
		t.Errorf("default_reasoning_level = %v, want max", e["default_reasoning_level"])
	}
	if res.Routes["relay-gpt-x"].UltraToMax {
		t.Error("等级取自官方时 UltraToMax 应为 false")
	}
	if want := []string{"high", "max", "ultra"}; !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v", res.ReasoningLevels, want)
	}
}
