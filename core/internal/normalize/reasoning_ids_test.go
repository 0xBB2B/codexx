package normalize_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modelbridge/internal/normalize"
)

func TestReasoningIDs_AddHasAndPersist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	set, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if set.Has("rs_a") {
		t.Fatal("新集合应为空")
	}
	if err := set.Add("rs_a", "rs_b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "relay-reasoning-ids.json")); err != nil {
		t.Fatalf("应写出 relay-reasoning-ids.json：%v", err)
	}
	reopened, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Has("rs_a") || !reopened.Has("rs_b") || reopened.Has("rs_c") || reopened.Len() != 2 {
		t.Fatalf("重新打开后集合不一致，Len = %d", reopened.Len())
	}
}

func TestReasoningIDs_AddDuplicateKeepsOne(t *testing.T) {
	t.Parallel()
	set := newIDs(t)
	_ = set.Add("rs_a")
	_ = set.Add("rs_a")
	if set.Len() != 1 {
		t.Fatalf("Len = %d，期望 1", set.Len())
	}
}

func TestReasoningIDs_EvictsOldest(t *testing.T) {
	t.Parallel()
	if normalize.MaxReasoningIDs != 5000 {
		t.Fatalf("MaxReasoningIDs = %d", normalize.MaxReasoningIDs)
	}
	dir := t.TempDir()
	set, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := normalize.MaxReasoningIDs + 3
	ids := make([]string, total)
	for i := range ids {
		ids[i] = fmt.Sprintf("rs_%d", i)
	}
	if err := set.Add(ids...); err != nil {
		t.Fatal(err)
	}
	check := func(name string, s *normalize.ReasoningIDs) {
		if s.Len() != normalize.MaxReasoningIDs {
			t.Fatalf("%s：Len = %d", name, s.Len())
		}
		for i := 0; i < 3; i++ {
			if s.Has(ids[i]) {
				t.Fatalf("%s：最旧的 %s 应被淘汰", name, ids[i])
			}
		}
		if !s.Has(ids[3]) || !s.Has(ids[total-1]) {
			t.Fatalf("%s：较新的 id 应保留", name)
		}
	}
	check("内存", set)
	reopened, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	check("重启后", reopened)
}

func TestOpenReasoningIDs_CorruptFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relay-reasoning-ids.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatalf("损坏文件应按空集合处理，实际错误：%v", err)
	}
	if set.Len() != 0 {
		t.Fatalf("Len = %d", set.Len())
	}
}

func sseEvent(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }

func TestTapReasoningIDs(t *testing.T) {
	t.Parallel()
	doneRs := func(id string) string {
		return sseEvent("response.output_item.done", `{"type":"response.output_item.done","item":{"type":"reasoning","id":"`+id+`","summary":[]}}`)
	}
	tests := []struct {
		name    string
		stream  string
		wantIDs []string
		skipIDs []string
	}{
		{"记录 reasoning 完成事件", sseEvent("response.created", `{"type":"response.created"}`) + doneRs("rs_1") + doneRs("rs_2"), []string{"rs_1", "rs_2"}, nil},
		{"忽略 message 完成事件", sseEvent("response.output_item.done", `{"item":{"type":"message","id":"msg_1"}}`) + doneRs("rs_1"), []string{"rs_1"}, []string{"msg_1"}},
		{"忽略 added 事件", sseEvent("response.output_item.added", `{"item":{"type":"reasoning","id":"rs_added"}}`), nil, []string{"rs_added"}},
		{"data 非法 JSON 不影响其他事件", sseEvent("response.output_item.done", `{bad`) + doneRs("rs_1"), []string{"rs_1"}, nil},
		{"只有 data 行", "data: " + `{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_nodata"}}` + "\n\n", []string{"rs_nodata"}, nil},
		{"CRLF 换行", strings.ReplaceAll(doneRs("rs_crlf"), "\n", "\r\n"), []string{"rs_crlf"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := newIDs(t)
			got, err := io.ReadAll(normalize.TapReasoningIDs(strings.NewReader(tc.stream), set))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.stream {
				t.Fatalf("透传字节被改动\n得到：%q\n期望：%q", got, tc.stream)
			}
			for _, id := range tc.wantIDs {
				if !set.Has(id) {
					t.Fatalf("应记录 %s", id)
				}
			}
			for _, id := range tc.skipIDs {
				if set.Has(id) {
					t.Fatalf("不应记录 %s", id)
				}
			}
		})
	}
}

type oneByteReader struct{ r io.Reader }

func (o oneByteReader) Read(p []byte) (int, error) { return o.r.Read(p[:1]) }

func TestTapReasoningIDs_SplitAcrossReads(t *testing.T) {
	t.Parallel()
	stream := sseEvent("response.output_item.done", `{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_split"}}`)
	set := newIDs(t)
	got, err := io.ReadAll(normalize.TapReasoningIDs(oneByteReader{strings.NewReader(stream)}, set))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != stream || !set.Has("rs_split") {
		t.Fatalf("逐字节读取时应仍能记录并透传，透传 = %q", got)
	}
}
