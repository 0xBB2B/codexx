package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

//go:embed params.json
var builtinParams []byte

type Param struct {
	ID                    string   `json:"id"`
	ContextWindow         int      `json:"context_window"`
	InputModalities       []string `json:"input_modalities"`
	ReasoningLevels       []string `json:"reasoning_levels"`
	DefaultReasoningLevel *string  `json:"default_reasoning_level"`
	Source                string   `json:"source"`
}

var validLevels = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

func BuiltinParams() ([]Param, error) { return LoadParams(builtinParams) }

func LoadParams(data []byte) ([]Param, error) {
	var file struct {
		Models []Param `json:"models"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("参数表不是合法 JSON: %w", err)
	}
	for _, p := range file.Models {
		if p.Source == "" {
			return nil, fmt.Errorf("参数表条目 %q 缺少 source", p.ID)
		}
		for _, l := range p.ReasoningLevels {
			if !validLevels[l] {
				return nil, fmt.Errorf("参数表条目 %q 的思考等级 %q 非法", p.ID, l)
			}
		}
		if d := p.DefaultReasoningLevel; d != nil && !slices.Contains(p.ReasoningLevels, *d) {
			return nil, fmt.Errorf("参数表条目 %q 的默认思考等级 %q 不在 reasoning_levels 里", p.ID, *d)
		}
	}
	return file.Models, nil
}
