package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modelbridge/internal/normalize"
	"modelbridge/internal/proxy"
)

const (
	nVisibleMsg = `{"type":"message","role":"user","content":[{"type":"input_text","text":"你好"}]}`
	nRelayRs    = `{"type":"reasoning","id":"rs_relay_1","summary":[]}`
	nOfficialRs = `{"type":"reasoning","id":"rs_official","encrypted_content":"ENC","summary":[]}`
	nSecretText = "摘要正文-占位"
)

var jsonHdr = map[string]string{"Content-Type": "application/json", "Authorization": officialKey}

func newNormalizeEnv(t *testing.T, dataDir string, relayHandler http.HandlerFunc) *testEnv {
	t.Helper()
	e := &testEnv{
		api:     newFakeUpstream(t, nil),
		chatgpt: newFakeUpstream(t, nil),
		relay:   newFakeUpstream(t, relayHandler),
		log:     &syncBuffer{},
	}
	e.srv = proxy.New(proxy.Config{
		OfficialAPIURL:     e.api.URL + "/v1",
		OfficialChatGPTURL: e.chatgpt.URL + "/backend-api/codex",
		LogOutput:          e.log,
		DataDir:            dataDir,
	})
	e.srv.SetRelay(proxy.RelayConfig{Models: []string{relayModel}, BaseURL: e.relay.URL + "/v1", Key: relayKey})
	e.front = httptest.NewServer(e.srv.Handler())
	t.Cleanup(e.front.Close)
	return e
}

func summaryReply(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"output": []any{map[string]any{
			"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}},
		}}})
		_, _ = w.Write(b)
	}
}

func TestNormalize_RelayRequestCleaned(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), nil)
	body := `{"model":"` + relayModel + `","input":[` + nOfficialRs + `,` + nVisibleMsg + `],"tools":[{"type":"function","name":"shell"},{"type":"web_search"}]}`
	e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(body))
	got := string(e.relay.onlyHit(t).Body)
	if strings.Contains(got, "ENC") || strings.Contains(got, "web_search") {
		t.Fatalf("中台收到的请求未整理：%s", got)
	}
	if !strings.Contains(got, `"shell"`) || !strings.Contains(got, "你好") {
		t.Fatalf("function 工具和可见消息应保留：%s", got)
	}
}

func TestNormalize_RelayStreamPassthroughAndRecorded(t *testing.T) {
	t.Parallel()
	stream := "event: response.created\ndata: {\"type\":\"response.created\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"rs_relay_1\",\"summary\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"
	dir := t.TempDir()
	e := newNormalizeEnv(t, dir, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	})
	_, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(`{"model":"`+relayModel+`","stream":true,"input":[]}`))
	if string(got) != stream {
		t.Fatalf("客户端收到的字节与中台发出的不一致\n得到：%q", got)
	}

	file := filepath.Join(dir, "relay-reasoning-ids.json")
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, _ := os.ReadFile(file)
		if strings.Contains(string(b), "rs_relay_1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("2 秒内 %s 里没有 rs_relay_1，内容：%q", file, b)
		}
		time.Sleep(10 * time.Millisecond)
	}

	e2 := newNormalizeEnv(t, dir, nil)
	off := `{"model":"official-model","input":[` + nVisibleMsg + `,` + nRelayRs + `]}`
	e2.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(off))
	if h := string(e2.api.onlyHit(t).Body); strings.Contains(h, "rs_relay_1") {
		t.Fatalf("重启后官方仍收到中台 reasoning：%s", h)
	}
}

func TestNormalize_RelayErrorResponseNotRecorded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	e := newNormalizeEnv(t, dir, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	resp, body := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(`{"model":"`+relayModel+`","input":[]}`))
	assertNeutralError(t, e, resp, body, 500, "中台服务返回 500：中台服务暂时不可用", "relay_5xx")
}

func TestNormalize_OfficialRequest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ids, err := normalize.OpenReasoningIDs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ids.Add("rs_relay_1"); err != nil {
		t.Fatal(err)
	}

	t.Run("带中台推理条目：官方收到的请求体里没有它", func(t *testing.T) {
		t.Parallel()
		e := newNormalizeEnv(t, dir, nil)
		raw := `{"model":"official-model","input":[` + nVisibleMsg + `,` + nRelayRs + `,` + nOfficialRs + `]}`
		e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(raw))
		got := string(e.api.onlyHit(t).Body)
		if strings.Contains(got, "rs_relay_1") || !strings.Contains(got, "rs_official") || !strings.Contains(got, "你好") {
			t.Fatalf("官方收到的请求体不对：%s", got)
		}
	})

	t.Run("带中台推理条目的 zstd 请求：明文发出并去掉 Content-Encoding", func(t *testing.T) {
		t.Parallel()
		e := newNormalizeEnv(t, dir, nil)
		raw := `{"model":"official-model","input":[` + nRelayRs + `,` + nVisibleMsg + `]}`
		hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
		e.do(t, http.MethodPost, "/v1/responses", hdr, zstdEncode(t, []byte(raw)))
		hit := e.api.onlyHit(t)
		if hit.Header.Get("Content-Encoding") != "" {
			t.Fatalf("Content-Encoding 应被去掉，实际 %q", hit.Header.Get("Content-Encoding"))
		}
		if strings.Contains(string(hit.Body), "rs_relay_1") || !strings.Contains(string(hit.Body), "你好") {
			t.Fatalf("官方收到的请求体不对：%q", hit.Body)
		}
	})

	t.Run("本工具摘要被展开", func(t *testing.T) {
		t.Parallel()
		e := newNormalizeEnv(t, dir, nil)
		raw := `{"model":"official-model","input":[` + string(normalize.NewCompactionItem(nSecretText)) + `]}`
		e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(raw))
		got := string(e.api.onlyHit(t).Body)
		if strings.Contains(got, "model_bridge_compaction") || !strings.Contains(got, "以下是之前对话的摘要："+nSecretText) {
			t.Fatalf("官方收到的请求体不对：%s", got)
		}
	})

	t.Run("不含两类内容的 zstd 请求：官方收到原样压缩体", func(t *testing.T) {
		t.Parallel()
		e := newNormalizeEnv(t, dir, nil)
		raw := `{"model":"official-model","input":[` + nOfficialRs + `,` + nVisibleMsg + `]}`
		compressed := zstdEncode(t, []byte(raw))
		hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
		e.do(t, http.MethodPost, "/v1/responses", hdr, compressed)
		hit := e.api.onlyHit(t)
		if string(hit.Body) != string(compressed) {
			t.Fatal("官方收到的压缩体与原始压缩体不一致")
		}
		if hit.Header.Get("Content-Encoding") != "zstd" {
			t.Fatalf("Content-Encoding = %q，应保持 zstd", hit.Header.Get("Content-Encoding"))
		}
	})
}

func TestCompactionTakeover_Stream(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), summaryReply(nSecretText))
	body := `{"model":"` + relayModel + `","stream":true,"instructions":"原指令","input":[` + nOfficialRs + `,` + nVisibleMsg + `,{"type":"compaction_trigger"}]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(body))

	hit := e.relay.onlyHit(t)
	if hit.Path != "/v1/responses" {
		t.Fatalf("中台收到的路径 = %q", hit.Path)
	}
	var sent map[string]any
	if err := json.Unmarshal(hit.Body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["stream"] != false || sent["instructions"] != normalize.SummaryInstructions || sent["model"] != relayModel {
		t.Fatalf("总结请求字段不对：%s", hit.Body)
	}
	if strings.Contains(string(hit.Body), "compaction_trigger") || strings.Contains(string(hit.Body), "ENC") {
		t.Fatalf("总结请求未整理：%s", hit.Body)
	}
	if hit.Header.Get("Authorization") != "Bearer "+relayKey {
		t.Fatalf("总结请求的 Authorization 应使用中台 Key")
	}

	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	var names []string
	var doneItem []byte
	for _, blk := range strings.Split(strings.TrimSuffix(string(got), "\n\n"), "\n\n") {
		lines := strings.Split(blk, "\n")
		if len(lines) != 2 {
			t.Fatalf("事件格式不对：%q", blk)
		}
		name := strings.TrimPrefix(lines[0], "event: ")
		names = append(names, name)
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &data); err != nil || data["type"] != name {
			t.Fatalf("data 不对：%q", lines[1])
		}
		if name == "response.output_item.done" {
			doneItem, _ = json.Marshal(data["item"])
		}
	}
	want := "response.created,response.output_item.added,response.output_item.done,response.completed"
	if strings.Join(names, ",") != want {
		t.Fatalf("事件顺序 = %v", names)
	}
	if s, ok := normalize.ParseCompactionItem(doneItem); !ok || s != nSecretText {
		t.Fatalf("done 事件的 item 不是本工具压缩条目：%s", doneItem)
	}
	if strings.Contains(e.log.String(), nSecretText) || strings.Contains(e.log.String(), relayKey) {
		t.Fatalf("日志泄露了摘要或 Key：%q", e.log.String())
	}
}

func TestCompactionTakeover_RelayFailure(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "relay-secret-body")
	})
	body := `{"model":"` + relayModel + `","input":[` + nVisibleMsg + `,{"type":"compaction_trigger"}]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(body))
	assertNeutralError(t, e, resp, got, 500, "中台服务返回 500：中台服务暂时不可用", "relay_5xx")
}

func TestCompactionTakeover_EmptySummaryIsRelayError(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), summaryReply(""))
	body := `{"model":"` + relayModel + `","input":[` + nVisibleMsg + `,{"type":"compaction_trigger"}]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(body))
	if resp.StatusCode < 400 {
		t.Fatalf("状态码 = %d，摘要为空应按中台错误处理", resp.StatusCode)
	}
	var m struct {
		Error struct{ Type, Code string } `json:"error"`
	}
	if err := json.Unmarshal(got, &m); err != nil || m.Error.Type != "model_bridge_error" || m.Error.Code == "" {
		t.Fatalf("应返回中性错误体，实际 %q", got)
	}
}

func TestCompactEndpoint_RelayModel(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), summaryReply(nSecretText))
	body := `{"model":"` + relayModel + `","input":[` + nVisibleMsg + `]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses/compact", jsonHdr, []byte(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 %q", resp.StatusCode, got)
	}
	var out struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(got, &out); err != nil || len(out.Output) != 1 {
		t.Fatalf("应返回 {\"output\":[条目]}，实际 %q", got)
	}
	if s, ok := normalize.ParseCompactionItem(out.Output[0]); !ok || s != nSecretText {
		t.Fatalf("output[0] 不是本工具压缩条目：%s", out.Output[0])
	}
	hit := e.relay.onlyHit(t)
	if hit.Path != "/v1/responses" {
		t.Fatalf("中台收到的路径 = %q，期望 /v1/responses", hit.Path)
	}
	if strings.Contains(string(hit.Body), `"stream":true`) || !strings.Contains(string(hit.Body), normalize.SummaryInstructions) {
		t.Fatalf("总结请求不对：%s", hit.Body)
	}
}

func TestCompactEndpoint_OfficialModelUntouched(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), nil)
	body := `{"model":"official-model","input":[` + nVisibleMsg + `]}`
	_, got := e.do(t, http.MethodPost, "/v1/responses/compact", jsonHdr, []byte(body))
	if string(got) != upstreamReply {
		t.Fatalf("官方模型应原样转给官方，响应 %q", got)
	}
	if hit := e.api.onlyHit(t); hit.Path != "/v1/responses/compact" {
		t.Fatalf("官方收到的路径 = %q", hit.Path)
	}
}

func TestCompactEndpoint_RelayFailure(t *testing.T) {
	t.Parallel()
	e := newNormalizeEnv(t, t.TempDir(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	body := `{"model":"` + relayModel + `","input":[` + nVisibleMsg + `]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses/compact", jsonHdr, []byte(body))
	assertNeutralError(t, e, resp, got, 500, "中台服务返回 500：中台服务暂时不可用", "relay_5xx")
}

func TestCompactionTakeover_NotLimitedByResponseHeaderTimeout(t *testing.T) {
	oldRelay, oldSummary := proxy.ResponseHeaderTimeout, proxy.SummaryResponseHeaderTimeout
	proxy.ResponseHeaderTimeout = 1 * time.Second
	proxy.SummaryResponseHeaderTimeout = 5 * time.Second
	t.Cleanup(func() { proxy.ResponseHeaderTimeout, proxy.SummaryResponseHeaderTimeout = oldRelay, oldSummary })

	slow := summaryReply(nSecretText)
	e := newNormalizeEnv(t, t.TempDir(), func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		slow(w, r)
	})
	body := `{"model":"` + relayModel + `","input":[` + nVisibleMsg + `,{"type":"compaction_trigger"}]}`
	resp, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(body))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(got), "response.completed") {
		t.Fatalf("慢中台的压缩应成功，状态 %d，响应 %q", resp.StatusCode, got)
	}
}
