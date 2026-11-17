package control_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/pelletier/go-toml/v2"

	"modelbridge/internal/codexauth"
	"modelbridge/internal/control"
)

const (
	officialJSON     = `{"models":[{"slug":"gpt-a","priority":3,"display_name":"A","base_instructions":"x"},{"slug":"gpt-b","priority":7,"display_name":"B"}]}`
	keyA             = "sk-example-0000"
	keyB             = "sk-example-1111"
	originalConfig   = "sandbox = \"read-only\"\nopenai_base_url = \"https://old.example.com/v1\"\n"
	proxyURLInConfig = "http://127.0.0.1:47311/v1"
)

func ptr[T any](v T) *T { return &v }

func trustCert(t *testing.T, srv *httptest.Server) {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport)
	old := tr.TLSClientConfig
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	t.Cleanup(func() {
		tr.TLSClientConfig = old
		tr.CloseIdleConnections()
	})
}

type fakeRelay struct {
	*httptest.Server
	mu        sync.Mutex
	ids       []string
	keys      map[string]bool
	status    int
	modelHits int
	forwarded []string
	bodies    []string
}

func newFakeRelay(t *testing.T, ids []string, keys ...string) *fakeRelay {
	t.Helper()
	r := &fakeRelay{ids: ids, keys: map[string]bool{}}
	for _, k := range keys {
		r.keys[k] = true
	}
	r.Server = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.Close)
	trustCert(t, r.Server)
	return r
}

func (r *fakeRelay) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	auth := req.Header.Get("Authorization")
	if req.URL.Path != "/v1/models" {
		r.forwarded = append(r.forwarded, auth)
		body, _ := io.ReadAll(req.Body)
		r.bodies = append(r.bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
		return
	}
	r.modelHits++
	if r.status != 0 {
		w.WriteHeader(r.status)
		return
	}
	if !r.keys[strings.TrimPrefix(auth, "Bearer ")] {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	data := make([]map[string]string, 0, len(r.ids))
	for _, id := range r.ids {
		data = append(data, map[string]string{"id": id})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (r *fakeRelay) baseURL() string { return r.URL + "/v1" }

func (r *fakeRelay) setIDs(ids ...string) {
	r.mu.Lock()
	r.ids = ids
	r.mu.Unlock()
}

func (r *fakeRelay) setStatus(code int) {
	r.mu.Lock()
	r.status = code
	r.mu.Unlock()
}

func (r *fakeRelay) hits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.modelHits
}

func (r *fakeRelay) forwardedBodies() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.bodies)
}

func (r *fakeRelay) forwardedAuths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.forwarded)
}

func writeFakeCodex(t *testing.T, stdout string, exit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(out, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\ncat '" + out + "'\nexit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func occupiedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

type env struct {
	dataDir    string
	configPath string
	codexPath  string
	listenAddr string
	relay      *fakeRelay
	detect     *fakeDetect
	opts       control.Options
	svc        *control.Service
	shutDown   bool

	mu      sync.Mutex
	updates []control.Status
}

func newEnv(t *testing.T, forwarding bool, mods ...func(*env)) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{
		dataDir:    filepath.Join(root, "data"),
		configPath: filepath.Join(root, "codex", "config.toml"),
		codexPath:  writeFakeCodex(t, officialJSON, 0),
		listenAddr: freeAddr(t),
		relay:      newFakeRelay(t, []string{"deepseek-flash"}, keyA),
	}
	for _, d := range []string{e.dataDir, filepath.Dir(e.configPath)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(e.configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if forwarding {
		if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range mods {
		m(e)
	}
	e.opts = control.Options{
		DataDir:            e.dataDir,
		Version:            "1.2.3",
		ListenAddr:         e.listenAddr,
		OfficialAPIURL:     "https://api.example.com/v1",
		OfficialChatGPTURL: "https://chatgpt.example.com/backend-api/codex",
		CodexPath:          e.codexPath,
		ConfigPath:         e.configPath,
		OnStatus: func(s control.Status) {
			e.mu.Lock()
			e.updates = append(e.updates, s)
			e.mu.Unlock()
		},
	}
	if e.detect != nil {
		e.opts.DetectLogin = e.detect.fn
	}
	e.svc = control.NewService(e.opts)
	t.Cleanup(func() {
		if !e.shutDown {
			_ = e.svc.Shutdown(context.Background(), false)
		}
	})
	return e
}

func (e *env) initRelay(t *testing.T) control.Status {
	t.Helper()
	st, err := e.svc.Init(context.Background(), initParamsOf(t, initParams(e)))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return st
}

func (e *env) config(t *testing.T) string {
	t.Helper()
	return readFile(t, e.configPath)
}

func (e *env) handEditBaseURL(t *testing.T) string {
	t.Helper()
	text := e.config(t)
	if !strings.Contains(text, proxyURLInConfig) {
		t.Fatalf("配置里没有接入项: %q", text)
	}
	edited := strings.Replace(text, proxyURLInConfig, "http://proxy.example.org/v1", 1)
	if err := os.WriteFile(e.configPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return edited
}

func (e *env) catalogPath() string { return filepath.Join(e.dataDir, "model-bridge-catalog.json") }

func (e *env) backupPath() string { return e.configPath + ".before-model-bridge" }

func (e *env) settingsForwarding(t *testing.T) bool {
	t.Helper()
	s, err := control.LoadSettings(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return s.Forwarding
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func requireCode(t *testing.T, err error, code string) *control.Error {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) {
		t.Fatalf("期望 *control.Error(%s), 得到 %v", code, err)
	}
	if ce.Code != code {
		t.Fatalf("错误码 = %q, want %q", ce.Code, code)
	}
	return ce
}

func proxyReachable(addr string) bool {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func postViaProxy(t *testing.T, addr, model string) int {
	t.Helper()
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader([]byte(`{"model":"`+model+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func relayJSON(id any, name, baseURL, key string) map[string]any {
	return map[string]any{"id": id, "name": name, "base_url": baseURL, "api_key": key}
}

func initParamsOf(t *testing.T, m map[string]any) control.InitParams {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var p control.InitParams
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

type relayView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ModelCount int     `json:"model_count"`
	FetchedAt  *string `json:"fetched_at"`
	Stale      bool    `json:"stale"`
	Error      *string `json:"error"`
}

func relaysOf(t *testing.T, st control.Status) []relayView {
	t.Helper()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Relays []relayView `json:"relays"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v.Relays
}

func TestInit(t *testing.T) {
	t.Run("转发开着时启动转发生成目录并写入接入项", func(t *testing.T) {
		e := newEnv(t, true)
		st := e.initRelay(t)
		if st.Version != "1.2.3" || !st.Forwarding {
			t.Errorf("Version/Forwarding = %q/%v", st.Version, st.Forwarding)
		}
		if !st.Proxy.Running || st.Proxy.Error != nil {
			t.Errorf("Proxy = %+v", st.Proxy)
		}
		if !st.ChatGPT.Found || st.ChatGPT.Error != nil {
			t.Errorf("ChatGPT = %+v", st.ChatGPT)
		}
		rs := relaysOf(t, st)
		if len(rs) != 1 || rs[0].ID != "1" || rs[0].Name != "中转" || rs[0].ModelCount != 1 || rs[0].FetchedAt == nil || rs[0].Stale || rs[0].Error != nil {
			t.Errorf("Relays = %+v", rs)
		}
		if st.ConfigPath != e.configPath {
			t.Errorf("ConfigPath = %q, want %q", st.ConfigPath, e.configPath)
		}
		if !proxyReachable(e.listenAddr) {
			t.Error("转发服务没有在监听")
		}
		cfg := e.config(t)
		if !strings.Contains(cfg, proxyURLInConfig) || !strings.Contains(cfg, e.catalogPath()) {
			t.Errorf("接入项没写入: %q", cfg)
		}
		if !strings.Contains(readFile(t, e.catalogPath()), "deepseek-flash") {
			t.Error("合并目录里没有中转模型")
		}
	})

	t.Run("转发关着时只检测 ChatGPT 不拉中转", func(t *testing.T) {
		e := newEnv(t, false)
		st := e.initRelay(t)
		if st.Forwarding || st.Proxy.Running {
			t.Errorf("转发应关闭: %+v", st)
		}
		if !st.ChatGPT.Found {
			t.Errorf("ChatGPT = %+v", st.ChatGPT)
		}
		if e.relay.hits() != 0 {
			t.Errorf("转发关着不该拉中转, 实际 %d 次", e.relay.hits())
		}
		if e.config(t) != originalConfig || exists(e.catalogPath()) {
			t.Error("转发关着不该改配置或生成目录")
		}
	})

	t.Run("转发关着且找不到 codex", func(t *testing.T) {
		e := newEnv(t, false, func(e *env) { e.codexPath = filepath.Join(t.TempDir(), "no-such-codex") })
		st := e.initRelay(t)
		if st.ChatGPT.Found || st.ChatGPT.Error == nil || *st.ChatGPT.Error != "chatgpt_not_found" {
			t.Errorf("ChatGPT = %+v", st.ChatGPT)
		}
	})

	t.Run("转发开着但端口被占", func(t *testing.T) {
		e := newEnv(t, true, func(e *env) { e.listenAddr = occupiedAddr(t) })
		st := e.initRelay(t)
		if st.Proxy.Running || st.Proxy.Error == nil || *st.Proxy.Error != "proxy_port_in_use" {
			t.Errorf("Proxy = %+v", st.Proxy)
		}
	})
}

func TestSetForwardingTrue(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		e := newEnv(t, false)
		e.initRelay(t)
		st, err := e.svc.SetForwarding(context.Background(), true)
		if err != nil {
			t.Fatalf("SetForwarding: %v", err)
		}
		if !st.Forwarding || !st.Proxy.Running {
			t.Errorf("Status = %+v", st)
		}
		if !slices.Contains(st.TakenOver, "openai_base_url") {
			t.Errorf("TakenOver = %v, 应含原来就有值的 openai_base_url", st.TakenOver)
		}
		if !e.settingsForwarding(t) {
			t.Error("settings.json 没记 forwarding true")
		}
		if got := readFile(t, e.backupPath()); got != originalConfig {
			t.Errorf("首次备份 = %q", got)
		}
		if !exists(e.catalogPath()) {
			t.Error("合并目录没生成")
		}
		cfg := e.config(t)
		if !strings.Contains(cfg, proxyURLInConfig) || !strings.Contains(cfg, e.catalogPath()) {
			t.Errorf("接入项没写入: %q", cfg)
		}
	})

	tests := []struct {
		name     string
		mod      func(t *testing.T) func(*env)
		wantCode string
	}{
		{"找不到 codex", func(t *testing.T) func(*env) {
			return func(e *env) { e.codexPath = filepath.Join(t.TempDir(), "no-such-codex") }
		}, "chatgpt_not_found"},
		{"官方清单失败", func(t *testing.T) func(*env) {
			return func(e *env) { e.codexPath = writeFakeCodex(t, officialJSON, 1) }
		}, "chatgpt_unsupported"},
		{"端口被占", func(t *testing.T) func(*env) {
			return func(e *env) { e.listenAddr = occupiedAddr(t) }
		}, "proxy_port_in_use"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"时不写配置", func(t *testing.T) {
			e := newEnv(t, false, tt.mod(t))
			e.initRelay(t)
			_, err := e.svc.SetForwarding(context.Background(), true)
			requireCode(t, err, tt.wantCode)
			if e.config(t) != originalConfig {
				t.Errorf("配置被改动: %q", e.config(t))
			}
			if exists(e.backupPath()) || exists(e.catalogPath()) {
				t.Error("不该产生备份或合并目录")
			}
			if e.settingsForwarding(t) {
				t.Error("settings.json 不该记 forwarding true")
			}
			if cur := e.svc.Status(); cur.Forwarding || cur.Proxy.Running {
				t.Errorf("Status = %+v", cur)
			}
		})
	}
}

func TestSetForwardingFalse(t *testing.T) {
	ctx := context.Background()

	t.Run("无手改项时完整还原并停止转发", func(t *testing.T) {
		e := newEnv(t, true)
		e.initRelay(t)
		addr := e.listenAddr
		st, err := e.svc.SetForwarding(ctx, false)
		if err != nil {
			t.Fatalf("SetForwarding: %v", err)
		}
		if st.Forwarding || st.Proxy.Running || len(st.RestoreBlocked) != 0 {
			t.Errorf("Status = %+v", st)
		}
		if e.config(t) != originalConfig {
			t.Errorf("配置没还原: %q", e.config(t))
		}
		if e.settingsForwarding(t) {
			t.Error("settings.json 应记 forwarding false")
		}
		if proxyReachable(addr) {
			t.Error("转发服务应已停止")
		}
	})

	t.Run("有手改项时返回 restore_blocked", func(t *testing.T) {
		e := newEnv(t, true)
		e.initRelay(t)
		addr := e.listenAddr
		e.handEditBaseURL(t)

		_, err := e.svc.SetForwarding(ctx, false)
		ce := requireCode(t, err, "restore_blocked")
		if !reflect.DeepEqual(ce.Keys, []string{"openai_base_url"}) {
			t.Errorf("Keys = %v", ce.Keys)
		}
		st := e.svc.Status()
		if st.Forwarding || st.Proxy.Running {
			t.Errorf("转发应算关闭且服务停止: %+v", st)
		}
		if !reflect.DeepEqual(st.RestoreBlocked, []string{"openai_base_url"}) {
			t.Errorf("RestoreBlocked = %v", st.RestoreBlocked)
		}
		if e.settingsForwarding(t) {
			t.Error("settings.json 应记 forwarding false")
		}
		if proxyReachable(addr) {
			t.Error("转发服务应已停止")
		}
	})
}

func TestCheckUpdate(t *testing.T) {
	for _, manual := range []bool{true, false} {
		t.Run("manual="+strconv.FormatBool(manual), func(t *testing.T) {
			e := newEnv(t, false)
			res, err := e.svc.CheckUpdate(context.Background(), manual)
			if err != nil {
				t.Fatalf("CheckUpdate: %v", err)
			}
			if res.IsNewer || res.LatestVersion != nil || res.URL != nil {
				t.Errorf("res = %+v", res)
			}
		})
	}
}

func TestPrepareUninstall(t *testing.T) {
	ctx := context.Background()
	toolFiles := []string{"model-bridge-catalog.json", "access-state.json", "relay-models-1.json", "relay-models-2.json", "relay-models.json", "relay-reasoning-ids.json", "settings.json", "model-bridge.log"}

	plant := func(t *testing.T, e *env) {
		t.Helper()
		for _, name := range []string{"relay-reasoning-ids.json", "model-bridge.log", "relay-models-2.json", "relay-models.json"} {
			if err := os.WriteFile(filepath.Join(e.dataDir, name), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("有手改项时全有或全无并保留数据目录文件", func(t *testing.T) {
		e := newEnv(t, true)
		e.initRelay(t)
		plant(t, e)
		edited := e.handEditBaseURL(t)

		err := e.svc.PrepareUninstall(ctx)
		ce := requireCode(t, err, "restore_blocked")
		if !reflect.DeepEqual(ce.Keys, []string{"openai_base_url"}) {
			t.Errorf("Keys = %v", ce.Keys)
		}
		if e.config(t) != edited {
			t.Errorf("全有或全无: 配置不该有任何改动: %q", e.config(t))
		}
		for _, name := range toolFiles {
			if !exists(filepath.Join(e.dataDir, name)) {
				t.Errorf("%s 应保留", name)
			}
		}
	})

	t.Run("无手改项时还原并删除本工具文件但留备份", func(t *testing.T) {
		e := newEnv(t, true)
		e.initRelay(t)
		plant(t, e)

		if err := e.svc.PrepareUninstall(ctx); err != nil {
			t.Fatalf("PrepareUninstall: %v", err)
		}
		if e.config(t) != originalConfig {
			t.Errorf("配置没还原: %q", e.config(t))
		}
		for _, name := range toolFiles {
			if exists(filepath.Join(e.dataDir, name)) {
				t.Errorf("%s 应被删除", name)
			}
		}
		if got := readFile(t, e.backupPath()); got != originalConfig {
			t.Errorf("备份应保留, 内容 = %q", got)
		}
	})

	t.Run("从未开启过转发时也能成功", func(t *testing.T) {
		e := newEnv(t, false)
		if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.PrepareUninstall(ctx); err != nil {
			t.Fatalf("PrepareUninstall: %v", err)
		}
		if exists(filepath.Join(e.dataDir, "settings.json")) || e.config(t) != originalConfig {
			t.Error("settings.json 应删除且配置不变")
		}
	})
}

func TestShutdown(t *testing.T) {
	for _, restore := range []bool{true, false} {
		t.Run("restore="+strconv.FormatBool(restore), func(t *testing.T) {
			e := newEnv(t, true)
			e.initRelay(t)
			addr := e.listenAddr
			e.shutDown = true

			if err := e.svc.Shutdown(context.Background(), restore); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
			if proxyReachable(addr) {
				t.Error("转发服务应已停止")
			}
			restored := e.config(t) == originalConfig
			if restored != restore {
				t.Errorf("配置还原 = %v, want %v: %q", restored, restore, e.config(t))
			}
		})
	}
}

func TestStatusNotification(t *testing.T) {
	e := newEnv(t, false)
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if n := len(e.updates); n == 0 || !e.updates[n-1].Forwarding || !e.updates[n-1].Proxy.Running {
		t.Errorf("状态变化应回调 OnStatus, 实际 %+v", e.updates)
	}
}

func TestInit_ActivateFailureRecordedInStatus(t *testing.T) {
	e := newEnv(t, true, func(e *env) {
		if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	st := e.initRelay(t)
	if st.Proxy.Running || st.Proxy.Error == nil || *st.Proxy.Error != "config_invalid" {
		t.Errorf("Proxy = %+v", st.Proxy)
	}
}

func TestSetForwardingTrue_ConfigInvalidCarriesKeys(t *testing.T) {
	e := newEnv(t, false, func(e *env) {
		if err := os.WriteFile(e.configPath, []byte("openai_base_url = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	e.initRelay(t)
	_, err := e.svc.SetForwarding(context.Background(), true)
	ce := requireCode(t, err, "config_invalid")
	if !reflect.DeepEqual(ce.Keys, []string{"openai_base_url"}) {
		t.Errorf("Keys = %v", ce.Keys)
	}
}

func TestSetForwardingFalse_NonBlockedErrorKeepsState(t *testing.T) {
	e := newEnv(t, true)
	e.initRelay(t)
	if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := e.svc.SetForwarding(context.Background(), false)
	requireCode(t, err, "config_invalid")
	st := e.svc.Status()
	if !st.Forwarding || !st.Proxy.Running {
		t.Errorf("Status = %+v, 转发应保持开着", st)
	}
	if !e.settingsForwarding(t) {
		t.Error("settings.json 不该改")
	}
	if !proxyReachable(e.listenAddr) {
		t.Error("转发服务应继续运行")
	}
}

func TestInit_RecoversTakenOverAfterRestart(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	first := e.initRelay(t)
	if !slices.Contains(first.TakenOver, "openai_base_url") {
		t.Fatalf("TakenOver = %v", first.TakenOver)
	}
	e.shutDown = true
	if err := e.svc.Shutdown(ctx, false); err != nil {
		t.Fatal(err)
	}

	svc := control.NewService(e.opts)
	t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })
	st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(st.TakenOver, "openai_base_url") {
		t.Errorf("重启后 TakenOver = %v", st.TakenOver)
	}
}

func TestInit_RecoversRestoreBlockedAfterRestart(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	e.initRelay(t)
	e.handEditBaseURL(t)
	_, err := e.svc.SetForwarding(ctx, false)
	requireCode(t, err, "restore_blocked")

	svc := control.NewService(e.opts)
	st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.RestoreBlocked, []string{"openai_base_url"}) {
		t.Errorf("重启后 RestoreBlocked = %v", st.RestoreBlocked)
	}
}

func TestSetForwardingTrue_SameNameModelUsesPrefixedSlugAndOriginalNameUpstream(t *testing.T) {
	e := newEnv(t, true)
	e.relay.setIDs("gpt-a", "deepseek-flash", "unknown-model-x")
	e.initRelay(t)

	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFile(t, e.catalogPath())), &file); err != nil {
		t.Fatal(err)
	}
	var slugs []any
	display := map[any]any{}
	for _, m := range file.Models {
		slugs = append(slugs, m["slug"])
		display[m["slug"]] = m["display_name"]
	}
	if want := []any{"gpt-a", "gpt-b", "relay-deepseek-flash", "relay-gpt-a"}; !reflect.DeepEqual(slugs, want) {
		t.Errorf("slugs = %v, want %v（两边都没有的 unknown-model-x 不出现）", slugs, want)
	}
	if display["relay-gpt-a"] != "中转 · gpt-a" {
		t.Errorf("display_name = %v", display["relay-gpt-a"])
	}

	if code := postViaProxy(t, e.listenAddr, "relay-gpt-a"); code != http.StatusOK {
		t.Fatalf("转发状态码 = %d", code)
	}
	bodies := e.relay.forwardedBodies()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"model":"gpt-a"`) {
		t.Errorf("中转收到的请求体 = %v，model 应改回原名 gpt-a", bodies)
	}
}

const threeOfficialJSON = `{"models":[` +
	`{"slug":"gpt-a","priority":3,"display_name":"A"},` +
	`{"slug":"gpt-b","priority":5,"display_name":"B","context_window":123456,"max_context_window":123456,"input_modalities":["text","image"]},` +
	`{"slug":"gpt-c","priority":8,"display_name":"C"}]}`

func writeModelsCache(t *testing.T, e *env, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.configPath), "models_cache.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func catalogModels(t *testing.T, e *env) []map[string]any {
	t.Helper()
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFile(t, e.catalogPath())), &file); err != nil {
		t.Fatal(err)
	}
	return file.Models
}

func catalogSlugs(models []map[string]any) []any {
	var out []any
	for _, m := range models {
		out = append(out, m["slug"])
	}
	return out
}

func TestCatalog_OfficialLimitedToAccountModelsCache(t *testing.T) {
	e := newEnv(t, true, func(e *env) { e.codexPath = writeFakeCodex(t, threeOfficialJSON, 0) })
	writeModelsCache(t, e, `{"fetched_at":"2026-01-01T00:00:00Z","etag":"e","client_version":"0.0.0","identity":"placeholder","models":[{"slug":"gpt-c","visibility":"list"},{"slug":"ghost-not-in-full-set"},{"slug":"gpt-a","visibility":"list"}]}`)
	e.relay.setIDs("gpt-b", "deepseek-flash")
	st := e.initRelay(t)

	models := catalogModels(t, e)
	if want := []any{"gpt-a", "gpt-c", "relay-deepseek-flash", "relay-gpt-b"}; !reflect.DeepEqual(catalogSlugs(models), want) {
		t.Fatalf("slugs = %v, want %v（官方只留名单里有的，顺序同全集；账号没有的 gpt-b 仍有中转同名条目）", catalogSlugs(models), want)
	}
	if rs := relaysOf(t, st); len(rs) != 1 || rs[0].ModelCount != 2 {
		t.Errorf("Relays = %+v, want 模型数 2", rs)
	}
	rb := models[3]
	if rb["display_name"] != "中转 · gpt-b" || rb["context_window"] != float64(123456) || !reflect.DeepEqual(rb["input_modalities"], []any{"text", "image"}) {
		t.Errorf("relay-gpt-b 应照搬全集里的 gpt-b 参数: %v", rb)
	}
	if rb["priority"] == nil || models[2]["priority"] == nil {
		t.Fatal("中转条目缺 priority")
	}
	p0, p1 := models[2]["priority"].(float64), rb["priority"].(float64)
	if p0 <= models[1]["priority"].(float64) || p1 != p0+1 {
		t.Errorf("中转 priority = %v, %v，应在官方之后连续递增", p0, p1)
	}
}

func TestCatalog_ModelsCacheUnusable_AllOfficialKept(t *testing.T) {
	tests := []struct {
		name  string
		cache *string
	}{
		{"文件不存在", nil},
		{"不是合法 JSON", ptr("not json")},
		{"models 为空", ptr(`{"models":[]}`)},
		{"没有 models 字段", ptr(`{"etag":"e"}`)},
		{"名单里的模型全集里一个都没有", ptr(`{"models":[{"slug":"ghost-a"},{"slug":"ghost-b"}]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, true, func(e *env) { e.codexPath = writeFakeCodex(t, threeOfficialJSON, 0) })
			if tt.cache != nil {
				writeModelsCache(t, e, *tt.cache)
			}
			e.relay.setIDs("deepseek-flash")
			e.initRelay(t)
			want := []any{"gpt-a", "gpt-b", "gpt-c", "relay-deepseek-flash"}
			if got := catalogSlugs(catalogModels(t, e)); !reflect.DeepEqual(got, want) {
				t.Errorf("slugs = %v, want %v", got, want)
			}
		})
	}
}

func postBodyViaProxy(t *testing.T, addr, body string, zstdBody bool) {
	t.Helper()
	var payload []byte = []byte(body)
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if zstdBody {
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatal(err)
		}
		payload = enc.EncodeAll(payload, nil)
		enc.Close()
		req.Header.Set("Content-Encoding", "zstd")
	}
	req.Body = io.NopCloser(bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func TestForwarding_UltraEffortChangedBackToMaxOnlyForParamsTableModels(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		effort     string
		zstd       bool
		wantEffort string
	}{
		{"参数表模型 ultra 改回 max", "relay-deepseek-flash", "ultra", false, "max"},
		{"参数表模型 ultra 改回 max（zstd）", "relay-deepseek-flash", "ultra", true, "max"},
		{"参数表模型 high 原样", "relay-deepseek-flash", "high", false, "high"},
		{"官方同名中转模型 ultra 原样", "relay-gpt-a", "ultra", false, "ultra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.relay.setIDs("deepseek-flash", "gpt-a")
			e.initRelay(t)
			postBodyViaProxy(t, e.listenAddr, `{"model":"`+tt.model+`","reasoning":{"effort":"`+tt.effort+`"}}`, tt.zstd)
			bodies := e.relay.forwardedBodies()
			if len(bodies) != 1 {
				t.Fatalf("中转收到 %d 次请求", len(bodies))
			}
			var sent struct {
				Reasoning struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(bodies[0]), &sent); err != nil {
				t.Fatalf("请求体不是 JSON: %q", bodies[0])
			}
			if sent.Reasoning.Effort != tt.wantEffort {
				t.Errorf("中转收到的 effort = %q, want %q（%s）", sent.Reasoning.Effort, tt.wantEffort, bodies[0])
			}
		})
	}
}

func TestForwarding_ParamsTableModel_NoReasoningFieldsAdded(t *testing.T) {
	tests := []struct {
		name string
		body string
		want map[string]any
	}{
		{"不带 reasoning", `{"model":"relay-deepseek-flash","input":"hi"}`, map[string]any{"model": "deepseek-flash", "input": "hi"}},
		{"reasoning 为空对象", `{"model":"relay-deepseek-flash","reasoning":{}}`, map[string]any{"model": "deepseek-flash", "reasoning": map[string]any{}}},
		{"reasoning 只有 summary", `{"model":"relay-deepseek-flash","reasoning":{"summary":"auto"}}`, map[string]any{"model": "deepseek-flash", "reasoning": map[string]any{"summary": "auto"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.relay.setIDs("deepseek-flash")
			e.initRelay(t)
			postBodyViaProxy(t, e.listenAddr, tt.body, false)
			bodies := e.relay.forwardedBodies()
			if len(bodies) != 1 {
				t.Fatalf("中转收到 %d 次请求", len(bodies))
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
				t.Fatalf("请求体不是 JSON: %q", bodies[0])
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("中转收到的请求体 = %s，期望字段集合 %v（只有 model 改回原名）", bodies[0], tt.want)
			}
		})
	}
}

type fakeDetect struct {
	mu     sync.Mutex
	result codexauth.Result
	calls  int
	paths  []string
}

func (d *fakeDetect) fn(_ context.Context, codexPath string) codexauth.Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.paths = append(d.paths, codexPath)
	return d.result
}

func (d *fakeDetect) set(r codexauth.Result) {
	d.mu.Lock()
	d.result = r
	d.mu.Unlock()
}

func (d *fakeDetect) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func withDetect(r codexauth.Result) func(*env) {
	return func(e *env) { e.detect = &fakeDetect{result: r} }
}

func withSettings(content string) func(*env) {
	return func(e *env) {
		if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(content), 0o644); err != nil {
			panic(err)
		}
	}
}

func accessOf(t *testing.T, st control.Status) map[string]any {
	t.Helper()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	acc, ok := m["access"].(map[string]any)
	if !ok {
		t.Fatalf("状态里没有 access 对象: %s", raw)
	}
	return acc
}

func requireAccess(t *testing.T, st control.Status, mode, target, detect any) {
	t.Helper()
	want := map[string]any{"mode": mode, "target": target, "detect": detect}
	if got := accessOf(t, st); !reflect.DeepEqual(got, want) {
		t.Errorf("access = %v, want %v", got, want)
	}
}

func settingsJSON(t *testing.T, e *env) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(e.dataDir, "settings.json"))), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func parseConfig(cfg string) map[string]any {
	var m map[string]any
	if err := toml.Unmarshal([]byte(cfg), &m); err != nil {
		return nil
	}
	return m
}

func isRelayConfig(cfg string) bool {
	m := parseConfig(cfg)
	providers, _ := m["model_providers"].(map[string]any)
	_, hasProvider := providers["modelbridge"].(map[string]any)
	return m != nil && m["model_provider"] == "modelbridge" && hasProvider &&
		m["openai_base_url"] == "https://old.example.com/v1"
}

func isBothConfig(cfg string) bool {
	m := parseConfig(cfg)
	return m != nil && m["openai_base_url"] == proxyURLInConfig && m["model_provider"] != "modelbridge"
}

func requireRelayOnlyCatalog(t *testing.T, e *env) {
	t.Helper()
	slugs := catalogSlugs(catalogModels(t, e))
	if len(slugs) == 0 {
		t.Fatal("中转方式的目录不该为空")
	}
	for _, s := range slugs {
		if !strings.HasPrefix(s.(string), "relay-") {
			t.Errorf("目录里出现非中转条目 %v, 全部: %v", s, slugs)
		}
	}
}

func postStatusBody(t *testing.T, addr, model string) (int, string) {
	t.Helper()
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"`+model+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func writeLoginFakeCodex(t *testing.T, loginStderr string, loginExit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(out, []byte(officialJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\nif [ \"$1\" = login ]; then\n  printf '%s\\n' '" + loginStderr + "' >&2\n  exit " + strconv.Itoa(loginExit) + "\nfi\ncat '" + out + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestAccess_OpenForwardingRelay(t *testing.T) {
	e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
	e.initRelay(t)
	st, err := e.svc.SetForwarding(context.Background(), true)
	if err != nil {
		t.Fatalf("SetForwarding: %v", err)
	}
	requireAccess(t, st, "relay", "relay", "not_logged_in")
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("配置不是中转写法: %q", cfg)
	}
	requireRelayOnlyCatalog(t, e)
	code, body := postStatusBody(t, e.listenAddr, "gpt-a")
	if code != http.StatusBadRequest {
		t.Errorf("官方模型请求状态码 = %d, want 400", code)
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || parsed.Error.Code != "official_unavailable" {
		t.Errorf("正文 = %q (%v), want error.code official_unavailable", body, err)
	}
	if got := e.detect.paths; len(got) != 1 || got[0] != e.codexPath {
		t.Errorf("检测收到的 codex 路径 = %v, want [%s]", got, e.codexPath)
	}
}

func TestAccess_OpenForwardingBoth(t *testing.T) {
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	st, err := e.svc.SetForwarding(context.Background(), true)
	if err != nil {
		t.Fatalf("SetForwarding: %v", err)
	}
	requireAccess(t, st, "both", "both", "logged_in")
	cfg := e.config(t)
	if !isBothConfig(cfg) || !strings.Contains(cfg, e.catalogPath()) {
		t.Errorf("配置不是官方加中转写法: %q", cfg)
	}
	slugs := catalogSlugs(catalogModels(t, e))
	if !slices.Contains(slugs, any("gpt-a")) || !slices.Contains(slugs, any("relay-deepseek-flash")) {
		t.Errorf("目录应含官方和中转条目: %v", slugs)
	}
	if code := postViaProxy(t, e.listenAddr, "relay-deepseek-flash"); code != http.StatusOK {
		t.Errorf("中转模型转发状态码 = %d", code)
	}
}

func TestAccess_FailedFallsBackToLastDetected(t *testing.T) {
	tests := []struct {
		name       string
		settings   string
		wantTarget string
		recorded   any
	}{
		{"上次成功是 both", `{"forwarding":false,"last_detected_mode":"both"}`, "both", "both"},
		{"上次成功是 relay", `{"forwarding":false,"last_detected_mode":"relay"}`, "relay", "relay"},
		{"从没成功过", `{"forwarding":false}`, "relay", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, false, withDetect(codexauth.Failed), withSettings(tt.settings))
			e.initRelay(t)
			st, err := e.svc.SetForwarding(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			requireAccess(t, st, tt.wantTarget, tt.wantTarget, "failed")
			if got := settingsJSON(t, e)["last_detected_mode"]; got != tt.recorded {
				t.Errorf("失败不该改写 last_detected_mode: %v, want %v", got, tt.recorded)
			}
		})
	}
}

func TestAccess_SuccessfulDetectIsRemembered(t *testing.T) {
	tests := []struct {
		name   string
		result codexauth.Result
		want   string
	}{
		{"已登录记 both", codexauth.LoggedIn, "both"},
		{"未登录记 relay", codexauth.NotLoggedIn, "relay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, false, withDetect(tt.result))
			e.initRelay(t)
			if _, err := e.svc.SetForwarding(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if got := settingsJSON(t, e)["last_detected_mode"]; got != tt.want {
				t.Errorf("last_detected_mode = %v, want %s", got, tt.want)
			}
		})
	}

	t.Run("重启后检测失败仍按记住的方式", func(t *testing.T) {
		e := newEnv(t, false, withDetect(codexauth.LoggedIn))
		e.initRelay(t)
		if _, err := e.svc.SetForwarding(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Shutdown(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		e.detect.set(codexauth.Failed)
		restarted := control.NewService(e.opts)
		t.Cleanup(func() { _ = restarted.Shutdown(context.Background(), false) })
		st, err := restarted.Init(context.Background(), initParamsOf(t, initParams(e)))
		if err != nil {
			t.Fatal(err)
		}
		requireAccess(t, st, "both", "both", "failed")
	})

	t.Run("同一服务里检测失败沿用上次成功", func(t *testing.T) {
		e := newEnv(t, false, withDetect(codexauth.LoggedIn))
		e.initRelay(t)
		if _, err := e.svc.SetForwarding(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		e.detect.set(codexauth.Failed)
		st, err := e.svc.DetectAccess(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		requireAccess(t, st, "both", "both", "failed")
	})
}

func TestAccess_InitRestoresRecordedModeThenDetects(t *testing.T) {
	tests := []struct {
		name     string
		settings string
	}{
		{"记录为 both", `{"forwarding":true,"access_mode":"both"}`},
		{"没有记录按 both", `{"forwarding":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t, true, withDetect(codexauth.NotLoggedIn), withSettings(tt.settings))
			st := e.initRelay(t)
			if cfg := e.config(t); !isBothConfig(cfg) {
				t.Errorf("Init 应先按 both 写法恢复: %q", cfg)
			}
			requireAccess(t, st, "both", "relay", "not_logged_in")

			st, err := e.svc.ApplyAccess(ctx)
			if err != nil {
				t.Fatalf("ApplyAccess: %v", err)
			}
			requireAccess(t, st, "relay", "relay", "not_logged_in")
			applied := e.config(t)
			if !isRelayConfig(applied) {
				t.Fatalf("切换后不是中转写法: %q", applied)
			}
			requireRelayOnlyCatalog(t, e)
			if got := settingsJSON(t, e)["access_mode"]; got != "relay" {
				t.Errorf("access_mode = %v, want relay", got)
			}

			if _, err := e.svc.SetForwarding(ctx, false); err != nil {
				t.Fatal(err)
			}
			if _, err := e.svc.SetForwarding(ctx, true); err != nil {
				t.Fatal(err)
			}
			if fresh := e.config(t); fresh != applied {
				t.Errorf("切换结果与关闭后重新打开不一致:\n%q\n%q", applied, fresh)
			}
		})
	}
}

func TestAccess_InitRestoresRecordedRelay(t *testing.T) {
	e := newEnv(t, true, withDetect(codexauth.LoggedIn), withSettings(`{"forwarding":true,"access_mode":"relay"}`))
	st := e.initRelay(t)
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("Init 应按记录的 relay 写法恢复: %q", cfg)
	}
	requireAccess(t, st, "relay", "both", "logged_in")
}

func TestAccess_ApplyAccessNoChange(t *testing.T) {
	ctx := context.Background()

	t.Run("方式相同", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			result codexauth.Result
		}{{"both", codexauth.LoggedIn}, {"relay", codexauth.NotLoggedIn}} {
			t.Run(tt.name, func(t *testing.T) {
				e := newEnv(t, false, withDetect(tt.result))
				e.initRelay(t)
				if _, err := e.svc.SetForwarding(ctx, true); err != nil {
					t.Fatal(err)
				}
				before := e.config(t)
				st, err := e.svc.ApplyAccess(ctx)
				if err != nil {
					t.Fatalf("ApplyAccess: %v", err)
				}
				if e.config(t) != before {
					t.Errorf("配置被改动: %q", e.config(t))
				}
				requireAccess(t, st, tt.name, tt.name, map[codexauth.Result]string{codexauth.LoggedIn: "logged_in", codexauth.NotLoggedIn: "not_logged_in"}[tt.result])
			})
		}
	})

	t.Run("转发关着", func(t *testing.T) {
		e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
		e.initRelay(t)
		if _, err := e.svc.DetectAccess(ctx); err != nil {
			t.Fatal(err)
		}
		st, err := e.svc.ApplyAccess(ctx)
		if err != nil {
			t.Fatalf("ApplyAccess: %v", err)
		}
		if e.config(t) != originalConfig {
			t.Errorf("配置被改动: %q", e.config(t))
		}
		if exists(e.catalogPath()) {
			t.Error("转发关着不该生成目录")
		}
		requireAccess(t, st, nil, "relay", "not_logged_in")
	})
}

func TestAccess_ApplyAccessKeepsHandEdit(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.handEditBaseURL(t)
	e.detect.set(codexauth.NotLoggedIn)
	if _, err := e.svc.DetectAccess(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.ApplyAccess(ctx)
	if err != nil {
		t.Fatalf("ApplyAccess: %v", err)
	}
	requireAccess(t, st, "relay", "relay", "not_logged_in")
	cfg := e.config(t)
	if !strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, "http://proxy.example.org/v1") {
		t.Errorf("手改的 openai_base_url 应保留: %q", cfg)
	}
	if !strings.Contains(cfg, `model_provider = "modelbridge"`) || !strings.Contains(cfg, "model_providers.modelbridge") {
		t.Errorf("其余项应是中转写法: %q", cfg)
	}
}

func TestAccess_DetectAccessOnlyUpdatesTargetAndDetect(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	before := e.config(t)
	e.detect.set(codexauth.NotLoggedIn)
	st, err := e.svc.DetectAccess(ctx)
	if err != nil {
		t.Fatalf("DetectAccess: %v", err)
	}
	requireAccess(t, st, "both", "relay", "not_logged_in")
	if e.config(t) != before {
		t.Errorf("配置被改动: %q", e.config(t))
	}
	if got := settingsJSON(t, e)["last_detected_mode"]; got != "relay" {
		t.Errorf("last_detected_mode = %v, want relay", got)
	}
	if e.detect.count() != 2 {
		t.Errorf("检测次数 = %d, want 2 (打开转发一次, DetectAccess 一次)", e.detect.count())
	}
}

func TestAccess_InitWithForwardingOffDoesNotDetect(t *testing.T) {
	e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
	st := e.initRelay(t)
	if e.detect.count() != 0 {
		t.Errorf("检测次数 = %d, want 0", e.detect.count())
	}
	requireAccess(t, st, nil, nil, nil)
}

func TestAccess_InitWithForwardingOnDetectsOnce(t *testing.T) {
	e := newEnv(t, true, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if e.detect.count() != 1 {
		t.Errorf("检测次数 = %d, want 1", e.detect.count())
	}
}

func TestAccess_CloseForwardingRestoresAndClearsMode(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result codexauth.Result
	}{{"both", codexauth.LoggedIn}, {"relay", codexauth.NotLoggedIn}} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t, false, withDetect(tt.result))
			e.initRelay(t)
			if _, err := e.svc.SetForwarding(ctx, true); err != nil {
				t.Fatal(err)
			}
			calls := e.detect.count()
			st, err := e.svc.SetForwarding(ctx, false)
			if err != nil {
				t.Fatalf("SetForwarding(false): %v", err)
			}
			if e.config(t) != originalConfig {
				t.Errorf("配置没还原: %q", e.config(t))
			}
			if got := accessOf(t, st)["mode"]; got != nil {
				t.Errorf("access.mode = %v, want null", got)
			}
			if e.detect.count() != calls {
				t.Errorf("关闭转发不该检测: %d -> %d", calls, e.detect.count())
			}
		})
	}
}

func TestAccess_DefaultDetectionUsesCodexLoginStatus(t *testing.T) {
	const marker = "SECRET-LOGIN-OUTPUT-XYZ"
	ctx := context.Background()

	t.Run("Not logged in 得到 relay", func(t *testing.T) {
		e := newEnv(t, false, func(e *env) { e.codexPath = writeLoginFakeCodex(t, "Not logged in", 1) })
		e.initRelay(t)
		st, err := e.svc.SetForwarding(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		requireAccess(t, st, "relay", "relay", "not_logged_in")
	})

	t.Run("检测命令的输出不进入状态日志和数据目录", func(t *testing.T) {
		e := newEnv(t, true, func(e *env) { e.codexPath = writeLoginFakeCodex(t, marker, 1) })
		e.initRelay(t)
		st, err := e.svc.DetectAccess(ctx)
		if err != nil {
			t.Fatal(err)
		}
		requireAccess(t, st, "both", "relay", "failed")
		raw, _ := json.Marshal(st)
		if bytes.Contains(raw, []byte(marker)) {
			t.Errorf("状态里出现检测输出: %s", raw)
		}
		e.mu.Lock()
		for _, u := range e.updates {
			if b, _ := json.Marshal(u); bytes.Contains(b, []byte(marker)) {
				t.Errorf("推送的状态里出现检测输出: %s", b)
			}
		}
		e.mu.Unlock()
		entries, _ := os.ReadDir(e.dataDir)
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			if strings.Contains(readFile(t, filepath.Join(e.dataDir, ent.Name())), marker) {
				t.Errorf("%s 里出现检测输出", ent.Name())
			}
		}
	})
}

func breakRelayWrite(t *testing.T, e *env) {
	t.Helper()
	if err := os.WriteFile(e.configPath, []byte("model_providers = \"oops\"\n"+e.config(t)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAccess_ApplyAccessFailureRestoresState(t *testing.T) {
	ctx := context.Background()
	var officialHits int
	var hitsMu sync.Mutex
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitsMu.Lock()
		officialHits++
		hitsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(official.Close)

	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.opts.OfficialAPIURL = official.URL + "/v1"
	e.svc = control.NewService(e.opts)
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.detect.set(codexauth.NotLoggedIn)
	if _, err := e.svc.DetectAccess(ctx); err != nil {
		t.Fatal(err)
	}
	breakRelayWrite(t, e)
	slugsBefore := catalogSlugs(catalogModels(t, e))

	if _, err := e.svc.ApplyAccess(ctx); err == nil {
		t.Fatal("relay 写法写入失败时 ApplyAccess 应返回错误")
	}
	st := e.svc.Status()
	if got := accessOf(t, st)["mode"]; got != "both" {
		t.Errorf("access.mode = %v, want both", got)
	}
	if !st.Proxy.Running {
		t.Error("转发服务应仍在运行")
	}
	if got := catalogSlugs(catalogModels(t, e)); !reflect.DeepEqual(got, slugsBefore) || !slices.Contains(got, any("gpt-a")) {
		t.Errorf("目录应与切换前一致且含官方条目: %v, 切换前 %v", got, slugsBefore)
	}
	if code := postViaProxy(t, e.listenAddr, "gpt-a"); code != http.StatusOK {
		t.Errorf("官方模型请求状态码 = %d, want 200 (转发到官方)", code)
	}
	hitsMu.Lock()
	defer hitsMu.Unlock()
	if officialHits != 1 {
		t.Errorf("官方服务收到 %d 次请求, want 1", officialHits)
	}
}

func TestAccess_SetForwardingTrueFailureKeepsMode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.detect.set(codexauth.NotLoggedIn)
	breakRelayWrite(t, e)

	if _, err := e.svc.SetForwarding(ctx, true); err == nil {
		t.Fatal("写入失败时 SetForwarding(true) 应返回错误")
	}
	if got := accessOf(t, e.svc.Status())["mode"]; got != "both" {
		t.Errorf("access.mode = %v, want both", got)
	}
}

func TestAccess_InvalidLastDetectedModeTreatedAsNone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true, withDetect(codexauth.Failed), withSettings(`{"forwarding":true,"last_detected_mode":"x"}`))
	st := e.initRelay(t)
	if got := accessOf(t, st)["target"]; got != "relay" {
		t.Errorf("target = %v, want relay", got)
	}
	st, err := e.svc.ApplyAccess(ctx)
	if err != nil {
		t.Fatalf("ApplyAccess: %v", err)
	}
	if got := accessOf(t, st)["mode"]; got != "relay" {
		t.Errorf("mode = %v, want relay", got)
	}
	if got := settingsJSON(t, e)["access_mode"]; got != "relay" {
		t.Errorf("access_mode = %v, want relay", got)
	}
}

func TestAccess_CloseForwardingClearsAccessModeKeepsLastDetected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if settingsJSON(t, e)["access_mode"] == nil {
		t.Fatal("打开转发后 access_mode 应已记录")
	}
	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	s := settingsJSON(t, e)
	if s["access_mode"] != nil {
		t.Errorf("access_mode = %v, want null", s["access_mode"])
	}
	if s["last_detected_mode"] != "relay" {
		t.Errorf("last_detected_mode = %v, want relay", s["last_detected_mode"])
	}
}

func TestAccess_CloseForwardingRestoreBlockedClearsMode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.handEditBaseURL(t)
	_, err := e.svc.SetForwarding(ctx, false)
	requireCode(t, err, "restore_blocked")
	if got := accessOf(t, e.svc.Status())["mode"]; got != nil {
		t.Errorf("access.mode = %v, want null", got)
	}
	if got := settingsJSON(t, e)["access_mode"]; got != nil {
		t.Errorf("access_mode = %v, want null", got)
	}
}

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
	if _, err := svc.Init(context.Background(), initParamsOf(t, initParams(e))); err != nil {
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
