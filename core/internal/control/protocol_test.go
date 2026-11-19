package control_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/codexauth"
	"modelbridge/internal/control"
)

type client struct {
	t         *testing.T
	in        *io.PipeWriter
	lines     chan map[string]any
	done      chan error
	collected chan struct{}
	nextID    int

	mu     sync.Mutex
	raw    bytes.Buffer
	events []map[string]any
}

func startServe(t *testing.T, opts control.Options) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, lines: make(chan map[string]any, 64), done: make(chan error, 1), collected: make(chan struct{})}
	go func() {
		c.done <- control.Serve(context.Background(), opts, inR, outW)
		_ = outW.Close()
	}()
	go func() {
		defer close(c.collected)
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
		select {
		case <-c.collected:
		case <-time.After(5 * time.Second):
			c.t.Fatal("输出没有在 5 秒内收集完")
		}
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
	return initParamsFor(relayJSON("1", "中转", e.relay.baseURL(), keyA))
}

func initParamsFor(relays ...any) map[string]any {
	if relays == nil {
		relays = []any{}
	}
	return map[string]any{"relays": relays, "chatgpt_dir": nil}
}

func (c *client) errorOf(method string, params any) map[string]any {
	c.t.Helper()
	m := c.call(method, params)
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s 应返回 error: %v", method, m)
	}
	if _, has := m["result"]; has {
		c.t.Fatalf("错误响应不该带 result: %v", m)
	}
	return errObj
}

func (c *client) errCode(method string, params any) string {
	c.t.Helper()
	code, _ := c.errorOf(method, params)["code"].(string)
	return code
}

func relayList(t *testing.T, res map[string]any) []map[string]any {
	t.Helper()
	raw, ok := res["relays"].([]any)
	if !ok {
		t.Fatalf("Status 缺少 relays 数组: %v", res)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("relays 项不是对象: %v", r)
		}
		out = append(out, m)
	}
	return out
}

func relayIDList(t *testing.T, res map[string]any) []string {
	t.Helper()
	var ids []string
	for _, r := range relayList(t, res) {
		id, _ := r["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func relayNames(t *testing.T, res map[string]any) []string {
	t.Helper()
	var names []string
	for _, r := range relayList(t, res) {
		n, _ := r["name"].(string)
		names = append(names, n)
	}
	return names
}

func requireRelaysUnchanged(t *testing.T, c *client, want []map[string]any) {
	t.Helper()
	if got := relayList(t, c.result("status", nil)); !reflect.DeepEqual(got, want) {
		t.Errorf("失败后 relays 应不变:\n%v\n%v", got, want)
	}
}

func requireNewID(t *testing.T, res map[string]any) string {
	t.Helper()
	id, ok := res["id"].(string)
	n, err := strconv.Atoi(id)
	if !ok || err != nil || n <= 0 || strconv.Itoa(n) != id {
		t.Fatalf("新编号 = %v, want 正整数十进制字符串", res["id"])
	}
	return id
}

func twoRelayInit(e *env, other *fakeRelay) map[string]any {
	return initParamsFor(
		relayJSON("1", "中转", e.relay.baseURL(), keyA),
		relayJSON("2", "Bee", other.baseURL(), keyB),
	)
}

func TestServe_StatusShape(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	res := c.result("init", initParams(e))

	for _, k := range []string{"version", "forwarding", "proxy", "chatgpt", "relays", "default_relay", "taken_over", "restore_blocked", "config_path", "update"} {
		if _, ok := res[k]; !ok {
			t.Errorf("Status 缺少字段 %q: %v", k, res)
		}
	}
	proxy := res["proxy"].(map[string]any)
	if proxy["running"] != true || proxy["error"] != nil {
		t.Errorf("proxy = %v", proxy)
	}
	if _, has := res["relay"]; has {
		t.Errorf("Status 不该再有 relay: %v", res)
	}
	if v, ok := res["default_relay"]; !ok || v != nil {
		t.Errorf("default_relay = %v, want null", v)
	}
	relays := relayList(t, res)
	if len(relays) != 1 {
		t.Fatalf("relays = %v", relays)
	}
	relay := relays[0]
	if relay["id"] != "1" || relay["name"] != "中转" || relay["model_count"] != float64(1) || relay["stale"] != false || relay["error"] != nil {
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
		m := c.call("save_relay", relayJSON(nil, "新", "http://relay.example.com/v1", keyA))
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
		m := c.call("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyB))
		if errObj, _ := m["error"].(map[string]any); errObj["code"] != "relay_401" {
			t.Fatalf("响应 = %v", m)
		}
	})

	t.Run("name_duplicate 的 keys 是已有的名字", func(t *testing.T) {
		errObj := c.errorOf("save_relay", relayJSON(nil, "中转", e.relay.baseURL(), keyA))
		if errObj["code"] != "name_duplicate" || !reflect.DeepEqual(errObj["keys"], []any{"中转"}) {
			t.Fatalf("error = %v", errObj)
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

func TestServe_SaveRelayResult(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	e.relay.setIDs("deepseek-flash", "deepseek-v4-pro")

	res := c.result("save_relay", relayJSON("1", "  中转  ", e.relay.baseURL(), keyA))
	want := map[string]any{"id": "1", "name": "中转", "model_count": float64(2), "models_changed": true}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("res = %v, want %v", res, want)
	}
}

func TestServe_SaveRelay_InterfaceModelParamsReachCacheAndCatalog(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	e.relay.setModelsJSON(relayModelJSON("model-a", "low", "high", "max"))
	c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))

	var cache struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(fileBytes(relayCachePath(e, "1")), &cache); err != nil || len(cache.Models) != 1 {
		t.Fatalf("缓存应有 1 个模型: %v %+v", err, cache)
	}
	if m := cache.Models[0]; m["id"] != "model-a" || m["context_length"] != float64(1048576) || !reflect.DeepEqual(m["reasoning_efforts"], []any{"low", "high", "max"}) {
		t.Errorf("缓存里的模型 = %v", m)
	}
	var entry map[string]any
	for _, m := range catalogModels(t, e) {
		if m["slug"] == "relay-model-a" {
			entry = m
		}
	}
	if entry == nil {
		t.Fatal("目录里没有 relay-model-a")
	}
	if entry["context_window"] != float64(1048576) {
		t.Errorf("context_window = %v, want 1048576", entry["context_window"])
	}
	if levels, _ := entry["supported_reasoning_levels"].([]any); len(levels) != 3 {
		t.Errorf("supported_reasoning_levels = %v, want 3 档", entry["supported_reasoning_levels"])
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

	res := c.result("refresh_models", map[string]any{"id": nil})
	relay := relayList(t, res)[0]
	firstRelay := relayList(t, first)[0]
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
	c.call("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyB))
	c.call("save_relay", relayJSON(nil, "新", "http://relay.example.com/v1", keyB))
	c.result("refresh_models", map[string]any{"id": nil})
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
		"`keys` 在 restore_blocked 和 config_invalid 时出现，列出相关键名；在 name_duplicate 时放已有的那个配置名字。",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("PROTOCOL.md 缺少 %q", want)
		}
	}
	if !strings.Contains(doc, "| proxy_start_timeout |") {
		t.Error("PROTOCOL.md 错误码表缺少 proxy_start_timeout 这一行")
	}
}

func TestServe_AccessMethods(t *testing.T) {
	e := newEnv(t, true, withDetect(codexauth.LoggedIn))
	c := startServe(t, e.opts)
	res := c.result("init", initParams(e))
	if want := map[string]any{"mode": "both", "target": "both", "detect": "logged_in"}; !reflect.DeepEqual(res["access"], want) {
		t.Errorf("init access = %v, want %v", res["access"], want)
	}

	e.detect.set(codexauth.NotLoggedIn)
	res = c.result("detect_access", nil)
	if want := map[string]any{"mode": "both", "target": "relay", "detect": "not_logged_in"}; !reflect.DeepEqual(res["access"], want) {
		t.Errorf("detect_access access = %v, want %v", res["access"], want)
	}
	for _, k := range []string{"version", "forwarding", "proxy", "relays", "default_relay", "config_path"} {
		if _, ok := res[k]; !ok {
			t.Errorf("detect_access 应返回完整状态, 缺少 %q", k)
		}
	}

	res = c.result("apply_access", map[string]any{})
	if want := map[string]any{"mode": "relay", "target": "relay", "detect": "not_logged_in"}; !reflect.DeepEqual(res["access"], want) {
		t.Errorf("apply_access access = %v, want %v", res["access"], want)
	}
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("apply_access 后配置不是中转写法: %q", cfg)
	}
	if again := c.result("status", nil); !reflect.DeepEqual(again["access"], res["access"]) {
		t.Errorf("status.access = %v, want %v", again["access"], res["access"])
	}
}

func TestServe_AccessNullWhenForwardingOff(t *testing.T) {
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	c := startServe(t, e.opts)
	res := c.result("init", initParams(e))
	if want := map[string]any{"mode": nil, "target": nil, "detect": nil}; !reflect.DeepEqual(res["access"], want) {
		t.Errorf("access = %v, want %v", res["access"], want)
	}
}

func TestServe_SaveConnectionRemoved(t *testing.T) {
	e := newEnv(t, false)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	if code := c.errCode("save_connection", map[string]any{"base_url": e.relay.baseURL(), "api_key": keyA}); code != "unknown_method" {
		t.Errorf("code = %q, want unknown_method", code)
	}
}

func TestServe_Init_RelaysInStatus(t *testing.T) {
	e := newEnv(t, true)
	other := newFakeRelay(t, []string{"deepseek-flash", "gpt-a"}, keyB)
	c := startServe(t, e.opts)
	res := c.result("init", twoRelayInit(e, other))

	if _, has := res["relay"]; has {
		t.Errorf("Status 不该再有 relay: %v", res)
	}
	if v, ok := res["default_relay"]; !ok || v != nil {
		t.Errorf("default_relay = %v, want null", v)
	}
	rs := relayList(t, res)
	if len(rs) != 2 {
		t.Fatalf("relays = %v", rs)
	}
	if rs[0]["id"] != "1" || rs[0]["name"] != "中转" || rs[0]["model_count"] != float64(1) {
		t.Errorf("relays[0] = %v", rs[0])
	}
	if rs[1]["id"] != "2" || rs[1]["name"] != "Bee" || rs[1]["model_count"] != float64(2) {
		t.Errorf("relays[1] = %v", rs[1])
	}
	for i, r := range rs {
		if s, _ := r["fetched_at"].(string); s == "" || r["stale"] != false || r["error"] != nil {
			t.Errorf("relays[%d] = %v", i, r)
		}
	}
}

func TestServe_Init_BadRelayID(t *testing.T) {
	for _, id := range []string{"abc", "../x", "0", "-1", "1.5", "", "1e2", "01", "+1", "1.0"} {
		t.Run("编号="+id, func(t *testing.T) {
			e := newEnv(t, false)
			c := startServe(t, e.opts)
			if code := c.errCode("init", initParamsFor(relayJSON(id, "中转", e.relay.baseURL(), keyA))); code != "bad_params" {
				t.Errorf("code = %q, want bad_params", code)
			}
		})
	}
}

func TestServe_SaveRelay_BadRelayID(t *testing.T) {
	for _, id := range []string{"abc", "../x", "0", "-1", "1.5", "", "1e2", "01", "+1", "1.0"} {
		t.Run("编号="+id, func(t *testing.T) {
			e := newEnv(t, false)
			c := startServe(t, e.opts)
			before := relayList(t, c.result("init", initParams(e)))
			hits := e.relay.hits()
			if code := c.errCode("save_relay", relayJSON(id, "新", e.relay.baseURL(), keyA)); code != "bad_params" {
				t.Errorf("code = %q, want bad_params", code)
			}
			requireRelaysUnchanged(t, c, before)
			if e.relay.hits() != hits {
				t.Error("编号非法时不该访问中转")
			}
		})
	}
}

func TestServe_SaveRelay_Add(t *testing.T) {
	t.Run("新编号比现有编号都大且缺省从 2 起算", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))

		res := c.result("save_relay", relayJSON(nil, "  新家  ", e.relay.baseURL(), keyA))
		if id := requireNewID(t, res); id != "3" {
			t.Errorf("id = %s, want 3", id)
		}
		if res["name"] != "新家" || res["model_count"] != float64(1) || res["models_changed"] != true {
			t.Errorf("res = %v", res)
		}
		st := c.result("status", nil)
		if !reflect.DeepEqual(relayIDList(t, st), []string{"1", "2", "3"}) || !reflect.DeepEqual(relayNames(t, st), []string{"中转", "Bee", "新家"}) {
			t.Errorf("relays = %v", relayList(t, st))
		}
	})

	t.Run("编号从 settings.json 的 next_relay_id 开始并递增", func(t *testing.T) {
		e := newEnv(t, false, withDetect(codexauth.LoggedIn), withSettings(`{"forwarding":false,"next_relay_id":7}`))
		c := startServe(t, e.opts)
		c.result("init", initParams(e))

		if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "甲", e.relay.baseURL(), keyA))); id != "7" {
			t.Errorf("id = %s, want 7", id)
		}
		if got := fmt.Sprint(settingsJSON(t, e)["next_relay_id"]); got != "8" {
			t.Errorf("next_relay_id = %s, want 8", got)
		}
		if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "乙", e.relay.baseURL(), keyA))); id != "8" {
			t.Errorf("id = %s, want 8", id)
		}
		c.result("set_forwarding", map[string]any{"enabled": true})
		if got := fmt.Sprint(settingsJSON(t, e)["next_relay_id"]); got != "9" {
			t.Errorf("改其他设置后 next_relay_id = %s, want 9", got)
		}
	})

	t.Run("next_relay_id 不大于现有编号时仍给更大的编号", func(t *testing.T) {
		e := newEnv(t, false, withSettings(`{"forwarding":false,"next_relay_id":1}`))
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyA))); id != "3" {
			t.Errorf("id = %s, want 3", id)
		}
	})

	t.Run("删除后新增不会拿到被删的编号", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		c.result("delete_relay", map[string]any{"id": "2"})

		id := requireNewID(t, c.result("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyA)))
		if id == "2" || id == "1" {
			t.Errorf("id = %s, 不该复用 1 或已删除的 2", id)
		}
	})

	t.Run("重启后用过的编号不再分配", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyA))); id != "3" {
			t.Fatalf("id = %s, want 3", id)
		}
		c.result("delete_relay", map[string]any{"id": "3"})
		c.result("shutdown", map[string]any{"restore": false})
		if err := c.waitExit(); err != nil {
			t.Fatal(err)
		}

		c2 := startServe(t, e.opts)
		c2.result("init", twoRelayInit(e, other))
		if id := requireNewID(t, c2.result("save_relay", relayJSON(nil, "又新", e.relay.baseURL(), keyA))); id != "4" {
			t.Errorf("重启后 id = %s, want 4", id)
		}
	})
}

func TestServe_SaveRelay_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		id        any
		relayName string
		base      string
		key       string
		wantCode  string
		wantKeys  []any
	}{
		{"名字为空", nil, "", "", keyA, "name_empty", nil},
		{"名字全是空格", nil, "   ", "", keyA, "name_empty", nil},
		{"名字 17 个汉字", nil, strings.Repeat("字", 17), "", keyA, "name_too_long", nil},
		{"名字 17 个字母", nil, strings.Repeat("a", 17), "", keyA, "name_too_long", nil},
		{"名字含点", nil, "A·B", "", keyA, "name_has_dot", nil},
		{"名字和其他配置不区分大小写重复", nil, "bEE", "", keyA, "name_duplicate", []any{"Bee"}},
		{"名字去空格后重复", nil, "  中转 ", "", keyA, "name_duplicate", []any{"中转"}},
		{"编辑时改成别的配置的名字", "2", "中转", "", keyB, "name_duplicate", []any{"中转"}},
		{"地址是 http", nil, "新", "http://relay.example.com/v1", keyA, "invalid_base_url", nil},
		{"地址没有协议", nil, "新", "relay.example.com/v1", keyA, "invalid_base_url", nil},
		{"地址为空", nil, "新", "NONE", keyA, "invalid_base_url", nil},
		{"配置不存在", "99", "新", "", keyA, "relay_not_found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, true)
			other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
			c := startServe(t, e.opts)
			before := relayList(t, c.result("init", twoRelayInit(e, other)))
			hits1, hits2 := e.relay.hits(), other.hits()
			cfgBefore := e.config(t)

			base := tt.base
			switch base {
			case "":
				base = e.relay.baseURL()
			case "NONE":
				base = ""
			}
			errObj := c.errorOf("save_relay", relayJSON(tt.id, tt.relayName, base, tt.key))
			if errObj["code"] != tt.wantCode {
				t.Fatalf("error = %v, want code %s", errObj, tt.wantCode)
			}
			if tt.wantKeys != nil && !reflect.DeepEqual(errObj["keys"], tt.wantKeys) {
				t.Errorf("keys = %v, want %v", errObj["keys"], tt.wantKeys)
			}
			requireRelaysUnchanged(t, c, before)
			if e.relay.hits() != hits1 || other.hits() != hits2 {
				t.Error("校验不通过时不该访问任何中转")
			}
			if e.config(t) != cfgBefore {
				t.Error("校验不通过时不该改 Codex 配置")
			}
		})
	}

	t.Run("Key 为空", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		before := relayList(t, c.result("init", initParams(e)))
		hits := e.relay.hits()
		if code := c.errCode("save_relay", relayJSON(nil, "新", e.relay.baseURL(), "")); code != "bad_params" {
			t.Errorf("code = %q, want bad_params", code)
		}
		requireRelaysUnchanged(t, c, before)
		if e.relay.hits() != hits {
			t.Error("Key 为空时不该访问中转")
		}
	})
}

func TestServe_SaveRelay_NameRules(t *testing.T) {
	t.Run("去首尾空格后存储", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		res := c.result("save_relay", relayJSON(nil, "  B家  ", e.relay.baseURL(), keyA))
		if res["name"] != "B家" {
			t.Errorf("name = %v, want B家", res["name"])
		}
		if names := relayNames(t, c.result("status", nil)); !slices.Contains(names, "B家") || slices.Contains(names, "  B家  ") {
			t.Errorf("names = %q", names)
		}
	})

	t.Run("16 个汉字可以", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		name := strings.Repeat("字", 16)
		if res := c.result("save_relay", relayJSON(nil, name, e.relay.baseURL(), keyA)); res["name"] != name {
			t.Errorf("name = %v", res["name"])
		}
	})

	t.Run("编辑时名字只是大小写变化不算重名", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		if res := c.result("save_relay", relayJSON("2", "BEE", other.baseURL(), keyB)); res["id"] != "2" || res["name"] != "BEE" || res["models_changed"] != false {
			t.Errorf("res = %v", res)
		}
		if names := relayNames(t, c.result("status", nil)); !reflect.DeepEqual(names, []string{"中转", "BEE"}) {
			t.Errorf("names = %q", names)
		}
	})
}

func TestServe_SaveRelay_Limit(t *testing.T) {
	e := newEnv(t, false)
	var relays []any
	for i := 1; i <= 9; i++ {
		relays = append(relays, relayJSON(strconv.Itoa(i), "N"+strconv.Itoa(i), e.relay.baseURL(), keyA))
	}
	c := startServe(t, e.opts)
	c.result("init", initParamsFor(relays...))

	requireNewID(t, c.result("save_relay", relayJSON(nil, "第十个", e.relay.baseURL(), keyA)))
	before := relayList(t, c.result("status", nil))
	if len(before) != 10 {
		t.Fatalf("relays = %d, want 10", len(before))
	}
	hits := e.relay.hits()
	if code := c.errCode("save_relay", relayJSON(nil, "第十一个", e.relay.baseURL(), keyA)); code != "relay_limit" {
		t.Errorf("code = %q, want relay_limit", code)
	}
	requireRelaysUnchanged(t, c, before)
	if e.relay.hits() != hits {
		t.Error("超过上限时不该访问中转")
	}
	if res := c.result("save_relay", relayJSON("5", "N5", e.relay.baseURL(), keyA)); res["id"] != "5" {
		t.Errorf("已满时编辑现有配置仍应可以: %v", res)
	}
}

func TestServe_SaveRelay_RelayErrorsKeepState(t *testing.T) {
	tests := []struct {
		status   int
		wantCode string
	}{
		{401, "relay_401"},
		{403, "relay_403"},
		{404, "relay_404"},
		{429, "relay_429"},
		{500, "relay_5xx"},
		{418, "relay_4xx"},
	}
	for _, tt := range tests {
		t.Run(tt.wantCode, func(t *testing.T) {
			e := newEnv(t, false)
			c := startServe(t, e.opts)
			before := relayList(t, c.result("init", initParams(e)))
			e.relay.setStatus(tt.status)
			if code := c.errCode("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyB)); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			requireRelaysUnchanged(t, c, before)
		})
	}

	t.Run("relay_unreachable", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		before := relayList(t, c.result("init", initParams(e)))
		base := e.relay.baseURL()
		e.relay.Close()
		if code := c.errCode("save_relay", relayJSON(nil, "新", base, keyB)); code != "relay_unreachable" {
			t.Errorf("code = %q, want relay_unreachable", code)
		}
		requireRelaysUnchanged(t, c, before)
	})

	t.Run("新增失败后下次新增仍从原编号起", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		if code := c.errCode("save_relay", relayJSON(nil, "新", e.relay.baseURL(), "sk-example-bad")); code != "relay_401" {
			t.Fatalf("code = %q", code)
		}
		if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyA))); id != "2" {
			t.Errorf("id = %s, want 2", id)
		}
	})
}

func TestServe_SaveRelay_EditFailureKeepsOldConnection(t *testing.T) {
	e := newEnv(t, true)
	c := startServe(t, e.opts)
	before := relayList(t, c.result("init", initParams(e)))

	if code := c.errCode("save_relay", relayJSON("1", "中转", e.relay.baseURL(), "sk-example-bad")); code != "relay_401" {
		t.Fatalf("code = %q, want relay_401", code)
	}
	requireRelaysUnchanged(t, c, before)
	if code := postViaProxy(t, e.listenAddr, "relay-deepseek-flash"); code != http.StatusOK {
		t.Fatalf("转发状态码 = %d", code)
	}
	if auths := e.relay.forwardedAuths(); len(auths) != 1 || auths[0] != "Bearer "+keyA {
		t.Errorf("中转收到的 Authorization = %v, want 旧 Key", auths)
	}
	res := c.result("refresh_models", map[string]any{"id": nil})
	if r := relayList(t, res)[0]; r["error"] != nil {
		t.Errorf("旧连接应仍可用: %v", r)
	}
}

func TestServe_SaveRelay_EditApplies(t *testing.T) {
	t.Run("模型有变化才报 models_changed", func(t *testing.T) {
		e := newEnv(t, true)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))

		res := c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
		if res["models_changed"] != false || res["model_count"] != float64(1) {
			t.Fatalf("集合没变: %v", res)
		}
		e.relay.setIDs("deepseek-flash", "deepseek-v4-pro")
		res = c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
		if res["models_changed"] != true || res["model_count"] != float64(2) {
			t.Fatalf("多一个模型: %v", res)
		}
		if !strings.Contains(readFile(t, e.catalogPath()), "deepseek-v4-pro") {
			t.Error("保存后合并目录没有重新生成")
		}
	})

	t.Run("转发改用新 Key", func(t *testing.T) {
		e := newEnv(t, true)
		e.relay.keys[keyB] = true
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyB))
		if code := postViaProxy(t, e.listenAddr, "relay-deepseek-flash"); code != http.StatusOK {
			t.Fatalf("转发状态码 = %d", code)
		}
		if auths := e.relay.forwardedAuths(); len(auths) != 1 || auths[0] != "Bearer "+keyB {
			t.Errorf("中转收到的 Authorization = %v, want 新 Key", auths)
		}
	})

	t.Run("重建目录失败时回滚到旧 Key", func(t *testing.T) {
		e := newEnv(t, true)
		e.relay.keys[keyB] = true
		c := startServe(t, e.opts)
		c.result("init", initParams(e))

		if err := os.Chmod(e.codexPath, 0o644); err != nil {
			t.Fatal(err)
		}
		code := c.errCode("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyB))
		if err := os.Chmod(e.codexPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if code != "chatgpt_unsupported" {
			t.Fatalf("code = %q, want chatgpt_unsupported", code)
		}
		res := c.result("refresh_models", map[string]any{"id": nil})
		if r := relayList(t, res)[0]; r["error"] != nil {
			t.Errorf("回滚后应仍用旧 Key: %v", r)
		}
		if code := postViaProxy(t, e.listenAddr, "relay-deepseek-flash"); code != http.StatusOK {
			t.Fatalf("转发状态码 = %d", code)
		}
		if auths := e.relay.forwardedAuths(); len(auths) != 1 || auths[0] != "Bearer "+keyA {
			t.Errorf("中转收到的 Authorization = %v, want 旧 Key", auths)
		}
	})

	for _, forwarding := range []bool{true, false} {
		t.Run("一次保存只请求一次模型列表 forwarding="+strconv.FormatBool(forwarding), func(t *testing.T) {
			e := newEnv(t, forwarding)
			c := startServe(t, e.opts)
			c.result("init", initParams(e))
			before := e.relay.hits()
			c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
			if got := e.relay.hits() - before; got != 1 {
				t.Errorf("一次保存请求了 %d 次 /models, want 1", got)
			}
		})
	}
}

func TestServe_RefreshModels(t *testing.T) {
	t.Run("null 刷新全部都联网", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		h1, h2 := e.relay.hits(), other.hits()
		c.result("refresh_models", map[string]any{"id": nil})
		if e.relay.hits()-h1 != 1 || other.hits()-h2 != 1 {
			t.Errorf("请求次数增量 = %d/%d, want 1/1", e.relay.hits()-h1, other.hits()-h2)
		}
	})

	t.Run("带 id 只刷新该配置", func(t *testing.T) {
		e := newEnv(t, false)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		h1, h2 := e.relay.hits(), other.hits()
		res := c.result("refresh_models", map[string]any{"id": "2"})
		if e.relay.hits() != h1 || other.hits()-h2 != 1 {
			t.Errorf("请求次数增量 = %d/%d, want 0/1", e.relay.hits()-h1, other.hits()-h2)
		}
		if len(relayList(t, res)) != 2 {
			t.Errorf("应返回完整 Status: %v", res)
		}
	})

	t.Run("id 不存在", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		if code := c.errCode("refresh_models", map[string]any{"id": "9"}); code != "relay_not_found" {
			t.Errorf("code = %q, want relay_not_found", code)
		}
	})

	t.Run("一个失败只标这一个过期并保留上次记录", func(t *testing.T) {
		e := newEnv(t, true)
		other := newFakeRelay(t, []string{"deepseek-flash", "gpt-a"}, keyB)
		c := startServe(t, e.opts)
		first := relayList(t, c.result("init", twoRelayInit(e, other)))

		other.setStatus(500)
		rs := relayList(t, c.result("refresh_models", map[string]any{"id": nil}))
		if rs[0]["stale"] != false || rs[0]["error"] != nil {
			t.Errorf("正常的中转不受影响: %v", rs[0])
		}
		if rs[1]["stale"] != true || rs[1]["error"] != "relay_5xx" || rs[1]["model_count"] != first[1]["model_count"] || rs[1]["fetched_at"] != first[1]["fetched_at"] {
			t.Errorf("失败的中转应沿用上次记录: %v, 上次 %v", rs[1], first[1])
		}

		other.setStatus(0)
		other.setIDs("deepseek-flash", "deepseek-v4-pro", "gpt-a")
		rs = relayList(t, c.result("refresh_models", map[string]any{"id": "2"}))
		if rs[1]["stale"] != false || rs[1]["error"] != nil || rs[1]["model_count"] != float64(3) {
			t.Errorf("恢复后应清除过期标记: %v", rs[1])
		}
	})

	t.Run("从未成功过时模型数为 0 且 fetched_at 为 null", func(t *testing.T) {
		e := newEnv(t, true)
		e.relay.setStatus(401)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		r := relayList(t, c.result("refresh_models", map[string]any{"id": nil}))[0]
		if r["stale"] != true || r["error"] != "relay_401" || r["model_count"] != float64(0) || r["fetched_at"] != nil {
			t.Errorf("relay = %v", r)
		}
	})
}

func TestServe_ModelCount(t *testing.T) {
	t.Run("转发开着和关着数量一致，陌生模型名也计数", func(t *testing.T) {
		e := newEnv(t, true)
		e.relay.setIDs("deepseek-flash", "gpt-a", "unknown-model-x")
		c := startServe(t, e.opts)
		st := c.result("init", initParams(e))
		if n := relayList(t, st)[0]["model_count"]; n != float64(3) {
			t.Fatalf("转发开着: model_count = %v, want 3", n)
		}
		c.result("set_forwarding", map[string]any{"enabled": false})
		st = c.result("refresh_models", map[string]any{"id": nil})
		if n := relayList(t, st)[0]["model_count"]; n != float64(3) {
			t.Errorf("转发关着刷新: model_count = %v, want 3", n)
		}
		res := c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
		if res["model_count"] != float64(3) || res["models_changed"] != false {
			t.Errorf("转发关着保存: %v", res)
		}
	})

	t.Run("转发关着且找不到 codex 接口返回的全部计数", func(t *testing.T) {
		e := newEnv(t, false, func(e *env) { e.codexPath = filepath.Join(t.TempDir(), "no-such-codex") })
		e.relay.setIDs("model-a", "auto", "review-x")
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		st := c.result("refresh_models", map[string]any{"id": nil})
		if n := relayList(t, st)[0]["model_count"]; n != float64(3) {
			t.Errorf("model_count = %v, want 3", n)
		}
		res := c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
		if res["model_count"] != float64(3) || res["models_changed"] != false {
			t.Errorf("save_relay = %v", res)
		}
	})

	t.Run("save_relay 返回陌生模型名时计入模型数并写进目录", func(t *testing.T) {
		e := newEnv(t, true)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		e.relay.setIDs("never-seen-model")
		res := c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
		if res["model_count"] != float64(1) || res["models_changed"] != true {
			t.Errorf("save_relay = %v", res)
		}
		if !strings.Contains(readFile(t, e.catalogPath()), "relay-never-seen-model") {
			t.Errorf("目录里应有 relay-never-seen-model: %s", readFile(t, e.catalogPath()))
		}
	})

	const reviewHidden = `{"models":[{"slug":"gpt-a","priority":3,"display_name":"A"},{"slug":"review-x","priority":7,"display_name":"R","visibility":"hide"}]}`
	t.Run("转发开着：同名官方隐藏模型不计数，默认模型是 relay-auto", func(t *testing.T) {
		e := newEnv(t, true, func(e *env) { e.codexPath = writeFakeCodex(t, reviewHidden, 0) })
		e.relay.setIDs("model-a", "auto", "review-x")
		c := startServe(t, e.opts)
		st := c.result("init", initParams(e))
		if n := relayList(t, st)[0]["model_count"]; n != float64(2) {
			t.Errorf("model_count = %v, want 2", n)
		}
		if !strings.Contains(e.config(t), `model = "relay-auto"`) {
			t.Errorf("默认模型应为 relay-auto: %s", e.config(t))
		}
	})

	t.Run("转发关着：同名官方隐藏模型不计数，找不到 ChatGPT 时全计", func(t *testing.T) {
		e := newEnv(t, false, func(e *env) { e.codexPath = writeFakeCodex(t, reviewHidden, 0) })
		e.relay.setIDs("model-a", "auto", "review-x")
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		st := c.result("refresh_models", map[string]any{"id": nil})
		if n := relayList(t, st)[0]["model_count"]; n != float64(2) {
			t.Errorf("找到 ChatGPT: model_count = %v, want 2", n)
		}

		e2 := newEnv(t, false, func(e *env) { e.codexPath = filepath.Join(t.TempDir(), "no-such-codex") })
		e2.relay.setIDs("model-a", "auto", "review-x")
		c2 := startServe(t, e2.opts)
		c2.result("init", initParams(e2))
		st = c2.result("refresh_models", map[string]any{"id": nil})
		if n := relayList(t, st)[0]["model_count"]; n != float64(3) {
			t.Errorf("找不到 ChatGPT: model_count = %v, want 3", n)
		}
	})

	const withHidden = `{"models":[{"slug":"gpt-a","priority":3,"display_name":"A"},{"slug":"gpt-h","priority":7,"display_name":"H","visibility":"hide"}]}`
	for _, forwarding := range []bool{true, false} {
		t.Run("官方 hide 条目同名的不计 forwarding="+strconv.FormatBool(forwarding), func(t *testing.T) {
			e := newEnv(t, forwarding, func(e *env) { e.codexPath = writeFakeCodex(t, withHidden, 0) })
			e.relay.setIDs("deepseek-flash", "gpt-a", "gpt-h")
			c := startServe(t, e.opts)
			c.result("init", initParams(e))
			st := c.result("refresh_models", map[string]any{"id": nil})
			if n := relayList(t, st)[0]["model_count"]; n != float64(2) {
				t.Errorf("model_count = %v, want 2", n)
			}
			res := c.result("save_relay", relayJSON("1", "中转", e.relay.baseURL(), keyA))
			if res["model_count"] != float64(2) || res["models_changed"] != false {
				t.Errorf("save_relay = %v", res)
			}
		})
	}

	t.Run("官方只留账号名单里的，转发关着数量不变", func(t *testing.T) {
		e := newEnv(t, true, func(e *env) { e.codexPath = writeFakeCodex(t, threeOfficialJSON, 0) })
		writeModelsCache(t, e, `{"fetched_at":"2026-01-01T00:00:00Z","etag":"e","client_version":"0.0.0","identity":"placeholder","models":[{"slug":"gpt-c","visibility":"list"},{"slug":"gpt-a","visibility":"list"}]}`)
		e.relay.setIDs("gpt-b", "deepseek-flash")
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		c.result("set_forwarding", map[string]any{"enabled": false})
		st := c.result("refresh_models", map[string]any{"id": nil})
		if n := relayList(t, st)[0]["model_count"]; n != float64(2) {
			t.Errorf("转发关着 model_count = %v, want 2", n)
		}
	})
}

func TestServe_DeleteRelay(t *testing.T) {
	t.Run("删除后重新生成目录并继续转发", func(t *testing.T) {
		e := newEnv(t, true)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		if !strings.Contains(readFile(t, e.catalogPath()), "relay2-deepseek-flash") {
			t.Fatal("删除前合并目录应含第二个中转的模型")
		}

		st := c.result("delete_relay", map[string]any{"id": "2"})
		if !reflect.DeepEqual(relayIDList(t, st), []string{"1"}) {
			t.Errorf("relays = %v", relayList(t, st))
		}
		if strings.Contains(readFile(t, e.catalogPath()), "relay2-deepseek-flash") {
			t.Error("删除后合并目录不该再有被删中转的模型")
		}
		if code := postViaProxy(t, e.listenAddr, "relay-deepseek-flash"); code != http.StatusOK {
			t.Errorf("剩下的中转转发状态码 = %d", code)
		}
	})

	t.Run("只剩一个时 relay_last 且状态不变", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		before := relayList(t, c.result("init", initParams(e)))
		if code := c.errCode("delete_relay", map[string]any{"id": "1"}); code != "relay_last" {
			t.Errorf("code = %q, want relay_last", code)
		}
		requireRelaysUnchanged(t, c, before)
	})

	t.Run("配置不存在", func(t *testing.T) {
		e := newEnv(t, false)
		c := startServe(t, e.opts)
		before := relayList(t, c.result("init", initParams(e)))
		if code := c.errCode("delete_relay", map[string]any{"id": "99"}); code != "relay_not_found" {
			t.Errorf("code = %q, want relay_not_found", code)
		}
		requireRelaysUnchanged(t, c, before)
	})
}

func TestServe_DefaultModel(t *testing.T) {
	cases := []struct {
		name        string
		ids1, ids2  []string
		reversed    bool
		wantModel   string
		wantDefault any
	}{
		{"配置 1 没有 auto 配置 2 有", []string{"deepseek-flash"}, []string{"auto", "deepseek-flash"}, false, "relay2-auto", "Bee"},
		{"都有 auto 取列表里第一个", []string{"auto"}, []string{"auto"}, false, "relay-auto", "中转"},
		{"列表顺序决定第一个", []string{"auto"}, []string{"auto"}, true, "relay2-auto", "Bee"},
		{"都没有 auto 不写 model", []string{"deepseek-flash"}, []string{"deepseek-flash"}, false, "", nil},
	}
	modes := []struct {
		name   string
		result codexauth.Result
	}{
		{"只用中转", codexauth.NotLoggedIn},
		{"官方加中转", codexauth.LoggedIn},
	}
	for _, mode := range modes {
		for _, tt := range cases {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				e := newEnv(t, true, withDetect(mode.result))
				e.relay.setIDs(tt.ids1...)
				other := newFakeRelay(t, tt.ids2, keyB)
				params := twoRelayInit(e, other)
				if tt.reversed {
					rs := params["relays"].([]any)
					params["relays"] = []any{rs[1], rs[0]}
				}
				c := startServe(t, e.opts)
				st := c.result("init", params)

				if st["default_relay"] != tt.wantDefault {
					t.Errorf("default_relay = %v, want %v", st["default_relay"], tt.wantDefault)
				}
				m := parseConfig(e.config(t))
				if tt.wantModel == "" {
					if _, has := m["model"]; has {
						t.Errorf("没有 auto 时不该写 model: %v", m["model"])
					}
				} else if m["model"] != tt.wantModel {
					t.Errorf("model = %v, want %s", m["model"], tt.wantModel)
				}
			})
		}
	}

	t.Run("改名后内部名不变默认模型所在配置名跟着变", func(t *testing.T) {
		e := newEnv(t, true, withDetect(codexauth.NotLoggedIn))
		other := newFakeRelay(t, []string{"auto", "deepseek-flash"}, keyB)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		if !slices.Contains(catalogSlugs(catalogModels(t, e)), any("relay2-auto")) {
			t.Fatalf("改名前目录应有 relay2-auto: %v", catalogSlugs(catalogModels(t, e)))
		}

		res := c.result("save_relay", relayJSON("2", "改名", other.baseURL(), keyB))
		if res["models_changed"] != false {
			t.Errorf("只改名模型不变, models_changed = %v", res["models_changed"])
		}
		if slugs := catalogSlugs(catalogModels(t, e)); !slices.Contains(slugs, any("relay2-deepseek-flash")) {
			t.Errorf("编号 2 的非 auto 模型内部名应为 relay2-deepseek-flash: %v", slugs)
		}
		if !slices.Contains(catalogSlugs(catalogModels(t, e)), any("relay2-auto")) {
			t.Errorf("改名后目录里的内部名应不变: %v", catalogSlugs(catalogModels(t, e)))
		}
		if m := parseConfig(e.config(t)); m["model"] != "relay2-auto" {
			t.Errorf("model = %v, want relay2-auto", m["model"])
		}
		if st := c.result("status", nil); st["default_relay"] != "改名" {
			t.Errorf("default_relay = %v, want 改名", st["default_relay"])
		}
	})

	t.Run("刷新后出现或消失 auto 时写入和还原 model", func(t *testing.T) {
		e := newEnv(t, true)
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		if strings.Contains(e.config(t), `model = "relay-auto"`) {
			t.Fatalf("初始配置不该有 auto: %q", e.config(t))
		}

		e.relay.setIDs("auto", "deepseek-flash")
		c.result("refresh_models", map[string]any{"id": nil})
		if !strings.Contains(e.config(t), `model = "relay-auto"`) {
			t.Errorf("出现 auto 后应写入 model: %q", e.config(t))
		}

		e.relay.setIDs("deepseek-flash")
		c.result("refresh_models", map[string]any{"id": nil})
		if strings.Contains(e.config(t), `model = "relay-auto"`) {
			t.Errorf("auto 消失后应还原 model: %q", e.config(t))
		}
	})
}

func TestServe_RelayOnlySurvivesSaveRelayAndRefresh(t *testing.T) {
	e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	c.result("set_forwarding", map[string]any{"enabled": true})
	other := newFakeRelay(t, []string{"deepseek-flash", "gpt-a"}, keyB)

	c.result("save_relay", relayJSON("1", "中转", other.baseURL(), keyB))
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("保存后配置不是中转写法: %q", cfg)
	}
	requireRelayOnlyCatalog(t, e)

	other.setIDs("deepseek-flash")
	st := c.result("refresh_models", map[string]any{"id": nil})
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("刷新后配置不是中转写法: %q", cfg)
	}
	requireRelayOnlyCatalog(t, e)
	if want := map[string]any{"mode": "relay", "target": "relay", "detect": "not_logged_in"}; !reflect.DeepEqual(st["access"], want) {
		t.Errorf("access = %v, want %v", st["access"], want)
	}
}

func TestServe_KeysNeverWrittenAnywhere(t *testing.T) {
	const keyC = "sk-example-2222"
	const keyBad = "sk-example-bad"
	e := newEnv(t, true)
	other := newFakeRelay(t, []string{"deepseek-flash"}, keyB, keyC)
	e.relay.keys[keyC] = true
	c := startServe(t, e.opts)
	c.result("init", twoRelayInit(e, other))
	c.result("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyC))
	c.result("save_relay", relayJSON("2", "Bee", other.baseURL(), keyC))
	c.errorOf("save_relay", relayJSON(nil, "坏", e.relay.baseURL(), keyBad))
	c.result("refresh_models", map[string]any{"id": nil})
	c.result("delete_relay", map[string]any{"id": "3"})
	c.result("shutdown", map[string]any{"restore": false})
	if err := c.waitExit(); err != nil {
		t.Fatal(err)
	}

	keys := []string{keyA, keyB, keyC, keyBad}
	for _, k := range keys {
		if strings.Contains(c.output(), k) {
			t.Errorf("输出里出现了 Key %q", k)
		}
	}
	for _, root := range []string{e.dataDir, filepath.Dir(e.configPath)} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, k := range keys {
				if bytes.Contains(b, []byte(k)) {
					t.Errorf("%s 里出现了 Key", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProtocolDoc_NoLegacyRelayWording(t *testing.T) {
	word := "中" + "台"
	allowed := map[string]bool{
		filepath.Join("..", "..", "internal", "catalog", "catalog_test.go"):    true,
		filepath.Join("..", "..", "internal", "proxy", "relay_errors_test.go"): true,
	}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || allowed[path] {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(word)) {
			t.Errorf("%s 里还有旧叫法", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServe_Init_EmptyRelays(t *testing.T) {
	e := newEnv(t, false)
	c := startServe(t, e.opts)
	res := c.result("init", initParamsFor([]any{}...))
	if rs, ok := res["relays"].([]any); !ok || len(rs) != 0 {
		t.Errorf("relays = %v, want []", res["relays"])
	}
	if v, ok := res["default_relay"]; !ok || v != nil {
		t.Errorf("default_relay = %v, want null", v)
	}
	if e.relay.hits() != 0 {
		t.Errorf("没有配置时不该联网, 实际 %d 次", e.relay.hits())
	}
	if id := requireNewID(t, c.result("save_relay", relayJSON(nil, "首个", e.relay.baseURL(), keyA))); id != "1" {
		t.Errorf("首个编号 = %s, want 1", id)
	}
}

func breakConfig(t *testing.T, e *env) {
	t.Helper()
	if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileBytes(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}

func relayCachePath(e *env, id string) string {
	return filepath.Join(e.dataDir, "relay-models-"+id+".json")
}

func catalogCountWithPrefix(t *testing.T, e *env, prefix string) int {
	t.Helper()
	n := 0
	for _, s := range catalogSlugs(catalogModels(t, e)) {
		if strings.HasPrefix(s.(string), prefix) {
			n++
		}
	}
	return n
}

func TestServe_ConfigWriteFailureChangesNothing(t *testing.T) {
	const keyC = "sk-example-2222"
	setup := func(t *testing.T) (*env, *fakeRelay, *client, map[string]any) {
		e := newEnv(t, true)
		other := newFakeRelay(t, []string{"deepseek-flash"}, keyB, keyC)
		c := startServe(t, e.opts)
		c.result("init", twoRelayInit(e, other))
		breakConfig(t, e)
		return e, other, c, c.result("status", nil)
	}

	t.Run("save_relay 新增", func(t *testing.T) {
		e, _, c, st := setup(t)
		cat := fileBytes(e.catalogPath())
		if code := c.errCode("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyA)); code != "config_invalid" {
			t.Fatalf("code = %q, want config_invalid", code)
		}
		if !bytes.Equal(cat, fileBytes(e.catalogPath())) {
			t.Error("合并目录应与调用前逐字节相同")
		}
		if exists(relayCachePath(e, "3")) {
			t.Error("不该留下新编号的缓存文件")
		}
		if got := c.result("status", nil); !reflect.DeepEqual(got, st) {
			t.Errorf("status 应与调用前相同:\n%v\n%v", got, st)
		}
	})

	t.Run("save_relay 编辑", func(t *testing.T) {
		e, other, c, st := setup(t)
		other.setIDs("deepseek-flash", "deepseek-v4-pro")
		cat, cache := fileBytes(e.catalogPath()), fileBytes(relayCachePath(e, "2"))
		if code := c.errCode("save_relay", relayJSON("2", "Bee", other.baseURL(), keyC)); code != "config_invalid" {
			t.Fatalf("code = %q, want config_invalid", code)
		}
		if !bytes.Equal(cat, fileBytes(e.catalogPath())) {
			t.Error("合并目录应与调用前逐字节相同")
		}
		if !bytes.Equal(cache, fileBytes(relayCachePath(e, "2"))) {
			t.Error("被编辑配置的缓存文件应与调用前相同")
		}
		if got := c.result("status", nil); !reflect.DeepEqual(got, st) {
			t.Errorf("status 应与调用前相同:\n%v\n%v", got, st)
		}
	})

	t.Run("delete_relay", func(t *testing.T) {
		e, _, c, st := setup(t)
		cat := fileBytes(e.catalogPath())
		c.errorOf("delete_relay", map[string]any{"id": "2"})
		if !bytes.Equal(cat, fileBytes(e.catalogPath())) {
			t.Error("合并目录应与调用前逐字节相同")
		}
		if got := c.result("status", nil); !reflect.DeepEqual(relayIDList(t, got), []string{"1", "2"}) || !reflect.DeepEqual(got, st) {
			t.Errorf("被删配置应仍在 status 里: %v", relayList(t, got))
		}
	})

	t.Run("refresh_models 目录与内存状态一致", func(t *testing.T) {
		e, other, c, _ := setup(t)
		other.setIDs("deepseek-flash", "deepseek-v4-pro")
		c.errorOf("refresh_models", map[string]any{"id": nil})
		rs := relayList(t, c.result("status", nil))
		if got, want := catalogCountWithPrefix(t, e, "relay2-"), int(rs[1]["model_count"].(float64)); got != want {
			t.Errorf("目录里配置 2 有 %d 个模型, status 里 model_count = %d", got, want)
		}
	})
}

func TestServe_SaveRelay_OnlyRequestsNewRelay(t *testing.T) {
	for _, forwarding := range []bool{true, false} {
		t.Run("forwarding="+strconv.FormatBool(forwarding), func(t *testing.T) {
			e := newEnv(t, forwarding)
			other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
			third := newFakeRelay(t, []string{"deepseek-flash"}, keyA)
			c := startServe(t, e.opts)
			c.result("init", twoRelayInit(e, other))
			h1, h2 := e.relay.hits(), other.hits()

			c.result("save_relay", relayJSON(nil, "新", third.baseURL(), keyA))
			if e.relay.hits() != h1 || other.hits() != h2 {
				t.Errorf("其他中转请求数增量 = %d/%d, want 0/0", e.relay.hits()-h1, other.hits()-h2)
			}
			if third.hits() == 0 {
				t.Error("新配置的中转应被请求")
			}
		})
	}
}

func TestServe_DeleteRelay_NoNetwork(t *testing.T) {
	e := newEnv(t, true)
	other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
	c := startServe(t, e.opts)
	c.result("init", twoRelayInit(e, other))
	h1, h2 := e.relay.hits(), other.hits()

	c.result("delete_relay", map[string]any{"id": "2"})
	if e.relay.hits() != h1 || other.hits() != h2 {
		t.Errorf("删除后请求数增量 = %d/%d, want 0/0", e.relay.hits()-h1, other.hits()-h2)
	}
}

func TestServe_Init_TooManyRelays(t *testing.T) {
	e := newEnv(t, false)
	var relays []any
	for i := 1; i <= 11; i++ {
		relays = append(relays, relayJSON(strconv.Itoa(i), "N"+strconv.Itoa(i), e.relay.baseURL(), keyA))
	}
	c := startServe(t, e.opts)
	if code := c.errCode("init", initParamsFor(relays...)); code != "bad_params" {
		t.Errorf("code = %q, want bad_params", code)
	}
}

func TestServe_SaveRelay_ModelsChangedWhenNeverFetched(t *testing.T) {
	e := newEnv(t, true)
	other := newFakeRelay(t, []string{"deepseek-flash"}, keyB)
	other.setStatus(500)
	c := startServe(t, e.opts)
	rs := relayList(t, c.result("init", twoRelayInit(e, other)))
	if rs[1]["model_count"] != float64(0) || rs[1]["error"] == nil {
		t.Fatalf("配置 2 应获取失败且没有模型: %v", rs[1])
	}

	other.setStatus(0)
	res := c.result("save_relay", relayJSON("2", "Bee", other.baseURL(), keyB))
	if res["models_changed"] != true || res["model_count"] != float64(1) {
		t.Errorf("res = %v, want models_changed true 且 model_count 1", res)
	}
}

func TestServe_SaveRelay_UnreadableCacheNotDeletedOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受文件权限限制")
	}
	const keyC = "sk-example-2222"
	e := newEnv(t, true)
	other := newFakeRelay(t, []string{"deepseek-flash"}, keyB, keyC)
	c := startServe(t, e.opts)
	c.result("init", twoRelayInit(e, other))
	cache := relayCachePath(e, "2")
	type cacheFile struct {
		FetchedAt string           `json:"fetched_at"`
		Models    []map[string]any `json:"models"`
	}
	var want cacheFile
	if err := json.Unmarshal(fileBytes(cache), &want); err != nil || len(want.Models) == 0 {
		t.Fatalf("初始化后应有配置 2 的合法缓存文件: %v", err)
	}
	breakConfig(t, e)
	if err := os.Chmod(cache, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0o644) })

	c.errorOf("save_relay", relayJSON("2", "Bee", other.baseURL(), keyC))

	if !exists(cache) {
		t.Fatal("失败后缓存文件不该被删除")
	}
	if err := os.Chmod(cache, 0o644); err != nil {
		t.Fatal(err)
	}
	var got cacheFile
	if err := json.Unmarshal(fileBytes(cache), &got); err != nil || got.FetchedAt == "" || !reflect.DeepEqual(got.Models, want.Models) {
		t.Errorf("缓存应是合法 JSON 且 models 不变: %+v (%v), 原 %+v", got, err, want)
	}
}

func TestServe_EOFStillProcessesQueuedRequests(t *testing.T) {
	e := newEnv(t, true)
	var in strings.Builder
	for i, req := range []map[string]any{
		{"id": 1, "method": "init", "params": initParams(e)},
		{"id": 2, "method": "status"},
		{"id": 3, "method": "shutdown", "params": map[string]any{"restore": true}},
	} {
		line, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("请求 %d: %v", i, err)
		}
		in.Write(line)
		in.WriteByte('\n')
	}
	var out bytes.Buffer

	if err := control.Serve(context.Background(), e.opts, strings.NewReader(in.String()), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	answered := map[int]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		var m struct {
			ID    *int            `json:"id"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(line), &m) == nil && m.ID != nil && m.Error == nil {
			answered[*m.ID] = true
		}
	}
	for _, id := range []int{1, 2, 3} {
		if !answered[id] {
			t.Errorf("id=%d 的请求没有得到成功响应，输出：%s", id, out.String())
		}
	}
	if got := e.config(t); got != originalConfig {
		t.Errorf("shutdown restore:true 应还原配置，实际 %q", got)
	}
}

func TestServe_EOFEndsInFlightSlowRequestWithinGrace(t *testing.T) {
	e := newEnv(t, false)
	hang := newHangingRelay(t)
	var in strings.Builder
	for _, req := range []map[string]any{
		{"id": 1, "method": "init", "params": hangingInitParams(hang)},
		{"id": 2, "method": "refresh_models"},
	} {
		line, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		in.Write(line)
		in.WriteByte('\n')
	}
	var out bytes.Buffer

	start := time.Now()
	_ = control.Serve(context.Background(), e.opts, strings.NewReader(in.String()), &out)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Serve 在 EOF 后 %v 才结束, 应在约 2 秒宽限期加余量内结束", elapsed)
	}
}

func TestServe_EOFAfterStatusCancelsRestartAndRunsShutdown(t *testing.T) {
	addr, release := holdPort(t)
	e := newEnv(t, true, func(e *env) { e.listenAddr = addr })
	hang := newHangingRelay(t)
	c := startServe(t, e.opts)
	c.result("init", hangingInitParams(hang))
	release()

	start := time.Now()
	if _, err := c.in.Write([]byte(`{"id":100,"method":"status"}` + "\n" + `{"id":101,"method":"shutdown","params":{"restore":false}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.in.Close()
	_ = c.waitExit()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("EOF 后 %v 才处理完, status 的重启应被宽限期取消", elapsed)
	}
	out := c.output()
	if !strings.Contains(out, `"id":101`) {
		t.Errorf("排队的 shutdown 应得到响应, 输出：%s", out)
	}
}

func TestServe_SaveRelay_NonSuccessStatusCarriesStatusNumber(t *testing.T) {
	tests := []struct {
		status   int
		wantCode string
	}{
		{500, "relay_5xx"},
		{502, "relay_5xx"},
		{503, "relay_5xx"},
		{400, "relay_4xx"},
		{418, "relay_4xx"},
		{301, "relay_bad_response"},
		{302, "relay_bad_response"},
		{304, "relay_bad_response"},
		{201, "relay_bad_response"},
		{204, "relay_bad_response"},
		{206, "relay_bad_response"},
		{200, "relay_bad_response"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			e := newEnv(t, false)
			c := startServe(t, e.opts)
			before := relayList(t, c.result("init", initParams(e)))
			e.relay.setStatus(tt.status)

			errObj := c.errorOf("save_relay", relayJSON(nil, "新", e.relay.baseURL(), keyB))

			code, _ := errObj["code"].(string)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if keys, _ := errObj["keys"].([]any); !reflect.DeepEqual(keys, []any{strconv.Itoa(tt.status)}) {
				t.Errorf("keys = %v, want [%d]", errObj["keys"], tt.status)
			}
			requireRelaysUnchanged(t, c, before)
		})
	}
}
