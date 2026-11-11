package normalize_test

import (
	"encoding/json"
	"strings"
	"testing"

	"modelbridge/internal/normalize"
)

func TestCompactionItem_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, summary := range []string{"摘要正文", "含 \"引号\" 与\n换行", ""} {
		item := normalize.NewCompactionItem(summary)
		m := decodeMap(t, item)
		if m["type"] != "compaction" {
			t.Fatalf("type = %v", m["type"])
		}
		enc, _ := m["encrypted_content"].(string)
		inner := decodeMap(t, []byte(enc))
		if inner["type"] != "model_bridge_compaction" || inner["summary"] != summary {
			t.Fatalf("encrypted_content = %s", enc)
		}
		got, ok := normalize.ParseCompactionItem(item)
		if !ok || got != summary {
			t.Fatalf("ParseCompactionItem = %q, %v", got, ok)
		}
	}
}

func TestParseCompactionItem_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, item string }{
		{"OpenAI 压缩", openaiCmp},
		{"内层 type 不符", `{"type":"compaction","encrypted_content":"{\"type\":\"other\",\"summary\":\"x\"}"}`},
		{"外层 type 不符", `{"type":"message","encrypted_content":"{\"type\":\"model_bridge_compaction\",\"summary\":\"x\"}"}`},
		{"缺 encrypted_content", `{"type":"compaction"}`},
		{"非 JSON", `nope`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := normalize.ParseCompactionItem([]byte(tc.item)); ok {
				t.Fatal("不应识别为本工具压缩条目")
			}
		})
	}
}

func TestHasCompactionTrigger(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"最后一条是 trigger", `{"input":[` + visibleMsg + `,{"type":"compaction_trigger"}]}`, true},
		{"trigger 不在最后", `{"input":[{"type":"compaction_trigger"},` + visibleMsg + `]}`, false},
		{"没有 trigger", `{"input":[` + visibleMsg + `]}`, false},
		{"空 input", `{"input":[]}`, false},
		{"没有 input", `{}`, false},
		{"非法 JSON", `nope`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalize.HasCompactionTrigger([]byte(tc.body)); got != tc.want {
				t.Fatalf("= %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestSummaryInstructions(t *testing.T) {
	t.Parallel()
	want := "请为另一位编程助手总结下面的对话。保留目标、决定、约束、仓库状态、改过的文件、测试结果、失败、进行中的工作和下一步。只输出总结正文。"
	if normalize.SummaryInstructions != want {
		t.Fatalf("SummaryInstructions = %q", normalize.SummaryInstructions)
	}
}

func TestSummaryRequest(t *testing.T) {
	t.Parallel()
	own := string(normalize.NewCompactionItem("旧摘要"))
	body := `{"model":"relay-model-a","stream":true,"instructions":"原指令","input":[` + officialRs + `,` + own + `,` + visibleMsg + `,` + callItem + `,{"type":"compaction_trigger"}],"tools":[{"type":"function","name":"shell"}]}`
	out, err := normalize.SummaryRequest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	m := decodeMap(t, out)
	if m["model"] != "relay-model-a" {
		t.Fatalf("model = %v", m["model"])
	}
	if m["stream"] != false {
		t.Fatalf("stream = %v，应为 false", m["stream"])
	}
	if m["instructions"] != normalize.SummaryInstructions {
		t.Fatalf("instructions = %v", m["instructions"])
	}
	if strings.Contains(string(out), "compaction_trigger") {
		t.Fatalf("不应含 compaction_trigger：%s", out)
	}
	if strings.Contains(string(out), "ENC") {
		t.Fatalf("官方加密 reasoning 应被整理掉：%s", out)
	}
	items := inputItems(t, out)
	if len(items) != 3 || items[0]["role"] != "user" || items[1]["type"] != "message" || items[2]["type"] != "function_call" {
		t.Fatalf("input 应为 [摘要消息, 可见消息, 工具调用]：%s", out)
	}
	if !strings.Contains(string(out), "以下是之前对话的摘要：旧摘要") {
		t.Fatalf("旧摘要应展开：%s", out)
	}
}

func TestSummaryFromResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		resp    string
		want    string
		wantErr bool
	}{
		{"单段文字", `{"output":[{"type":"message","content":[{"type":"output_text","text":"总结正文"}]}]}`, "总结正文", false},
		{"忽略非 output_text 与 reasoning", `{"output":[{"type":"reasoning","summary":[]},{"type":"message","content":[{"type":"refusal","text":"x"},{"type":"output_text","text":"总结正文"}]}]}`, "总结正文", false},
		{"空文字", `{"output":[{"type":"message","content":[{"type":"output_text","text":""}]}]}`, "", true},
		{"没有 output", `{}`, "", true},
		{"非法 JSON", `nope`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalize.SummaryFromResponse([]byte(tc.resp))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v，期望出错 = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("= %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestCompactionStream_EventsInOrder(t *testing.T) {
	t.Parallel()
	out := normalize.CompactionStream("摘要正文")
	blocks := strings.Split(strings.TrimSuffix(string(out), "\n\n"), "\n\n")
	wantNames := []string{"response.created", "response.output_item.added", "response.output_item.done", "response.completed"}
	if len(blocks) != len(wantNames) {
		t.Fatalf("事件数 = %d，期望 4：\n%s", len(blocks), out)
	}
	for i, blk := range blocks {
		lines := strings.Split(blk, "\n")
		if len(lines) != 2 || lines[0] != "event: "+wantNames[i] || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("第 %d 个事件格式不对：%q", i, blk)
		}
		data := decodeMap(t, []byte(strings.TrimPrefix(lines[1], "data: ")))
		if data["type"] != wantNames[i] {
			t.Fatalf("第 %d 个事件 data.type = %v", i, data["type"])
		}
		if wantNames[i] == "response.completed" {
			resp, _ := data["response"].(map[string]any)
			if id, _ := resp["id"].(string); id == "" {
				t.Fatalf("completed 事件缺少非空 response.id：%s", blk)
			}
		}
		if wantNames[i] == "response.output_item.done" {
			itemJSON, _ := json.Marshal(data["item"])
			if got, ok := normalize.ParseCompactionItem(itemJSON); !ok || got != "摘要正文" {
				t.Fatalf("done 事件的 item 不是本工具压缩条目：%s", itemJSON)
			}
		}
	}
}

func TestCompactionOutput(t *testing.T) {
	t.Parallel()
	m := decodeMap(t, normalize.CompactionOutput("摘要正文"))
	arr, _ := m["output"].([]any)
	if len(arr) != 1 {
		t.Fatalf("output 条数 = %d", len(arr))
	}
	itemJSON, _ := json.Marshal(arr[0])
	if got, ok := normalize.ParseCompactionItem(itemJSON); !ok || got != "摘要正文" {
		t.Fatalf("output[0] 不是本工具压缩条目：%s", itemJSON)
	}
}
