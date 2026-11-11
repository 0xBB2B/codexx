package normalize

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	SummaryInstructions = "请为另一位编程助手总结下面的对话。保留目标、决定、约束、仓库状态、改过的文件、测试结果、失败、进行中的工作和下一步。只输出总结正文。"

	compactionMarker = "model_bridge_compaction"
)

func NewCompactionItem(summary string) []byte {
	inner, _ := json.Marshal(map[string]string{"type": compactionMarker, "summary": summary})
	b, _ := json.Marshal(map[string]string{"type": "compaction", "encrypted_content": string(inner)})
	return b
}

func ParseCompactionItem(item []byte) (string, bool) {
	var outer struct {
		Type             string `json:"type"`
		EncryptedContent string `json:"encrypted_content"`
	}
	if json.Unmarshal(item, &outer) != nil || outer.Type != "compaction" {
		return "", false
	}
	var inner struct {
		Type    string `json:"type"`
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(outer.EncryptedContent), &inner) != nil || inner.Type != compactionMarker {
		return "", false
	}
	return inner.Summary, true
}

func HasCompactionTrigger(body []byte) bool {
	var v struct {
		Input []itemHead `json:"input"`
	}
	if json.Unmarshal(body, &v) != nil || len(v.Input) == 0 {
		return false
	}
	return v.Input[len(v.Input)-1].Type == "compaction_trigger"
}

func SummaryRequest(body []byte) ([]byte, error) {
	cleaned, err := ForRelay(body)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &top); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if raw, ok := top["input"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		var h itemHead
		_ = json.Unmarshal(it, &h)
		if h.Type != "compaction_trigger" {
			kept = append(kept, it)
		}
	}
	top["input"], _ = json.Marshal(kept)
	top["stream"] = json.RawMessage("false")
	top["instructions"], _ = json.Marshal(SummaryInstructions)
	delete(top, "tools")
	delete(top, "tool_choice")
	return json.Marshal(top)
}

func SummaryFromResponse(body []byte) (string, error) {
	var v struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, o := range v.Output {
		for _, c := range o.Content {
			if c.Type == "output_text" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() == 0 {
		return "", errors.New("empty summary")
	}
	return sb.String(), nil
}

func CompactionStream(summary string) []byte {
	item := json.RawMessage(NewCompactionItem(summary))
	resp := func(status string, output []json.RawMessage) map[string]any {
		return map[string]any{"id": "resp_model_bridge", "object": "response", "status": status, "output": output}
	}
	events := []struct {
		name string
		data map[string]any
	}{
		{"response.created", map[string]any{"type": "response.created", "response": resp("in_progress", []json.RawMessage{})}},
		{"response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}},
		{"response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}},
		{"response.completed", map[string]any{"type": "response.completed", "response": resp("completed", []json.RawMessage{item})}},
	}
	var sb strings.Builder
	for _, e := range events {
		b, _ := json.Marshal(e.data)
		sb.WriteString("event: " + e.name + "\ndata: " + string(b) + "\n\n")
	}
	return []byte(sb.String())
}

func CompactionOutput(summary string) []byte {
	b, _ := json.Marshal(map[string]any{"output": []json.RawMessage{NewCompactionItem(summary)}})
	return b
}
