package catalog

import (
	"errors"
	"maps"
	"time"
)

const RelaySlugPrefix = "relay-"

type Result struct {
	Models          []Entry
	ReasoningLevels []string
	RelayModels     []string
	RelayShown      []string
	RelayErr        error
	StoreErr        error
	RelayFetchedAt  time.Time
}

func RelayNames(ids []string, params []Param, official []Entry) (paramOnly []Param, sameName []string) {
	slugs := make(map[string]bool, len(official))
	for _, e := range official {
		if slug, ok := e["slug"].(string); ok {
			slugs[slug] = true
		}
	}
	inRelay := make(map[string]bool, len(ids))
	for _, id := range ids {
		inRelay[id] = true
	}
	for _, p := range params {
		if inRelay[p.ID] && !slugs[p.ID] {
			paramOnly = append(paramOnly, p)
		}
	}
	seen := map[string]bool{}
	for _, e := range official {
		if slug, ok := e["slug"].(string); ok && inRelay[slug] && !seen[slug] {
			seen[slug] = true
			sameName = append(sameName, slug)
		}
	}
	return paramOnly, sameName
}

func MergeRelay(official []Entry, relay []Param, sameName []string) (*Result, error) {
	if len(official) == 0 {
		return nil, errors.New("没有官方模型条目，无法作为底板")
	}
	maxPriority, _ := toFloat(official[0]["priority"])
	bySlug := make(map[string]Entry, len(official))
	for _, e := range official {
		if p, ok := toFloat(e["priority"]); ok && p > maxPriority {
			maxPriority = p
		}
		if slug, ok := e["slug"].(string); ok {
			if _, dup := bySlug[slug]; !dup {
				bySlug[slug] = e
			}
		}
	}

	models := append([]Entry(nil), official...)
	var levels []string
	seen := map[string]bool{}
	addLevel := func(l string) {
		if !seen[l] {
			seen[l] = true
			levels = append(levels, l)
		}
	}
	for i, p := range relay {
		e := Entry(maps.Clone(official[0]))
		supported := make([]map[string]any, 0, len(p.ReasoningLevels))
		for _, l := range p.ReasoningLevels {
			supported = append(supported, map[string]any{"effort": l, "description": l})
			addLevel(l)
		}
		e["context_window"] = p.ContextWindow
		e["max_context_window"] = p.ContextWindow
		e["input_modalities"] = p.InputModalities
		e["supported_reasoning_levels"] = supported
		e["visibility"] = "list"
		if p.DefaultReasoningLevel != nil {
			e["default_reasoning_level"] = *p.DefaultReasoningLevel
		} else {
			delete(e, "default_reasoning_level")
		}
		e["supports_reasoning_effort_updates"] = len(p.ReasoningLevels) > 0
		applyRelayOverrides(e, p.ID, maxPriority+1+float64(i))
		models = append(models, e)
	}
	for j, name := range sameName {
		base := bySlug[name]
		e := Entry(maps.Clone(base))
		if list, ok := base["supported_reasoning_levels"].([]any); ok {
			for _, it := range list {
				if m, ok := it.(map[string]any); ok {
					if l, ok := m["effort"].(string); ok {
						addLevel(l)
					}
				}
			}
		}
		applyRelayOverrides(e, name, maxPriority+1+float64(len(relay)+j))
		models = append(models, e)
	}
	return &Result{Models: models, ReasoningLevels: levels}, nil
}

func applyRelayOverrides(e Entry, name string, priority float64) {
	e["slug"] = RelaySlugPrefix + name
	e["display_name"] = name + "（中台）"
	e["description"] = "中台模型"
	e["supported_in_api"] = true
	e["priority"] = priority
	e["supports_search_tool"] = false
	e["experimental_supported_tools"] = []any{}
	e["service_tiers"] = []any{}
	e["additional_speed_tiers"] = []any{}
	// 新版 Codex 不接受 web_search_tool_type 为 null；不写字段时 Codex 会用自己的默认值。
	for _, k := range []string{"web_search_tool_type", "upgrade", "availability_nux", "model_messages"} {
		delete(e, k)
	}
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

func ShownRelayNames(paramOnly []Param, sameName []string, official []Entry) []string {
	hidden := map[string]bool{}
	for _, e := range official {
		if v, has := e["visibility"]; has && v != "list" {
			slug, _ := e["slug"].(string)
			hidden[slug] = true
		}
	}
	var out []string
	for _, p := range paramOnly {
		out = append(out, p.ID)
	}
	for _, n := range sameName {
		if !hidden[n] {
			out = append(out, n)
		}
	}
	return out
}
