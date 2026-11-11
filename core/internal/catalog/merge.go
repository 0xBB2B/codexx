package catalog

import (
	"errors"
	"maps"
	"time"
)

type Result struct {
	Models          []Entry
	ReasoningLevels []string
	RelayErr        error
	StoreErr        error
	RelayFetchedAt  time.Time
}

func SelectRelay(ids []string, params []Param) []Param {
	byID := make(map[string]Param, len(params))
	for _, p := range params {
		byID[p.ID] = p
	}
	var out []Param
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

func Merge(official []Entry, relay []Param) (*Result, error) {
	if len(official) == 0 {
		return nil, errors.New("没有官方模型条目，无法作为底板")
	}
	maxPriority, _ := toFloat(official[0]["priority"])
	for _, e := range official {
		if p, ok := toFloat(e["priority"]); ok && p > maxPriority {
			maxPriority = p
		}
	}

	models := append([]Entry(nil), official...)
	var levels []string
	seen := map[string]bool{}
	for i, p := range relay {
		e := Entry(maps.Clone(official[0]))
		supported := make([]map[string]any, 0, len(p.ReasoningLevels))
		for _, l := range p.ReasoningLevels {
			supported = append(supported, map[string]any{"effort": l, "description": l})
			if !seen[l] {
				seen[l] = true
				levels = append(levels, l)
			}
		}
		var def any
		if p.DefaultReasoningLevel != nil {
			def = *p.DefaultReasoningLevel
		}
		e["slug"] = p.ID
		e["display_name"] = p.ID
		e["description"] = "中台模型"
		e["visibility"] = "list"
		e["supported_in_api"] = true
		e["priority"] = maxPriority + 1 + float64(i)
		e["context_window"] = p.ContextWindow
		e["max_context_window"] = p.ContextWindow
		e["input_modalities"] = p.InputModalities
		e["supported_reasoning_levels"] = supported
		e["default_reasoning_level"] = def
		e["supports_reasoning_effort_updates"] = len(p.ReasoningLevels) > 0
		e["supports_search_tool"] = false
		e["web_search_tool_type"] = nil
		e["experimental_supported_tools"] = []any{}
		e["service_tiers"] = []any{}
		e["additional_speed_tiers"] = []any{}
		e["upgrade"] = nil
		e["availability_nux"] = nil
		e["model_messages"] = nil
		models = append(models, e)
	}
	return &Result{Models: models, ReasoningLevels: levels}, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
