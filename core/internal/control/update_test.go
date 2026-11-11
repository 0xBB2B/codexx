package control_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/control"
)

type githubStub struct {
	*httptest.Server

	mu     sync.Mutex
	status int
	tag    string
	body   string
	gate   chan struct{}
	reqs   []string
}

func newGitHubStub(t *testing.T, tag string) *githubStub {
	t.Helper()
	g := &githubStub{status: http.StatusOK, tag: tag}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.reqs = append(g.reqs, fmt.Sprintf("%s %s\n%v\n%s", r.Method, r.URL.String(), r.Header, body))
		status, tag, custom, gate := g.status, g.tag, g.body, g.gate
		g.mu.Unlock()
		if gate != nil {
			<-gate
		}
		w.WriteHeader(status)
		if custom != "" {
			_, _ = w.Write([]byte(custom))
			return
		}
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/acme/model-bridge/releases/tag/%s"}`, tag, tag)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *githubStub) setStatus(code int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status = code
}

func (g *githubStub) setBody(body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.body = body
}

func (g *githubStub) hits() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.reqs)
}

func (g *githubStub) rawRequests() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.Join(g.reqs, "\n")
}

type stubClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []stubTimer
}

type stubTimer struct {
	at time.Duration
	ch chan time.Time
}

func (c *stubClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, stubTimer{at: c.now + d, ch: ch})
	return ch
}

func (c *stubClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += d
	var rest []stubTimer
	for _, tm := range c.timers {
		if tm.at <= c.now {
			tm.ch <- time.Time{}
		} else {
			rest = append(rest, tm)
		}
	}
	c.timers = rest
}

func (c *stubClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func withUpdate(opts control.Options, g *githubStub, clock *stubClock) control.Options {
	opts.UpdateAPIBase = g.URL
	opts.UpdateRepository = "acme/model-bridge"
	if clock != nil {
		opts.UpdateClock = clock
	}
	return opts
}

func TestCheckUpdate_Manual(t *testing.T) {
	tests := []struct {
		name      string
		tag       string
		wantNewer bool
	}{
		{name: "有新版", tag: "v1.3.0", wantNewer: true},
		{name: "同版本", tag: "v1.2.3"},
		{name: "更旧", tag: "v1.2.2"},
		{name: "预发布标签", tag: "1.3.0-beta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, false)
			g := newGitHubStub(t, tt.tag)
			svc := control.NewService(withUpdate(e.opts, g, nil))
			t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

			res, err := svc.CheckUpdate(context.Background(), true)
			if err != nil {
				t.Fatalf("CheckUpdate: %v", err)
			}
			if res.IsNewer != tt.wantNewer {
				t.Fatalf("IsNewer = %v, want %v", res.IsNewer, tt.wantNewer)
			}
			if !tt.wantNewer {
				return
			}
			wantURL := "https://github.com/acme/model-bridge/releases/tag/" + tt.tag
			if res.LatestVersion == nil || *res.LatestVersion != "1.3.0" || res.URL == nil || *res.URL != wantURL {
				t.Errorf("result = %+v", res)
			}
		})
	}
}

func TestCheckUpdate_NewVersionFillsStatusAndPushesEvent(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	var mu sync.Mutex
	var pushed []control.Status
	opts := withUpdate(e.opts, g, nil)
	opts.OnStatus = func(s control.Status) {
		mu.Lock()
		pushed = append(pushed, s)
		mu.Unlock()
	}
	svc := control.NewService(opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	if _, err := svc.CheckUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	up := svc.Status().Update
	wantURL := "https://github.com/acme/model-bridge/releases/tag/v1.3.0"
	if up.LatestVersion == nil || *up.LatestVersion != "1.3.0" || up.URL == nil || *up.URL != wantURL {
		t.Fatalf("Status.Update = %+v", up)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pushed) == 0 {
		t.Fatal("发现新版后应调用 OnStatus")
	}
	last := pushed[len(pushed)-1].Update
	if last.LatestVersion == nil || *last.LatestVersion != "1.3.0" || last.URL == nil || *last.URL != wantURL {
		t.Errorf("推送的 Status.Update = %+v", last)
	}
}

func TestCheckUpdate_NoNewVersionLeavesStatusUpdateEmpty(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.2.3")
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	if _, err := svc.CheckUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if up := svc.Status().Update; up.LatestVersion != nil || up.URL != nil {
		t.Errorf("Status.Update = %+v, want 空", up)
	}
}

func TestCheckUpdate_ManualFailureReturnsUpdateUnavailable(t *testing.T) {
	tests := []struct {
		name  string
		setup func(g *githubStub)
	}{
		{name: "返回 500", setup: func(g *githubStub) { g.setStatus(500) }},
		{name: "返回 502", setup: func(g *githubStub) { g.setStatus(502) }},
		{name: "连不上", setup: func(g *githubStub) { g.Close() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, false)
			g := newGitHubStub(t, "v1.3.0")
			tt.setup(g)
			svc := control.NewService(withUpdate(e.opts, g, nil))
			t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

			res, err := svc.CheckUpdate(context.Background(), true)
			requireCode(t, err, "update_unavailable")
			if res.IsNewer {
				t.Error("失败时 IsNewer 应为 false")
			}
		})
	}
}

func TestCheckUpdate_NoRepositoryDoesNotRequest(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	opts := withUpdate(e.opts, g, nil)
	opts.UpdateRepository = ""
	svc := control.NewService(opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	for _, manual := range []bool{true, false} {
		res, err := svc.CheckUpdate(context.Background(), manual)
		if err != nil || res.IsNewer {
			t.Errorf("manual=%v res=%+v err=%v", manual, res, err)
		}
	}
	if n := g.hits(); n != 0 {
		t.Errorf("没有仓库名时不该发请求, 实际 %d 次", n)
	}
}

func TestCheckUpdate_RequestCarriesNoUserData(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })
	if _, err := svc.Init(context.Background(), control.InitParams{BaseURL: ptr(e.relay.baseURL()), APIKey: ptr(keyA)}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CheckUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	raw := g.rawRequests()
	if g.hits() != 1 {
		t.Fatalf("请求数 = %d, want 1", g.hits())
	}
	if !strings.Contains(raw, "GET /repos/acme/model-bridge/releases/latest\n") {
		t.Errorf("请求行不对: %s", raw)
	}
	if !strings.Contains(raw, "model-bridge/1.2.3") {
		t.Errorf("User-Agent 应带当前版本: %s", raw)
	}
	for _, secret := range []string{keyA, e.relay.URL, "Authorization"} {
		if strings.Contains(raw, secret) {
			t.Errorf("请求里不该出现 %q: %s", secret, raw)
		}
	}
}

func TestServe_ScheduledUpdateCheck(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	clock := &stubClock{}
	c := startServe(t, withUpdate(e.opts, g, clock))

	waitUntil(t, "首次定时器登记", func() bool { return clock.pending() == 1 })
	if g.hits() != 0 {
		t.Fatal("启动后到 1 分钟前不该检查")
	}
	clock.Advance(time.Minute)
	waitUntil(t, "启动后首次检查", func() bool { return g.hits() == 1 })
	var up map[string]any
	waitUntil(t, "status 记下新版并推送 status 事件", func() bool {
		up, _ = c.result("status", nil)["update"].(map[string]any)
		if up["latest_version"] != "1.3.0" {
			return false
		}
		for _, ev := range c.statusEvents() {
			data, _ := ev["data"].(map[string]any)
			if u, _ := data["update"].(map[string]any); u["latest_version"] == "1.3.0" {
				return true
			}
		}
		return false
	})
	if up["url"] != "https://github.com/acme/model-bridge/releases/tag/v1.3.0" {
		t.Errorf("status.update = %v", up)
	}

	waitUntil(t, "24 小时定时器登记", func() bool { return clock.pending() == 1 })
	clock.Advance(24 * time.Hour)
	waitUntil(t, "24 小时后第二次检查", func() bool { return g.hits() == 2 })
}

func TestServe_ScheduledCheckFailureIsSilent(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	g.setStatus(500)
	clock := &stubClock{}
	c := startServe(t, withUpdate(e.opts, g, clock))

	waitUntil(t, "首次定时器登记", func() bool { return clock.pending() == 1 })
	clock.Advance(time.Minute)
	waitUntil(t, "自动检查已发出", func() bool { return g.hits() == 1 })

	st := c.result("status", nil)
	if up, _ := st["update"].(map[string]any); up["latest_version"] != nil || up["url"] != nil {
		t.Errorf("失败的自动检查不该改动 status.update: %v", up)
	}
	for _, ev := range c.statusEvents() {
		if data, _ := ev["data"].(map[string]any); data["update"] != nil {
			if u := data["update"].(map[string]any); u["latest_version"] != nil {
				t.Errorf("失败的自动检查不该推送新版信息: %v", ev)
			}
		}
	}
}

func TestServe_ManualCheckFailureErrorCode(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	g.setStatus(500)
	c := startServe(t, withUpdate(e.opts, g, &stubClock{}))

	m := c.call("check_update", map[string]any{"manual": true})
	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "update_unavailable" {
		t.Errorf("response = %v", m)
	}
}

func TestServe_NoChecksAfterExit(t *testing.T) {
	tests := []struct {
		name string
		stop func(c *client)
	}{
		{name: "收到 shutdown", stop: func(c *client) {
			c.result("shutdown", map[string]any{"restore": false})
			c.waitExit()
		}},
		{name: "标准输入关闭", stop: func(c *client) {
			_ = c.in.Close()
			c.waitExit()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, false)
			g := newGitHubStub(t, "v1.3.0")
			clock := &stubClock{}
			c := startServe(t, withUpdate(e.opts, g, clock))
			waitUntil(t, "首次定时器登记", func() bool { return clock.pending() == 1 })

			tt.stop(c)
			clock.Advance(48 * time.Hour)
			time.Sleep(100 * time.Millisecond)
			if n := g.hits(); n != 0 {
				t.Errorf("退出后不该再检查, 实际 %d 次", n)
			}
		})
	}
}

func TestServe_ScheduledCheckFailureWritesLog(t *testing.T) {
	const secretBody = "SECRET-RESPONSE-BODY-9f3a"
	e := newEnv(t, false)
	logPath := filepath.Join(e.dataDir, "model-bridge.log")
	if err := os.WriteFile(logPath, []byte("已有日志\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newGitHubStub(t, "v1.3.0")
	g.setStatus(500)
	g.setBody(secretBody)
	clock := &stubClock{}
	startServe(t, withUpdate(e.opts, g, clock))

	waitUntil(t, "首次定时器登记", func() bool { return clock.pending() == 1 })
	clock.Advance(time.Minute)
	waitUntil(t, "失败日志写入", func() bool {
		b, _ := os.ReadFile(logPath)
		return strings.Contains(string(b), "更新检查失败：")
	})
	log := readFile(t, logPath)
	if !strings.HasPrefix(log, "已有日志\n") {
		t.Errorf("已有日志被覆盖: %q", log)
	}
	if !strings.Contains(log, "更新检查失败：") || !strings.Contains(log, "500") {
		t.Errorf("日志应含失败原因: %q", log)
	}
	if strings.Contains(log, secretBody) {
		t.Errorf("日志不该含响应正文: %q", log)
	}
}

func TestCheckUpdate_DevVersionNeverNewer(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	opts := withUpdate(e.opts, g, nil)
	opts.Version = "0.0.0-dev"
	svc := control.NewService(opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil || res.IsNewer {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestCheckUpdate_EmptyTagNotNewerAndNoError(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "")
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil || res.IsNewer {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestServe_ConcurrentAutoAndManualCheck(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	g.gate = make(chan struct{})
	clock := &stubClock{}
	c := startServe(t, withUpdate(e.opts, g, clock))

	waitUntil(t, "首次定时器登记", func() bool { return clock.pending() == 1 })
	clock.Advance(time.Minute)
	waitUntil(t, "自动检查在途", func() bool { return g.hits() == 1 })
	go func() {
		waitUntil(t, "手动检查在途", func() bool { return g.hits() == 2 })
		close(g.gate)
	}()

	res := c.result("check_update", map[string]any{"manual": true})
	if res["is_newer"] != true {
		t.Errorf("result = %v", res)
	}
	waitUntil(t, "状态已更新", func() bool {
		up, _ := c.result("status", nil)["update"].(map[string]any)
		return up["latest_version"] == "1.3.0"
	})
}
