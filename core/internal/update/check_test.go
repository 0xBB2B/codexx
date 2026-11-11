package update_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/update"
)

type fakeGitHub struct {
	*httptest.Server

	mu     sync.Mutex
	status int
	body   string
	reqs   []*http.Request
}

func newFakeGitHub(t *testing.T, tag string) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{status: http.StatusOK}
	g.setRelease(tag)
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.reqs = append(g.reqs, r)
		status, body := g.status, g.body
		g.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGitHub) setRelease(tag string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.body = fmt.Sprintf(`{"tag_name":%q,"html_url":"https://github.com/acme/model-bridge/releases/tag/%s"}`, tag, tag)
}

func (g *fakeGitHub) respond(status int, body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status, g.body = status, body
}

func (g *fakeGitHub) requests() []*http.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*http.Request(nil), g.reqs...)
}

func (g *fakeGitHub) cfg() update.Config {
	return update.Config{APIBase: g.URL, Repository: "acme/model-bridge", CurrentVersion: "1.2.0"}
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Duration
	ch chan time.Time
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{at: c.now + d, ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += d
	var rest []fakeTimer
	for _, tm := range c.timers {
		if tm.at <= c.now {
			tm.ch <- time.Time{}
		} else {
			rest = append(rest, tm)
		}
	}
	c.timers = rest
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCheck_Result(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		tag        string
		wantNewer  bool
		wantLatest string
	}{
		{name: "有新版", current: "1.2.0", tag: "v1.3.0", wantNewer: true, wantLatest: "1.3.0"},
		{name: "同版本", current: "1.2.0", tag: "v1.2.0"},
		{name: "线上更旧", current: "1.2.0", tag: "v1.1.9"},
		{name: "预发布标签", current: "1.2.0", tag: "1.3.0-beta"},
		{name: "带 v 的预发布标签", current: "1.2.0", tag: "v1.3.0-rc.1"},
		{name: "标签只有两段", current: "1.2.0", tag: "v1.3"},
		{name: "标签不是版本号", current: "1.2.0", tag: "nightly"},
		{name: "按数字比较次版本", current: "1.9.0", tag: "v1.10.0", wantNewer: true, wantLatest: "1.10.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newFakeGitHub(t, tt.tag)
			cfg := g.cfg()
			cfg.CurrentVersion = tt.current
			res, err := update.Check(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if res.IsNewer != tt.wantNewer {
				t.Fatalf("IsNewer = %v, want %v", res.IsNewer, tt.wantNewer)
			}
			if tt.wantNewer {
				if res.LatestVersion != tt.wantLatest {
					t.Errorf("LatestVersion = %q, want %q", res.LatestVersion, tt.wantLatest)
				}
				wantURL := "https://github.com/acme/model-bridge/releases/tag/" + tt.tag
				if res.URL != wantURL {
					t.Errorf("URL = %q, want %q", res.URL, wantURL)
				}
			}
		})
	}
}

func TestCheck_RequestShape(t *testing.T) {
	g := newFakeGitHub(t, "v1.3.0")
	if _, err := update.Check(context.Background(), g.cfg()); err != nil {
		t.Fatal(err)
	}
	reqs := g.requests()
	if len(reqs) != 1 {
		t.Fatalf("请求数 = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet {
		t.Errorf("method = %s", r.Method)
	}
	if r.URL.Path != "/repos/acme/model-bridge/releases/latest" {
		t.Errorf("path = %q", r.URL.Path)
	}
	if r.URL.RawQuery != "" {
		t.Errorf("不该带查询参数: %q", r.URL.RawQuery)
	}
	if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q", got)
	}
	if got := r.Header.Get("User-Agent"); got != "model-bridge/1.2.0" {
		t.Errorf("User-Agent = %q", got)
	}
	allowed := map[string]bool{"Accept": true, "User-Agent": true, "Accept-Encoding": true}
	for k := range r.Header {
		if !allowed[k] {
			t.Errorf("多余的请求头 %s: %v", k, r.Header[k])
		}
	}
}

func TestCheck_NoRepositoryDoesNotRequest(t *testing.T) {
	g := newFakeGitHub(t, "v1.3.0")
	cfg := g.cfg()
	cfg.Repository = ""
	res, err := update.Check(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.IsNewer {
		t.Error("没有仓库名时 IsNewer 应为 false")
	}
	if n := len(g.requests()); n != 0 {
		t.Errorf("没有仓库名时不该发请求, 实际 %d 次", n)
	}
}

func TestRepository_DefaultsToEmptyWithoutLdflags(t *testing.T) {
	if got := update.Repository(); got != "" {
		t.Errorf("Repository() = %q, want 空串", got)
	}
}

func TestCheck_Failures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(g *fakeGitHub)
	}{
		{name: "返回 500", setup: func(g *fakeGitHub) { g.respond(500, `{"message":"boom"}`) }},
		{name: "返回 503", setup: func(g *fakeGitHub) { g.respond(503, ``) }},
		{name: "返回 404", setup: func(g *fakeGitHub) { g.respond(404, `{"message":"Not Found"}`) }},
		{name: "返回内容不是 JSON", setup: func(g *fakeGitHub) { g.respond(200, `<html>oops</html>`) }},
		{name: "连不上", setup: func(g *fakeGitHub) { g.Close() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newFakeGitHub(t, "v1.3.0")
			tt.setup(g)
			res, err := update.Check(context.Background(), g.cfg())
			if err == nil {
				t.Fatalf("期望返回错误, 得到 %+v", res)
			}
			if res.IsNewer {
				t.Error("失败时 IsNewer 应为 false")
			}
		})
	}
}

func TestLoop_Schedule(t *testing.T) {
	clock := &fakeClock{}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	calls := 0
	count := func() int { mu.Lock(); defer mu.Unlock(); return calls }
	done := make(chan struct{})
	go func() {
		defer close(done)
		update.Loop(ctx, clock, func(context.Context) {
			mu.Lock()
			calls++
			mu.Unlock()
		})
	}()

	waitFor(t, "首次定时器登记", func() bool { return clock.pending() == 1 })
	if count() != 0 {
		t.Fatal("时钟没推进前不该检查")
	}
	clock.Advance(update.StartupDelay)
	waitFor(t, "首次检查", func() bool { return count() == 1 })

	waitFor(t, "第二次定时器登记", func() bool { return clock.pending() == 1 })
	clock.Advance(update.Interval - time.Minute)
	time.Sleep(50 * time.Millisecond)
	if count() != 1 {
		t.Fatalf("不足 24 小时不该再检查, calls = %d", count())
	}
	clock.Advance(time.Minute)
	waitFor(t, "第二次检查", func() bool { return count() == 2 })

	waitFor(t, "第三次定时器登记", func() bool { return clock.pending() == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消后 Loop 没有返回")
	}
	clock.Advance(3 * update.Interval)
	time.Sleep(50 * time.Millisecond)
	if count() != 2 {
		t.Errorf("停止后不该再检查, calls = %d", count())
	}
}

func TestLoop_Constants(t *testing.T) {
	if update.StartupDelay <= 0 || update.StartupDelay > time.Minute {
		t.Errorf("StartupDelay = %v, want (0, 1m]", update.StartupDelay)
	}
	if update.Interval != 24*time.Hour {
		t.Errorf("Interval = %v, want 24h", update.Interval)
	}
}

func TestCheck_OversizedResponseFails(t *testing.T) {
	g := newFakeGitHub(t, "v1.3.0")
	pad := strings.Repeat("a", 2<<20)
	g.respond(200, `{"tag_name":"v1.3.0","html_url":"https://github.com/acme/model-bridge/releases/tag/v1.3.0","pad":"`+pad+`"}`)
	res, err := update.Check(context.Background(), g.cfg())
	if err == nil {
		t.Fatalf("超过大小上限应按失败处理, 得到 %+v", res)
	}
	if res.IsNewer {
		t.Error("失败时 IsNewer 应为 false")
	}
}

func TestCheck_UntrustedReleaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "其他域名", url: "https://evil.example.com/releases/tag/v1.3.0"},
		{name: "http 协议", url: "http://github.com/acme/model-bridge/releases/tag/v1.3.0"},
		{name: "域名前缀相同", url: "https://github.com.evil.example/x"},
		{name: "缺失", url: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newFakeGitHub(t, "v1.3.0")
			g.respond(200, fmt.Sprintf(`{"tag_name":"v1.3.0","html_url":%q}`, tt.url))
			res, err := update.Check(context.Background(), g.cfg())
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if res.IsNewer || res.URL != "" {
				t.Errorf("res = %+v, want 非新版且 URL 为空", res)
			}
		})
	}
}

func TestCheck_DevCurrentVersionNeverNewer(t *testing.T) {
	g := newFakeGitHub(t, "v1.3.0")
	cfg := g.cfg()
	cfg.CurrentVersion = "0.0.0-dev"
	res, err := update.Check(context.Background(), cfg)
	if err != nil || res.IsNewer {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestCheck_EmptyTagNotNewerAndNoError(t *testing.T) {
	g := newFakeGitHub(t, "")
	res, err := update.Check(context.Background(), g.cfg())
	if err != nil || res.IsNewer {
		t.Errorf("res=%+v err=%v", res, err)
	}
	g.respond(200, `{"html_url":"https://github.com/acme/model-bridge/releases"}`)
	res, err = update.Check(context.Background(), g.cfg())
	if err != nil || res.IsNewer {
		t.Errorf("缺失 tag_name: res=%+v err=%v", res, err)
	}
}
