package normalize

import (
	"bytes"
	"encoding/json"
	"strings"
)

func inputSpan(body []byte) (start, end int, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if _, err = dec.Token(); err != nil {
		return 0, 0, false, err
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, false, err
		}
		if key == "input" {
			end = int(dec.InputOffset())
			start, found = end-len(raw), true
		}
	}
	return start, end, found, nil
}

func ForOfficial(body []byte, ids *ReasoningIDs) ([]byte, bool, error) {
	start, end, found, err := inputSpan(body)
	if err != nil {
		return body, false, err
	}
	if !found || !strings.HasPrefix(string(body[start:end]), "[") {
		return body, false, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body[start:end], &items); err != nil {
		return body, false, err
	}
	changed := false
	var arr bytes.Buffer
	arr.WriteByte('[')
	for _, it := range items {
		var h itemHead
		_ = json.Unmarshal(it, &h)
		switch h.Type {
		case "reasoning":
			if h.ID != "" && ids.Has(h.ID) {
				changed = true
				continue
			}
		case "compaction":
			if s, ok := ParseCompactionItem(it); ok {
				it = summaryMessage(s)
				changed = true
			}
		}
		if arr.Len() > 1 {
			arr.WriteByte(',')
		}
		arr.Write(it)
	}
	arr.WriteByte(']')
	if !changed {
		return body, false, nil
	}
	out := make([]byte, 0, len(body)+arr.Len())
	out = append(out, body[:start]...)
	out = append(out, arr.Bytes()...)
	out = append(out, body[end:]...)
	return out, true, nil
}
