package proxy_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/proxy"
)

const (
	relayModel    = "relay-model-a"
	relayOrigName = "model-a"
	relayKey      = "sk-example-0000"
	officialKey   = "Bearer sk-official-0000"
	accountID     = "acct-0000"
	cookieValue   = "session=cookie-0000"
	upstreamReply = `{"from":"upstream"}`
)

type upstreamHit struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type fakeUpstream struct {
	*httptest.Server
	mu      sync.Mutex
	hits    []upstreamHit
	handler http.HandlerFunc
}

func newFakeUpstream(t *testing.T, h http.HandlerFunc) *fakeUpstream {
	t.Helper()
	u := &fakeUpstream{handler: h}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.hits = append(u.hits, upstreamHit{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		h := u.handler
		u.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamReply)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *fakeUpstream) setHandler(h http.HandlerFunc) {
	u.mu.Lock()
	u.handler = h
	u.mu.Unlock()
}

func (u *fakeUpstream) Hits() []upstreamHit {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]upstreamHit(nil), u.hits...)
}

func (u *fakeUpstream) onlyHit(t *testing.T) upstreamHit {
	t.Helper()
	hits := u.Hits()
	if len(hits) != 1 {
		t.Fatalf("上游应恰好收到 1 次请求，实际 %d 次", len(hits))
	}
	return hits[0]
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testEnv struct {
	api, chatgpt, relay *fakeUpstream
	log                 *syncBuffer
	srv                 *proxy.Server
	front               *httptest.Server
}

func newEnv(t *testing.T, relayHandler http.HandlerFunc) *testEnv {
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
	})
	e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{
		relayModel:      e.target(relayOrigName),
		"relay-model-b": e.target("model-b"),
	}})
	e.front = httptest.NewServer(e.srv.Handler())
	t.Cleanup(e.front.Close)
	return e
}

func (e *testEnv) target(model string) proxy.RelayTarget {
	return proxy.RelayTarget{BaseURL: e.relay.URL + "/v1", Key: relayKey, Model: model}
}

func (e *testEnv) do(t *testing.T, method, path string, hdr map[string]string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, e.front.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, got
}

func waitLog(t *testing.T, b *syncBuffer, substrs ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := b.String()
		ok := true
		for _, sub := range substrs {
			if !strings.Contains(s, sub) {
				ok = false
			}
		}
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("日志里等不到 %q，实际日志：%q", substrs, b.String())
}

func TestDefaultListenAddr(t *testing.T) {
	t.Parallel()
	if proxy.DefaultListenAddr != "127.0.0.1:47311" {
		t.Fatalf("默认监听地址 = %q", proxy.DefaultListenAddr)
	}
}

func TestStart_ServesOnInjectedAddr(t *testing.T) {
	t.Parallel()
	srv := proxy.New(proxy.Config{ListenAddr: "127.0.0.1:0", LogOutput: io.Discard})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start 返回错误：%v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Get("http://" + srv.Addr() + "/_health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
}

func TestStart_PortInUse(t *testing.T) {
	t.Parallel()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })

	srv := proxy.New(proxy.Config{ListenAddr: occupied.Addr().String(), LogOutput: io.Discard})
	err = srv.Start()
	var pe *proxy.Error
	if !errors.As(err, &pe) {
		t.Fatalf("期望 *proxy.Error，实际 %v", err)
	}
	if pe.Code != "proxy_port_in_use" {
		t.Fatalf("错误码 = %q", pe.Code)
	}
}

func TestHandler_SourceAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{"IPv4 本机放行", "127.0.0.1:50000", http.StatusOK},
		{"IPv6 本机放行", "[::1]:50000", http.StatusOK},
		{"局域网 IPv4 拒绝", "192.0.2.10:50000", http.StatusForbidden},
		{"公网 IPv6 拒绝", "[2001:db8::1]:50000", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			for _, path := range []string{"/_health", "/v1/responses"} {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"`+relayModel+`"}`))
				req.RemoteAddr = tt.remoteAddr
				req.Host = "127.0.0.1:47311"
				rec := httptest.NewRecorder()
				e.srv.Handler().ServeHTTP(rec, req)
				if rec.Code != tt.wantStatus {
					t.Fatalf("%s 状态码 = %d，期望 %d", path, rec.Code, tt.wantStatus)
				}
			}
			if tt.wantStatus == http.StatusForbidden {
				if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
					t.Fatalf("被拒绝的请求不应到达任何上游，实际 %d 次", n)
				}
			}
		})
	}
}

func TestHandler_StreamingForwardedImmediately(t *testing.T) {
	sse := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: %d\n\n", i)
			fl.Flush()
			if i < 3 {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
	tests := []struct {
		name string
		body string
	}{
		{"中转", `{"model":"` + relayModel + `"}`},
		{"官方", `{"model":"official-model"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, sse)
			e.api.setHandler(sse)

			start := time.Now()
			req, _ := http.NewRequest(http.MethodPost, e.front.URL+"/v1/responses", strings.NewReader(tt.body))
			resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			var arrivals []time.Time
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if strings.HasPrefix(sc.Text(), "data:") {
					arrivals = append(arrivals, time.Now())
				}
			}
			if len(arrivals) != 4 {
				t.Fatalf("收到 %d 段，期望 4 段", len(arrivals))
			}
			if first := arrivals[0].Sub(start); first > 300*time.Millisecond {
				t.Fatalf("第一段延迟 %v，说明被缓冲", first)
			}
			for i := 1; i < len(arrivals); i++ {
				gap := arrivals[i].Sub(arrivals[i-1])
				if gap < 350*time.Millisecond || gap > 700*time.Millisecond {
					t.Fatalf("第 %d 段与上一段间隔 %v，期望约 500ms", i, gap)
				}
			}
		})
	}
}

func TestLog_RecordsDestinationAndStatus(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
	e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"official-model"}`))
	waitLog(t, e.log, "relay", "official", "200")
}

func TestLog_NoHeaderValuesNoModel(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	hdr := map[string]string{
		"Authorization":      officialKey,
		"Cookie":             cookieValue,
		"ChatGPT-Account-ID": accountID,
		"User-Agent":         "agent-marker-0000",
		"X-Custom":           "custom-marker-0000",
	}
	secretModel := "official-model-secret-name"
	e.do(t, http.MethodPost, "/v1/responses", hdr, []byte(`{"model":"`+relayModel+`"}`))
	e.do(t, http.MethodPost, "/v1/responses", hdr, []byte(`{"model":"`+secretModel+`"}`))
	waitLog(t, e.log, "relay", "official")

	logged := e.log.String()
	for _, forbidden := range []string{
		officialKey, "sk-official-0000", relayKey, cookieValue, accountID,
		"agent-marker-0000", "custom-marker-0000", relayModel, relayOrigName, secretModel,
	} {
		if strings.Contains(logged, forbidden) {
			t.Errorf("日志里出现了不该记录的内容 %q：%s", forbidden, logged)
		}
	}
}

func TestHandler_RejectsForeignHostAndOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		host       string
		origin     string
		wantStatus int
	}{
		{"伪造域名", "evil.example:47311", "", http.StatusForbidden},
		{"带 Origin", "127.0.0.1:47311", "http://evil.example", http.StatusForbidden},
		{"127.0.0.1", "127.0.0.1:47311", "", http.StatusOK},
		{"localhost", "localhost:47311", "", http.StatusOK},
		{"IPv6", "[::1]:47311", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			for _, path := range []string{"/_health", "/v1/responses"} {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"`+relayModel+`"}`))
				req.RemoteAddr = "127.0.0.1:50000"
				req.Host = tt.host
				if tt.origin != "" {
					req.Header.Set("Origin", tt.origin)
				}
				rec := httptest.NewRecorder()
				e.srv.Handler().ServeHTTP(rec, req)
				if rec.Code != tt.wantStatus {
					t.Fatalf("%s 状态码 = %d，期望 %d", path, rec.Code, tt.wantStatus)
				}
			}
			if tt.wantStatus == http.StatusForbidden {
				if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
					t.Fatalf("被拒绝的请求不应到达任何上游，实际 %d 次", n)
				}
			}
		})
	}
}

func TestRelay_ResponseHeaderTimeout_Unreachable(t *testing.T) {
	old := proxy.ResponseHeaderTimeout
	proxy.ResponseHeaderTimeout = 200 * time.Millisecond
	t.Cleanup(func() { proxy.ResponseHeaderTimeout = old })

	release := make(chan struct{})
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	t.Cleanup(func() { close(release) })

	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
	assertNeutralError(t, e, resp, body, http.StatusBadGateway, "连不上中转服务，请检查网络和 Base URL", "relay_unreachable")
}

func TestRelay_InvalidBaseURL_Unreachable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{relayModel: {BaseURL: "http://[::1", Key: relayKey, Model: relayOrigName}}})
	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
	assertNeutralError(t, e, resp, body, http.StatusBadGateway, "连不上中转服务，请检查网络和 Base URL", "relay_unreachable")
}

const officialUnavailableBody = `{"error":{"message":"当前没有登录 ChatGPT，官方模型不可用，请切换到中转模型。","type":"model_bridge_error","code":"official_unavailable"}}`

func newRelayOnlyEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t, nil)
	e.srv.SetRelay(proxy.RelayConfig{
		Routes:    map[string]proxy.RelayTarget{"relay-glm-5.3": e.target("glm-5.3")},
		RelayOnly: true,
	})
	return e
}

func (e *testEnv) upstreamHitCount() int {
	return len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits())
}

func TestRelayOnly_RelayModelStillForwarded(t *testing.T) {
	t.Parallel()
	e := newRelayOnlyEnv(t)
	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"relay-glm-5.3"}`))
	if resp.StatusCode != http.StatusOK || string(body) != upstreamReply {
		t.Fatalf("响应 = %d %s，期望中转回复", resp.StatusCode, body)
	}
	hit := e.relay.onlyHit(t)
	if !strings.Contains(string(hit.Body), `"glm-5.3"`) || strings.Contains(string(hit.Body), "relay-glm-5.3") {
		t.Fatalf("中转收到的模型名应改回原名，实际 %s", hit.Body)
	}
	if hit.Header.Get("Authorization") != "Bearer "+relayKey {
		t.Fatalf("中转应收到中转 Key，实际 %q", hit.Header.Get("Authorization"))
	}
	if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
		t.Fatalf("官方服务不应收到请求，实际 %d 次", n)
	}
	waitLog(t, e.log, "relay", "200")
}

func TestRelayOnly_NonRelayRequestsRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		hdr  map[string]string
		body string
	}{
		{"官方模型名", nil, `{"model":"gpt-a"}`},
		{"没有 model 字段", nil, `{"input":"hi"}`},
		{"不认识的模型", nil, `{"model":"unknown-model"}`},
		{"带 ChatGPT-Account-ID 的官方模型", map[string]string{"ChatGPT-Account-ID": accountID}, `{"model":"gpt-a"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newRelayOnlyEnv(t)
			resp, body := e.do(t, http.MethodPost, "/v1/responses", tt.hdr, []byte(tt.body))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("状态码 = %d，期望 400", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			if string(body) != officialUnavailableBody {
				t.Fatalf("正文 = %s", body)
			}
			if n := e.upstreamHitCount(); n != 0 {
				t.Fatalf("不应有任何上游收到请求，实际 %d 次", n)
			}
		})
	}
}

func TestRelayOnly_RejectedRequestLogged(t *testing.T) {
	t.Parallel()
	e := newRelayOnlyEnv(t)
	hdr := map[string]string{
		"Authorization":      "Bearer sk-official-marker",
		"Cookie":             cookieValue,
		"ChatGPT-Account-ID": accountID,
	}
	e.do(t, http.MethodPost, "/v1/responses", hdr, []byte(`{"model":"gpt-a"}`))
	e.do(t, http.MethodPost, "/v1/responses", hdr, []byte(`{"input":"hi"}`))
	waitLog(t, e.log, "rejected", "400")

	logged := e.log.String()
	if n := strings.Count(logged, "rejected"); n != 2 {
		t.Fatalf("应有 2 行 rejected 日志，实际 %d：%s", n, logged)
	}
	for _, forbidden := range []string{"sk-official-marker", "Authorization", cookieValue, accountID, "gpt-a"} {
		if strings.Contains(logged, forbidden) {
			t.Errorf("日志里出现了不该记录的内容 %q：%s", forbidden, logged)
		}
	}
}

func TestRelayOnly_SwitchedOffAtRuntime(t *testing.T) {
	t.Parallel()
	e := newRelayOnlyEnv(t)
	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"gpt-a"}`))
	if resp.StatusCode != http.StatusBadRequest || string(body) != officialUnavailableBody {
		t.Fatalf("开启时应拒绝，实际 %d %s", resp.StatusCode, body)
	}

	e.srv.SetRelay(proxy.RelayConfig{
		Routes:    map[string]proxy.RelayTarget{"relay-glm-5.3": e.target("glm-5.3")},
		RelayOnly: false,
	})
	resp, body = e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"gpt-a"}`))
	if resp.StatusCode != http.StatusOK || string(body) != upstreamReply {
		t.Fatalf("关闭后应转发官方，实际 %d %s", resp.StatusCode, body)
	}
	e.api.onlyHit(t)
}

func TestRelayOnly_SwitchedOnAtRuntime(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	resp, _ := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"gpt-a"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("默认应转发官方，实际 %d", resp.StatusCode)
	}
	e.api.onlyHit(t)

	e.srv.SetRelay(proxy.RelayConfig{
		Routes:    map[string]proxy.RelayTarget{"relay-glm-5.3": e.target("glm-5.3")},
		RelayOnly: true,
	})
	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"gpt-a"}`))
	if resp.StatusCode != http.StatusBadRequest || string(body) != officialUnavailableBody {
		t.Fatalf("开启后应拒绝，实际 %d %s", resp.StatusCode, body)
	}
	if n := len(e.api.Hits()); n != 1 {
		t.Fatalf("官方服务不应再收到请求，累计 %d 次", n)
	}
}

func TestRelayOnly_HealthAndWebSocketUnchanged(t *testing.T) {
	t.Parallel()
	e := newRelayOnlyEnv(t)

	resp, body := e.do(t, http.MethodGet, "/_health", nil, nil)
	if resp.StatusCode != http.StatusOK || string(body) != `{"ok":true}` {
		t.Fatalf("/_health = %d %s", resp.StatusCode, body)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Host = "127.0.0.1:47311"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("WebSocket 升级状态码 = %d，期望 426", rec.Code)
	}
	if n := e.upstreamHitCount(); n != 0 {
		t.Fatalf("不应有上游收到请求，实际 %d 次", n)
	}
}

func TestForward_DoesNotAddAcceptEncoding(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"%s","input":[]}`)
	tests := []struct {
		name  string
		model string
		hdr   map[string]string
		hit   func(e *testEnv) upstreamHit
	}{
		{"中转", relayModel, map[string]string{"Content-Type": "application/json"}, func(e *testEnv) upstreamHit { return e.relay.onlyHit(t) }},
		{"官方", "official-model", jsonHdr, func(e *testEnv) upstreamHit { return e.api.onlyHit(t) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			e.do(t, http.MethodPost, "/v1/responses", tc.hdr, []byte(fmt.Sprintf(string(body), tc.model)))
			if got := tc.hit(e).Header.Get("Accept-Encoding"); got != "" {
				t.Fatalf("上游收到的 Accept-Encoding = %q，转发不该自己加", got)
			}
		})
	}
}

func TestForward_OfficialCompressedBodyPassedThroughUntouched(t *testing.T) {
	t.Parallel()
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write([]byte(upstreamReply))
	_ = zw.Close()

	e := newEnv(t, nil)
	e.api.setHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(packed.Bytes())
	})
	resp, got := e.do(t, http.MethodPost, "/v1/responses", jsonHdr, []byte(`{"model":"official-model","input":[]}`))
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q，应保留 gzip", resp.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(got, packed.Bytes()) {
		t.Errorf("Codex 收到的应是上游的压缩字节，实际 %q", got)
	}
}
