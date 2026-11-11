package normalize_test

import (
	"bytes"
	"strings"
	"testing"

	"modelbridge/internal/normalize"
)

func newIDs(t *testing.T, ids ...string) *normalize.ReasoningIDs {
	t.Helper()
	set, err := normalize.OpenReasoningIDs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > 0 {
		if err := set.Add(ids...); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

const relayRs = `{"type":"reasoning","id":"rs_relay","summary":[]}`

func TestForOfficial_RemovesRelayReasoning(t *testing.T) {
	t.Parallel()
	odd := `{ "type":"message",  "role":"user","content":[{"type":"input_text","text":"你好"}] }`
	body := []byte(`{"model":"official-model","input":[` + odd + `,` + relayRs + `,` + officialRs + `,` + callItem + `]}`)
	out, changed, err := normalize.ForOfficial(body, newIDs(t, "rs_relay"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("应报告已改动")
	}
	if strings.Contains(string(out), "rs_relay") {
		t.Fatalf("中台 reasoning 仍在：%s", out)
	}
	for _, keep := range []string{odd, officialRs, callItem} {
		if !strings.Contains(string(out), keep) {
			t.Fatalf("应逐字节保留 %s\n实际：%s", keep, out)
		}
	}
}

func TestForOfficial_ExpandsOwnSummary(t *testing.T) {
	t.Parallel()
	own := string(normalize.NewCompactionItem("摘要正文"))
	body := []byte(`{"model":"official-model","input":[` + own + `,` + visibleMsg + `]}`)
	out, changed, err := normalize.ForOfficial(body, newIDs(t))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("应报告已改动")
	}
	items := inputItems(t, out)
	if len(items) != 2 || items[0]["type"] != "message" || items[0]["role"] != "user" {
		t.Fatalf("摘要应展开为用户消息：%s", out)
	}
	if !strings.Contains(string(out), "以下是之前对话的摘要：摘要正文") {
		t.Fatalf("缺少摘要文字：%s", out)
	}
	if !strings.Contains(string(out), visibleMsg) {
		t.Fatalf("其余条目应逐字节保留：%s", out)
	}
}

func TestForOfficial_Unchanged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		ids  []string
	}{
		{"官方 reasoning 与 OpenAI compaction 都保留", `{"model":"m",  "input":[` + officialRs + `,` + openaiCmp + `,` + visibleMsg + `]}`, []string{"rs_relay"}},
		{"集合为空", `{"model":"m","input":[` + relayRs + `]}`, nil},
		{"没有 input", `{"model":"m"}`, []string{"rs_relay"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, changed, err := normalize.ForOfficial([]byte(tc.body), newIDs(t, tc.ids...))
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				t.Fatal("应报告未改动")
			}
			if !bytes.Equal(out, []byte(tc.body)) {
				t.Fatalf("未改动时应原样返回\n得到：%s", out)
			}
		})
	}
}

func TestForOfficial_InvalidJSON(t *testing.T) {
	t.Parallel()
	out, changed, err := normalize.ForOfficial([]byte(`not json`), newIDs(t, "rs_relay"))
	if err == nil && (changed || string(out) != "not json") {
		t.Fatal("非法 JSON 应返回错误，或原样返回并报告未改动")
	}
}

func TestForOfficial_KeepsTopLevelBytes(t *testing.T) {
	t.Parallel()
	head := `{"model":"m", "stream":true,"input":[`
	tail := `],"include":["x"] }`
	body := []byte(head + relayRs + `,` + visibleMsg + tail)
	out, changed, err := normalize.ForOfficial(body, newIDs(t, "rs_relay"))
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	want := head + visibleMsg + tail
	if string(out) != want {
		t.Fatalf("input 以外应逐字节保留\n得到：%s\n期望：%s", out, want)
	}
}
