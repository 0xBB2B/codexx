package catalog

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"time"
)

const (
	RelaySlugPrefix   = "relay-"
	autoContextWindow = 1000000
)

type Result struct {
	Models          []Entry
	ReasoningLevels []string
	// RelayModels、RelayShown 里放的是内部名（relay-xxx）
	RelayModels  []string
	RelayShown   []string
	Relays       []RelayResult
	Routes       map[string]RelayRoute
	DefaultModel string
	StoreErr     error
}

type RelayResult struct {
	ID         string
	ModelCount int
	FetchedAt  time.Time
	Stale      bool
	Err        error
}

type RelayRoute struct {
	ID    string
	Model string
}

type RelayGroup struct {
	ID     string
	Name   string
	Models []RelayModel
}

const autoModel = "auto"

var levelRank = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

func MergeRelay(official []Entry, groups []RelayGroup) (*Result, error) {
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
	hidden := hiddenSlugs(official)

	res := &Result{Models: append([]Entry(nil), official...), Routes: map[string]RelayRoute{}}
	seen := map[string]bool{}
	next := maxPriority + 1
	for _, g := range groups {
		for _, m := range g.Models {
			if m.ID == "" {
				continue
			}
			e := relayEntry(m, official[0], bySlug[m.ID], hidden[m.ID])
			slug := RelaySlug(g.ID, m.ID)
			applyRelayOverrides(e, slug, g.Name, m.ID, next)
			next++
			if list, ok := e["supported_reasoning_levels"].([]any); ok {
				for _, it := range list {
					if l, ok := it.(map[string]any)["effort"].(string); ok && !seen[l] {
						seen[l] = true
						res.ReasoningLevels = append(res.ReasoningLevels, l)
					}
				}
			}
			res.Models = append(res.Models, e)
			res.Routes[slug] = RelayRoute{ID: g.ID, Model: m.ID}
			res.RelayModels = append(res.RelayModels, slug)
			if !hidden[m.ID] {
				res.RelayShown = append(res.RelayShown, slug)
			}
		}
	}
	return res, nil
}

// sameName 为同名官方条目，没有时为 nil；缺参数时同名取它的值，不同名取 first 的值
func relayEntry(m RelayModel, first, sameName Entry, hidden bool) Entry {
	base := first
	if sameName != nil {
		base = sameName
	}
	e := Entry(maps.Clone(base))
	if hidden {
		e["visibility"] = base["visibility"]
	} else {
		e["visibility"] = "list"
	}

	if m.ID == autoModel {
		e["context_window"] = autoContextWindow
		e["max_context_window"] = autoContextWindow
		e["input_modalities"] = []string{"text", "image"}
		setNoLevels(e)
		return e
	}
	if m.ContextLength > 0 {
		e["context_window"] = m.ContextLength
		e["max_context_window"] = m.ContextLength
	}
	if modalities := knownModalities(m.InputModalities); len(modalities) > 0 {
		e["input_modalities"] = modalities
	}
	if m.Efforts == nil {
		if sameName == nil {
			setNoLevels(e)
		}
		return e
	}
	levels, def := relayLevels(m.Efforts)
	if len(levels) == 0 {
		setNoLevels(e)
		return e
	}
	supported := make([]any, 0, len(levels))
	for _, l := range levels {
		supported = append(supported, map[string]any{"effort": l, "description": l})
	}
	e["supported_reasoning_levels"] = supported
	e["default_reasoning_level"] = def
	e["supports_reasoning_effort_updates"] = true
	return e
}

// Codex 读目录时遇到不认识的输入类型会整份目录加载失败
func knownModalities(modalities []string) []string {
	var out []string
	for _, m := range modalities {
		if m == "text" || m == "image" || m == "audio" {
			out = append(out, m)
		}
	}
	return out
}

func setNoLevels(e Entry) {
	e["supported_reasoning_levels"] = []any{}
	delete(e, "default_reasoning_level")
	e["supports_reasoning_effort_updates"] = false
}

// 有 high 时默认 high，不按排序取最高
func relayLevels(efforts []string) (levels []string, def string) {
	best := -1
	for _, l := range efforts {
		rank := slices.Index(levelRank, l)
		if rank < 0 {
			continue
		}
		levels = append(levels, l)
		if l == "high" {
			def, best = l, len(levelRank)
		} else if rank > best {
			def, best = l, rank
		}
	}
	return levels, def
}

func hiddenSlugs(official []Entry) map[string]bool {
	hidden := map[string]bool{}
	for _, e := range official {
		if v, has := e["visibility"]; has && v != "list" {
			if slug, ok := e["slug"].(string); ok {
				hidden[slug] = true
			}
		}
	}
	return hidden
}

func RelaySlug(id, model string) string {
	if id == "1" {
		return RelaySlugPrefix + model
	}
	return "relay" + id + "-" + model
}

func relayDisplayName(configName, model string) string {
	return configName + " · " + model
}

func applyRelayOverrides(e Entry, slug, configName, model string, priority float64) {
	e["slug"] = slug
	e["display_name"] = relayDisplayName(configName, model)
	e["description"] = "中转模型"
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

func ShownRelayNames(models []RelayModel, official []Entry) []string {
	hidden := hiddenSlugs(official)
	var out []string
	for _, m := range models {
		if m.ID != "" && !hidden[m.ID] {
			out = append(out, m.ID)
		}
	}
	return out
}

func FilterOfficial(official []Entry, accountSlugs []string) []Entry {
	if len(accountSlugs) == 0 {
		return official
	}
	allowed := make(map[string]bool, len(accountSlugs))
	for _, slug := range accountSlugs {
		allowed[slug] = true
	}
	var out []Entry
	for _, e := range official {
		if slug, ok := e["slug"].(string); ok && allowed[slug] {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return official
	}
	return out
}

func IntersectOfficial(official []Entry, slugs []string) []Entry {
	allowed := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		allowed[slug] = true
	}
	var out []Entry
	for _, e := range official {
		if slug, ok := e["slug"].(string); ok && allowed[slug] {
			out = append(out, e)
		}
	}
	return out
}

func AccountSlugs(modelsCachePath string) []string {
	b, err := os.ReadFile(modelsCachePath)
	if err != nil {
		return nil
	}
	var file struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &file) != nil {
		return nil
	}
	var out []string
	for _, m := range file.Models {
		out = append(out, m.Slug)
	}
	return out
}
