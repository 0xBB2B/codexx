package normalize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"modelbridge/internal/normalize"
)

func decodeMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("不是合法 JSON：%v\n%s", err, b)
	}
	return m
}

func inputItems(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	raw, _ := decodeMap(t, b)["input"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		items = append(items, v.(map[string]any))
	}
	return items
}

func toolTypes(t *testing.T, b []byte) []string {
	t.Helper()
	raw, _ := decodeMap(t, b)["tools"].([]any)
	var types []string
	for _, v := range raw {
		types = append(types, v.(map[string]any)["type"].(string))
	}
	return types
}

const (
	visibleMsg = `{"type":"message","role":"user","content":[{"type":"input_text","text":"你好"}]}`
	callItem   = `{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{}"}`
	callOutput = `{"type":"function_call_output","call_id":"call_1","output":"ok"}`
	officialRs = `{"type":"reasoning","id":"rs_official","encrypted_content":"ENC","summary":[]}`
	openaiCmp  = `{"type":"compaction","encrypted_content":"OPENAI_BLOB"}`
)

func TestForRelay(t *testing.T) {
	t.Parallel()
	ownCmp := string(normalize.NewCompactionItem("摘要正文"))
	summaryMsg := `{"type":"message","role":"user","content":[{"type":"input_text","text":"以下是之前对话的摘要：摘要正文"}]}`

	tests := []struct {
		name      string
		input     []string
		wantInput []string
	}{
		{"去掉官方加密 reasoning", []string{visibleMsg, officialRs, callItem}, []string{visibleMsg, callItem}},
		{"去掉 OpenAI compaction", []string{openaiCmp, visibleMsg}, []string{visibleMsg}},
		{"本工具摘要变成用户消息", []string{ownCmp, visibleMsg}, []string{summaryMsg, visibleMsg}},
		{"工具调用记录原样保留且顺序不变", []string{visibleMsg, callItem, callOutput}, []string{visibleMsg, callItem, callOutput}},
		{"不带 encrypted_content 的 reasoning 保留", []string{`{"type":"reasoning","id":"rs_x","summary":[]}`}, []string{`{"type":"reasoning","id":"rs_x","summary":[]}`}},
		{"encrypted_content 不是合法 JSON 的 compaction 当作非本工具生成", []string{`{"type":"compaction","encrypted_content":"not-json"}`, visibleMsg}, []string{visibleMsg}},
		{"JSON 的 type 不是本工具标记的 compaction 被去掉", []string{`{"type":"compaction","encrypted_content":"{\"type\":\"other\"}"}`, visibleMsg}, []string{visibleMsg}},
		{"去掉 context_compaction", []string{`{"type":"context_compaction","encrypted_content":"CTX"}`, visibleMsg}, []string{visibleMsg}},
		{"缺 call_id 的工具结果变成用户消息",
			[]string{`{"type":"function_call_output","id":"fco_1","namespace":"agent","name":"codex_apps_open_page","output":"{\"page_id\":null}"}`, visibleMsg},
			[]string{`{"type":"message","role":"user","content":[{"type":"input_text","text":"以下是 codex_apps_open_page 的结果：{\"page_id\":null}"}]}`, visibleMsg}},
		{"call_id 为空串的工具结果变成用户消息",
			[]string{`{"type":"function_call_output","call_id":"","name":"page","output":"状态"}`},
			[]string{`{"type":"message","role":"user","content":[{"type":"input_text","text":"以下是 page 的结果：状态"}]}`}},
		{"工具结果不是字符串时原样写成 JSON 文本",
			[]string{`{"type":"function_call_output","name":"page","output":[{"type":"input_text","text":"a"}]}`},
			[]string{`{"type":"message","role":"user","content":[{"type":"input_text","text":"以下是 page 的结果：[{\"type\":\"input_text\",\"text\":\"a\"}]"}]}`}},
		{"空 input", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"model":"relay-model-a","instructions":"请帮忙","input":[` + strings.Join(tc.input, ",") + `]}`
			out, err := normalize.ForRelay([]byte(body))
			if err != nil {
				t.Fatalf("返回错误：%v", err)
			}
			got := inputItems(t, out)
			if len(got) != len(tc.wantInput) {
				t.Fatalf("input 条数 = %d，期望 %d\n%s", len(got), len(tc.wantInput), out)
			}
			for i, w := range tc.wantInput {
				gb, _ := json.Marshal(got[i])
				var wm map[string]any
				_ = json.Unmarshal([]byte(w), &wm)
				wb, _ := json.Marshal(wm)
				if string(gb) != string(wb) {
					t.Fatalf("input[%d] = %s，期望 %s", i, gb, wb)
				}
			}
			m := decodeMap(t, out)
			if m["model"] != "relay-model-a" || m["instructions"] != "请帮忙" {
				t.Fatalf("其他顶层字段被改动：%s", out)
			}
		})
	}
}

func TestForRelay_Tools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tools string
		want  []string
	}{
		{"只保留 function", `[{"type":"function","name":"shell"},{"type":"web_search"},{"type":"image_generation"},{"type":"function","name":"read"}]`, []string{"function", "function"}},
		{"全是非 function 时剩空数组", `[{"type":"web_search"}]`, nil},
		{"空 tools", `[]`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := normalize.ForRelay([]byte(`{"model":"relay-model-a","input":[],"tools":` + tc.tools + `}`))
			if err != nil {
				t.Fatalf("返回错误：%v", err)
			}
			got := toolTypes(t, out)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("tools 类型 = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestForRelay_ToolChoice(t *testing.T) {
	t.Parallel()
	const (
		fnShell = `{"type":"function","name":"shell"}`
		webTool = `{"type":"web_search"}`
	)
	tests := []struct {
		name       string
		tools      string
		toolChoice string
		wantKept   bool
	}{
		{"指向被删掉的 web_search 工具", `[` + fnShell + `,` + webTool + `]`, `{"type":"web_search"}`, false},
		{"tools 变空且 required", `[` + webTool + `]`, `"required"`, false},
		{"tools 变空且指定某个函数", `[` + webTool + `]`, `{"type":"function","name":"shell"}`, false},
		{"tools 变空但 auto 保持不变", `[` + webTool + `]`, `"auto"`, true},
		{"tools 变空但 none 保持不变", `[` + webTool + `]`, `"none"`, true},
		{"指向保留下来的函数保持不变", `[` + fnShell + `,` + webTool + `]`, `{"type":"function","name":"shell"}`, true},
		{"tools 还剩函数时 required 保持不变", `[` + fnShell + `,` + webTool + `]`, `"required"`, true},
		{"没有被过滤的工具时 required 保持不变", `[` + fnShell + `]`, `"required"`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"model":"relay-model-a","input":[],"tools":` + tc.tools + `,"tool_choice":` + tc.toolChoice + `}`
			out, err := normalize.ForRelay([]byte(body))
			if err != nil {
				t.Fatalf("返回错误：%v", err)
			}
			got, has := decodeMap(t, out)["tool_choice"]
			if has != tc.wantKept {
				t.Fatalf("tool_choice 保留 = %v，期望 %v\n%s", has, tc.wantKept, out)
			}
			if has {
				gb, _ := json.Marshal(got)
				var want any
				_ = json.Unmarshal([]byte(tc.toolChoice), &want)
				wb, _ := json.Marshal(want)
				if string(gb) != string(wb) {
					t.Fatalf("tool_choice = %s，期望原样 %s", gb, wb)
				}
			}
		})
	}
}

func TestForRelay_KeepsFunctionToolFields(t *testing.T) {
	t.Parallel()
	out, err := normalize.ForRelay([]byte(`{"input":[],"tools":[{"type":"function","name":"shell","parameters":{"type":"object"}},{"type":"web_search"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"parameters"`) || !strings.Contains(string(out), `"shell"`) {
		t.Fatalf("function 工具字段丢失：%s", out)
	}
}

func TestForRelay_InvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := normalize.ForRelay([]byte(`not json`)); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

func TestForRelay_NonArrayInputKept(t *testing.T) {
	t.Parallel()
	out, err := normalize.ForRelay([]byte(`{"input":"hi","tools":[{"type":"web_search"},{"type":"function","name":"f"}]}`))
	if err != nil {
		t.Fatalf("input 不是数组时不应报错：%v", err)
	}
	if decodeMap(t, out)["input"] != "hi" {
		t.Fatalf("input 应原样保留：%s", out)
	}
	if got := toolTypes(t, out); strings.Join(got, ",") != "function" {
		t.Fatalf("tools 类型 = %v", got)
	}
}
