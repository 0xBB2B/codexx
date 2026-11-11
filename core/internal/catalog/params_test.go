package catalog_test

import (
	"testing"

	"modelbridge/internal/catalog"
)

func TestBuiltinParams_EverySourceNonEmptyAndLevelsValid(t *testing.T) {
	t.Parallel()
	params, err := catalog.BuiltinParams()
	if err != nil {
		t.Fatalf("BuiltinParams: %v", err)
	}
	if len(params) == 0 {
		t.Fatal("内置参数表为空")
	}
	valid := map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	for _, p := range params {
		t.Run(p.ID, func(t *testing.T) {
			if p.Source == "" {
				t.Errorf("source 为空")
			}
			for _, l := range p.ReasoningLevels {
				if !valid[l] {
					t.Errorf("非法等级 %q", l)
				}
			}
		})
	}
}

func TestBuiltinParams_ContainsAuto(t *testing.T) {
	t.Parallel()
	params, err := catalog.BuiltinParams()
	if err != nil {
		t.Fatalf("BuiltinParams: %v", err)
	}
	var auto *catalog.Param
	for i := range params {
		if params[i].ID == "auto" {
			auto = &params[i]
		}
	}
	if auto == nil {
		t.Fatal("参数表缺少 auto")
	}
	if auto.ContextWindow != 1000000 {
		t.Errorf("context_window = %d", auto.ContextWindow)
	}
	if len(auto.InputModalities) != 2 || auto.InputModalities[0] != "text" || auto.InputModalities[1] != "image" {
		t.Errorf("input_modalities = %v", auto.InputModalities)
	}
	if len(auto.ReasoningLevels) != 0 {
		t.Errorf("reasoning_levels = %v", auto.ReasoningLevels)
	}
	if auto.DefaultReasoningLevel != nil {
		t.Errorf("default_reasoning_level = %v, want nil", *auto.DefaultReasoningLevel)
	}
}

func TestLoadParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"全部合法等级", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":["none","minimal","low","medium","high","xhigh","max"],"default_reasoning_level":"medium","source":"https://example.com/doc"}]}`, false},
		{"默认等级为 null", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":[],"default_reasoning_level":null,"source":"s"}]}`, false},
		{"非法等级", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":["low","ultra"],"default_reasoning_level":"low","source":"s"}]}`, true},
		{"缺 source", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":[],"default_reasoning_level":null}]}`, true},
		{"空 source", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":[],"default_reasoning_level":null,"source":""}]}`, true},
		{"默认等级非法", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":["low"],"default_reasoning_level":"ultra","source":"s"}]}`, true},
		{"默认等级合法但不在列表里", `{"models":[{"id":"m","context_window":1000,"input_modalities":["text"],"reasoning_levels":["low"],"default_reasoning_level":"high","source":"s"}]}`, true},
		{"不是 JSON", `not json`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := catalog.LoadParams([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(params) != 1 {
				t.Fatalf("len = %d", len(params))
			}
		})
	}
}

func TestLoadParams_ParsesFields(t *testing.T) {
	t.Parallel()
	params, err := catalog.LoadParams([]byte(`{"models":[{"id":"deepseek-v3.2","context_window":128000,"input_modalities":["text"],"reasoning_levels":["low","high"],"default_reasoning_level":"high","source":"s"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := params[0]
	if p.ID != "deepseek-v3.2" || p.ContextWindow != 128000 || len(p.ReasoningLevels) != 2 || p.ReasoningLevels[1] != "high" {
		t.Errorf("解析结果不对: %+v", p)
	}
	if p.DefaultReasoningLevel == nil || *p.DefaultReasoningLevel != "high" {
		t.Errorf("default_reasoning_level 不对")
	}
}
