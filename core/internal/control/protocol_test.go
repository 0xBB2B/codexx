package control_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/control"
)

type client struct {
	t      *testing.T
	in     *io.PipeWriter
	lines  chan map[string]any
	done   chan error
	nextID int

	mu     sync.Mutex
	raw    bytes.Buffer
	events []map[string]any
}

func startServe(t *testing.T, opts control.Options) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, lines: make(chan map[string]any, 64), done: make(chan error, 1)}
	go func() {
		c.done <- control.Serve(context.Background(), opts, inR, outW)
		_ = outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			c.mu.Lock()
			c.raw.Write(sc.Bytes())
			c.raw.WriteByte('\n')
			c.mu.Unlock()
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("输出不是单行 JSON: %q", sc.Text())
				continue
			}
			c.lines <- m
		}
		close(c.lines)
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return c
}

func (c *client) next() (map[string]any, bool) {
	select {
	case m, ok := <-c.lines:
		return m, ok
	case <-time.After(10 * time.Second):
		c.t.Fatal("等待核心输出超时")
		return nil, false
	}
}

func (c *client) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	line, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		c.t.Fatal(err)
	}
	for {
		m, ok := c.next()
		if !ok {
			c.t.Fatalf("输出提前结束, 没等到 id=%d 的响应", id)
		}
		if _, isEvent := m["event"]; isEvent {
			c.mu.Lock()
			c.events = append(c.events, m)
			c.mu.Unlock()
			continue
		}
		if got, _ := m["id"].(float64); int(got) != id {
			c.t.Fatalf("响应 id = %v, want %d", m["id"], id)
		}
		return m
	}
}

func (c *client) result(method string, params any) map[string]any {
	c.t.Helper()
	m := c.call(method, params)
	res, ok := m["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s 应返回 result: %v", method, m)
	}
	if _, has := m["error"]; has {
		c.t.Fatalf("%s 不该带 error: %v", method, m)
	}
	return res
}

func (c *client) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw.String()
}

func (c *client) waitExit() error {
	select {
	case err := <-c.done:
		return err
	case <-time.After(5 * time.Second):
		c.t.Fatal("Serve 没有在 5 秒内退出")
		return nil
	}
}

func (c *client) statusEvents() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, e := range c.events {
		if e["event"] == "status" {
			out = append(out, e)
		}
	}
	return out
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func initParams(e *env) map[string]any {
	return map[string]any{"base_url": e.relay.baseURL(), "api_key": keyA, "chatgpt_dir": nil}
}

func TestServe_StatusShape(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	res := c.result("init", initParams(e))

	for _, k := range []string{"version", "forwarding", "proxy", "chatgpt", "relay", "taken_over", "restore_blocked", "config_path", "update"} {
		if _, ok := res[k]; !ok {
			t.Errorf("Status 缺少字段 %q: %v", k, res)
		}
	}
	proxy := res["proxy"].(map[string]any)
	if proxy["running"] != true || proxy["error"] != nil {
		t.Errorf("proxy = %v", proxy)
	}
	relay := res["relay"].(map[string]any)
	if relay["model_count"] != float64(1) || relay["stale"] != false || relay["error"] != nil {
		t.Errorf("relay = %v", relay)
	}
	if s, _ := relay["fetched_at"].(string); s == "" {
		t.Errorf("fetched_at = %v, want RFC3339 字符串", relay["fetched_at"])
	} else if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Errorf("fetched_at 不是 RFC3339: %v", err)
	}
	if _, ok := res["restore_blocked"].([]any); !ok {
		t.Errorf("restore_blocked 应为数组: %v", res["restore_blocked"])
	}
	if _, ok := res["taken_over"].([]any); !ok {
		t.Errorf("taken_over 应为数组: %v", res["taken_over"])
	}
	update := res["update"].(map[string]any)
	if v, ok := update["latest_version"]; !ok || v != nil {
		t.Errorf("update = %v", update)
	}

	again := c.result("status", map[string]any{})
	if !reflect.DeepEqual(again, res) {
		t.Errorf("status 与 init 结果不一致:\n%v\n%v", again, res)
	}
}

func TestServe_Errors(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))

	t.Run("invalid_base_url 不带 keys", func(t *testing.T) {
		m := c.call("save_connection", map[string]any{"base_url": "http://relay.example.com/v1", "api_key": keyA})
		errObj, ok := m["error"].(map[string]any)
		if !ok || errObj["code"] != "invalid_base_url" {
			t.Fatalf("响应 = %v", m)
		}
		if _, has := errObj["keys"]; has {
			t.Errorf("只有 restore_blocked 才带 keys: %v", errObj)
		}
		if _, has := m["result"]; has {
			t.Errorf("错误响应不该带 result: %v", m)
		}
	})

	t.Run("relay_401", func(t *testing.T) {
		m := c.call("save_connection", map[string]any{"base_url": e.relay.baseURL(), "api_key": keyB})
		if errObj, _ := m["error"].(map[string]any); errObj["code"] != "relay_401" {
			t.Fatalf("响应 = %v", m)
		}
	})

	t.Run("restore_blocked 带 keys", func(t *testing.T) {
		e.handEditBaseURL(t)
		m := c.call("set_forwarding", map[string]any{"enabled": false})
		errObj, _ := m["error"].(map[string]any)
		if errObj["code"] != "restore_blocked" || !reflect.DeepEqual(errObj["keys"], []any{"openai_base_url"}) {
			t.Fatalf("响应 = %v", m)
		}
		st := c.result("status", map[string]any{})
		if st["forwarding"] != false || !reflect.DeepEqual(st["restore_blocked"], []any{"openai_base_url"}) {
			t.Errorf("status = %v", st)
		}
	})

	if strings.Contains(c.output(), keyA) || strings.Contains(c.output(), keyB) {
		t.Error("输出里出现了 Key")
	}
}

func TestServe_SaveConnectionResult(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	e.relay.setIDs("deepseek-flash", "deepseek-v4-pro")

	res := c.result("save_connection", map[string]any{"base_url": e.relay.baseURL(), "api_key": keyA})
	if res["models_changed"] != true || res["relay_model_count"] != float64(2) {
		t.Errorf("res = %v", res)
	}
}

func TestServe_SetForwardingPushesStatusEvent(t *testing.T) {
	e := newEnv(t, false)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))

	res := c.result("set_forwarding", map[string]any{"enabled": true})
	if res["forwarding"] != true {
		t.Fatalf("res = %v", res)
	}
	st := c.result("status", map[string]any{})
	if st["forwarding"] != true {
		t.Fatalf("status = %v", st)
	}

	evs := c.statusEvents()
	if len(evs) == 0 {
		t.Fatal("状态变化后应推送 status 事件")
	}
	last := evs[len(evs)-1]
	if _, has := last["id"]; has {
		t.Errorf("事件不该带 id: %v", last)
	}
	data, _ := last["data"].(map[string]any)
	if data["forwarding"] != true {
		t.Errorf("事件 data = %v", data)
	}
}

func TestServe_CheckUpdate(t *testing.T) {
	e := newEnv(t, false)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	for _, manual := range []bool{true, false} {
		res := c.result("check_update", map[string]any{"manual": manual})
		if res["is_newer"] != false || res["latest_version"] != nil || res["url"] != nil {
			t.Errorf("manual=%v res=%v", manual, res)
		}
	}
}

func TestServe_RefreshModelsStale(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	first := c.result("init", initParams(e))
	e.relay.setStatus(500)

	res := c.result("refresh_models", map[string]any{})
	relay := res["relay"].(map[string]any)
	firstRelay := first["relay"].(map[string]any)
	if relay["stale"] != true || relay["error"] != "relay_5xx" || relay["fetched_at"] != firstRelay["fetched_at"] {
		t.Errorf("relay = %v", relay)
	}
}

func TestServe_PrepareUninstall(t *testing.T) {
	t.Run("有手改项", func(t *testing.T) {
		e := newEnv(t, true)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		e.handEditBaseURL(t)

		m := c.call("prepare_uninstall", map[string]any{})
		errObj, _ := m["error"].(map[string]any)
		if errObj["code"] != "restore_blocked" || !reflect.DeepEqual(errObj["keys"], []any{"openai_base_url"}) {
			t.Fatalf("响应 = %v", m)
		}
		if !exists(e.catalogPath()) || !exists(e.dataDir+"/settings.json") {
			t.Error("数据目录文件应保留")
		}
	})

	t.Run("无手改项", func(t *testing.T) {
		e := newEnv(t, true)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))

		res := c.result("prepare_uninstall", map[string]any{})
		if len(res) != 0 {
			t.Errorf("result = %v, want {}", res)
		}
		if e.config(t) != originalConfig || exists(e.catalogPath()) {
			t.Error("应还原配置并删除合并目录")
		}
	})
}

func TestServe_Shutdown(t *testing.T) {
	for _, restore := range []bool{true, false} {
		t.Run("restore="+map[bool]string{true: "true", false: "false"}[restore], func(t *testing.T) {
			e := newEnv(t, true)
			c := startServe(t, e.opts)
			c.result("init", initParams(e))

			res := c.result("shutdown", map[string]any{"restore": restore})
			if len(res) != 0 {
				t.Errorf("result = %v, want {}", res)
			}
			if err := c.waitExit(); err != nil {
				t.Errorf("Serve 返回 %v", err)
			}
			if restored := e.config(t) == originalConfig; restored != restore {
				t.Errorf("配置还原 = %v, want %v", restored, restore)
			}
		})
	}
}

func TestServe_StdinClosedStopsWithoutRestore(t *testing.T) {
	addr := freeAddr(t)
	e := newEnv(t, true, func(e *env) { e.listenAddr = addr })
	c := startServe(t, e.opts)
	st := c.result("init", initParams(e))
	if st["proxy"].(map[string]any)["running"] != true || !proxyReachable(addr) {
		t.Fatalf("status = %v", st)
	}
	taken := e.config(t)

	start := time.Now()
	_ = c.in.Close()
	if err := c.waitExit(); err != nil {
		t.Errorf("Serve 返回 %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("关闭输入后应在 5 秒内退出")
	}
	if e.config(t) != taken {
		t.Errorf("父进程退出不该还原配置: %q", e.config(t))
	}
	if proxyReachable(addr) {
		t.Error("转发服务应已停止")
	}
}

func TestServe_KeyNeverInOutput(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	c.call("save_connection", map[string]any{"base_url": e.relay.baseURL(), "api_key": keyB})
	c.call("save_connection", map[string]any{"base_url": "http://relay.example.com/v1", "api_key": keyB})
	c.result("refresh_models", map[string]any{})
	c.result("shutdown", map[string]any{"restore": false})
	c.waitExit()

	out := c.output()
	for _, k := range []string{keyA, keyB} {
		if strings.Contains(out, k) {
			t.Errorf("输出里出现了 Key %q", k)
		}
	}
	entries, err := os.ReadDir(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(e.dataDir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{keyA, keyB} {
			if bytes.Contains(b, []byte(k)) {
				t.Errorf("%s 里出现了 Key", ent.Name())
			}
		}
	}
}

func TestProtocolDoc_MatchesImplementation(t *testing.T) {
	raw, err := os.ReadFile("../../PROTOCOL.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, want := range []string{
		"| internal_error | 内部错误，请重试 |",
		"| unknown_method | 未知请求 |",
		"| bad_params | 请求参数有误 |",
		"`keys` 在 restore_blocked 和 config_invalid 时出现，列出相关键名",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("PROTOCOL.md 缺少 %q", want)
		}
	}
}
