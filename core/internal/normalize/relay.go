package normalize

import (
	"encoding/json"
)

type itemHead struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	EncryptedContent json.RawMessage `json:"encrypted_content"`
	CallID           string          `json:"call_id"`
	Name             string          `json:"name"`
	Output           json.RawMessage `json:"output"`
}

func (h itemHead) hasEncrypted() bool {
	return len(h.EncryptedContent) > 0 && string(h.EncryptedContent) != "null"
}

func summaryMessage(summary string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"type": "message",
		"role": "user",
		"content": []any{map[string]any{
			"type": "input_text",
			"text": "以下是之前对话的摘要：" + summary,
		}},
	})
	return b
}

func userText(text string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"type":    "message",
		"role":    "user",
		"content": []any{map[string]any{"type": "input_text", "text": text}},
	})
	return b
}

func outputText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func isArray(raw json.RawMessage) bool {
	return len(raw) > 0 && raw[0] == '['
}

func ForRelay(body []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, err
	}
	changed := false
	if raw, ok := top["input"]; ok && isArray(raw) {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		kept := make([]json.RawMessage, 0, len(items))
		for _, it := range items {
			var h itemHead
			_ = json.Unmarshal(it, &h)
			switch h.Type {
			case "reasoning":
				if h.hasEncrypted() {
					changed = true
					continue
				}
			case "function_call_output":
				// ChatGPT 桌面端插入的伴随页面状态没有 call_id，中转按 Responses 规范校验会拒收
				if h.CallID == "" {
					kept = append(kept, userText("以下是 "+h.Name+" 的结果："+outputText(h.Output)))
					changed = true
					continue
				}
			case "context_compaction":
				changed = true
				continue
			case "compaction":
				if s, ok := ParseCompactionItem(it); ok {
					kept = append(kept, summaryMessage(s))
				}
				changed = true
				continue
			}
			kept = append(kept, it)
		}
		if changed {
			top["input"], _ = json.Marshal(kept)
		}
	}
	if raw, ok := top["tools"]; ok {
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, err
		}
		kept := make([]json.RawMessage, 0, len(tools))
		names := map[string]bool{}
		for _, t := range tools {
			var h itemHead
			_ = json.Unmarshal(t, &h)
			if h.Type == "function" {
				kept = append(kept, t)
				names[h.Name] = true
			}
		}
		if len(kept) != len(tools) {
			changed = true
			top["tools"], _ = json.Marshal(kept)
			if toolChoiceUnusable(top["tool_choice"], names) {
				delete(top, "tool_choice")
			}
		}
	}
	if !changed {
		return body, nil
	}
	return json.Marshal(top)
}

func toolChoiceUnusable(raw json.RawMessage, names map[string]bool) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "required" && len(names) == 0
	}
	var h itemHead
	if json.Unmarshal(raw, &h) != nil {
		return false
	}
	return h.Type != "function" || !names[h.Name]
}
