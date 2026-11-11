package catalog_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"modelbridge/internal/catalog"
)

func strp(s string) *string { return &s }

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

func officialFixture() []catalog.Entry {
	return []catalog.Entry{
		{
			"slug": "gpt-a", "display_name": "GPT A", "priority": 3,
			"base_instructions": "keep-me", "shell_type": "shell_command",
			"truncation_policy":    map[string]any{"mode": "tokens", "limit": 10000},
			"supports_search_tool": true, "web_search_tool_type": "text",
			"upgrade":                      map[string]any{"model": "gpt-z"},
			"availability_nux":             map[string]any{"message": "hi"},
			"model_messages":               map[string]any{"a": "b"},
			"service_tiers":                []any{"fast"},
			"additional_speed_tiers":       []any{"fast"},
			"experimental_supported_tools": []any{"t"},
			"visibility":                   "hide", "supported_in_api": false,
			"context_window": 1, "max_context_window": 1,
			"input_modalities": []any{"text"},
			"description":      "官方",
		},
		{"slug": "gpt-b", "priority": 7},
		{"slug": "gpt-c", "priority": 5},
	}
}

var mergeParams = []catalog.Param{
	{ID: "auto", ContextWindow: 1000000, InputModalities: []string{"text", "image"}, ReasoningLevels: []string{}, DefaultReasoningLevel: nil, Source: "s"},
	{ID: "deepseek-v3.2", ContextWindow: 128000, InputModalities: []string{"text"}, ReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: strp("high"), Source: "s"},
}

func TestMerge_OfficialFirstUnchangedThenRelayWithConsecutivePriority(t *testing.T) {
	t.Parallel()
	res, err := catalog.Merge(officialFixture(), mergeParams)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got := roundtrip(t, res.Models)
	want := roundtrip(t, officialFixture())
	if len(got) != 5 {
		t.Fatalf("len = %d", len(got))
	}
	if !reflect.DeepEqual(got[:3], want) {
		t.Errorf("官方条目被改动或顺序变化")
	}
	if got[3]["slug"] != "auto" || got[4]["slug"] != "deepseek-v3.2" {
		t.Errorf("中台条目顺序不对: %v %v", got[3]["slug"], got[4]["slug"])
	}
	if got[3]["priority"] != float64(8) || got[4]["priority"] != float64(9) {
		t.Errorf("priority = %v, %v，应从官方最大值 7 之后连续递增", got[3]["priority"], got[4]["priority"])
	}
}

func TestMerge_RelayEntryOverridesAndKeepsBase(t *testing.T) {
	t.Parallel()
	res, err := catalog.Merge(officialFixture(), mergeParams)
	if err != nil {
		t.Fatal(err)
	}
	e := roundtrip(t, res.Models)[4]

	exact := map[string]any{
		"slug": "deepseek-v3.2", "display_name": "deepseek-v3.2", "description": "中台模型",
		"visibility": "list", "supported_in_api": true,
		"context_window": float64(128000), "max_context_window": float64(128000),
		"input_modalities":                  []any{"text"},
		"default_reasoning_level":           "high",
		"supports_reasoning_effort_updates": true,
		"supports_search_tool":              false,
		"web_search_tool_type":              nil,
		"experimental_supported_tools":      []any{},
		"service_tiers":                     []any{},
		"additional_speed_tiers":            []any{},
		"upgrade":                           nil, "availability_nux": nil, "model_messages": nil,
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
}

func TestMerge_EmptyReasoningLevels(t *testing.T) {
	t.Parallel()
	res, err := catalog.Merge(officialFixture(), mergeParams[:1])
	if err != nil {
		t.Fatal(err)
	}
	e := roundtrip(t, res.Models)[3]
	levels, ok := e["supported_reasoning_levels"].([]any)
	if !ok || len(levels) != 0 {
		t.Errorf("supported_reasoning_levels = %#v，应为空数组", e["supported_reasoning_levels"])
	}
	if e["supports_reasoning_effort_updates"] != false {
		t.Errorf("supports_reasoning_effort_updates = %v", e["supports_reasoning_effort_updates"])
	}
	if v, ok := e["default_reasoning_level"]; !ok || v != nil {
		t.Errorf("default_reasoning_level = %#v，应为 null", v)
	}
}

func TestMerge_DoesNotMutateOfficial(t *testing.T) {
	t.Parallel()
	off := officialFixture()
	if _, err := catalog.Merge(off, mergeParams); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundtrip(t, off), roundtrip(t, officialFixture())) {
		t.Errorf("Merge 改动了传入的官方条目")
	}
}

func TestMerge_NoRelayYieldsOnlyOfficial(t *testing.T) {
	t.Parallel()
	res, err := catalog.Merge(officialFixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Models) != 3 || len(res.ReasoningLevels) != 0 {
		t.Errorf("models=%d levels=%v", len(res.Models), res.ReasoningLevels)
	}
}

func TestMerge_EmptyOfficialIsError(t *testing.T) {
	t.Parallel()
	if _, err := catalog.Merge(nil, mergeParams); err == nil {
		t.Fatal("没有官方条目就没有底板，应报错")
	}
}

func TestMerge_ReasoningLevelsSummary(t *testing.T) {
	t.Parallel()
	params := []catalog.Param{
		{ID: "m1", ContextWindow: 1, InputModalities: []string{"text"}, ReasoningLevels: []string{"low", "high"}, Source: "s"},
		{ID: "m2", ContextWindow: 1, InputModalities: []string{"text"}, ReasoningLevels: []string{"high", "xhigh"}, Source: "s"},
		{ID: "m3", ContextWindow: 1, InputModalities: []string{"text"}, ReasoningLevels: nil, Source: "s"},
	}
	res, err := catalog.Merge(officialFixture(), params)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"low", "high", "xhigh"}
	if !reflect.DeepEqual(res.ReasoningLevels, want) {
		t.Errorf("ReasoningLevels = %v, want %v（按首次出现顺序去重）", res.ReasoningLevels, want)
	}
}

func TestMerge_NegativeOfficialPriority(t *testing.T) {
	t.Parallel()
	off := []catalog.Entry{{"slug": "a", "priority": -5}, {"slug": "b", "priority": -3}}
	res, err := catalog.Merge(off, mergeParams)
	if err != nil {
		t.Fatal(err)
	}
	got := roundtrip(t, res.Models)
	if got[2]["priority"] != float64(-2) || got[3]["priority"] != float64(-1) {
		t.Errorf("priority = %v, %v，应从 -2 开始", got[2]["priority"], got[3]["priority"])
	}
}
