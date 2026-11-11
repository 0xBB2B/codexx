package proxy_test

import (
	"bufio"
	"bytes"
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
	e.srv.SetRelay(proxy.RelayConfig{
		Models:  []string{relayModel, "relay-model-b"},
		BaseURL: e.relay.URL + "/v1",
		Key:     relayKey,
	})
	e.front = httptest.NewServer(e.srv.Handler())
	t.Cleanup(e.front.Close)
	return e
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
		{"中台", `{"model":"` + relayModel + `"}`},
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
		"agent-marker-0000", "custom-marker-0000", relayModel, secretModel,
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
	assertNeutralError(t, e, resp, body, http.StatusBadGateway, "连不上中台，请检查网络和 Base URL", "relay_unreachable")
}

func TestRelay_InvalidBaseURL_Unreachable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	e.srv.SetRelay(proxy.RelayConfig{Models: []string{relayModel}, BaseURL: "http://[::1", Key: relayKey})
	resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
	assertNeutralError(t, e, resp, body, http.StatusBadGateway, "连不上中台，请检查网络和 Base URL", "relay_unreachable")
}
