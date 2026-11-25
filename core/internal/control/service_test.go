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
	raw       []string
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
	w.Header().Set("Content-Type", "application/json")
	if r.raw != nil {
		_, _ = io.WriteString(w, `{"data":[`+strings.Join(r.raw, ",")+`]}`)
		return
	}
	data := make([]map[string]string, 0, len(r.ids))
	for _, id := range r.ids {
		data = append(data, map[string]string{"id": id})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (r *fakeRelay) baseURL() string { return r.URL + "/v1" }

func (r *fakeRelay) setIDs(ids ...string) {
	r.mu.Lock()
	r.ids = ids
	r.mu.Unlock()
}

func (r *fakeRelay) setModelsJSON(raw ...string) {
	r.mu.Lock()
	r.raw = raw
	r.mu.Unlock()
}

func relayModelJSON(id string, efforts ...string) string {
	quoted := make([]string, 0, len(efforts))
	for _, e := range efforts {
		quoted = append(quoted, strconv.Quote(e))
	}
	return `{"id":` + strconv.Quote(id) + `,"context_length":1048576,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":[` + strings.Join(quoted, ",") + `]}}`
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

func TestSetForwardingTrue_RelayModelsUsePrefixedSlugAndOriginalNameUpstream(t *testing.T) {
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
	if want := []any{"gpt-a", "gpt-b", "relay-gpt-a", "relay-deepseek-flash", "relay-unknown-model-x"}; !reflect.DeepEqual(slugs, want) {
		t.Errorf("slugs = %v, want %v（中转返回的陌生模型也进目录）", slugs, want)
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
	e.relay.setIDs("deepseek-flash", "gpt-b")
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

func TestForwarding_RelayEffortForwardedUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		effort string
		zstd   bool
	}{
		{"接口声明 max 的模型 max 原样", "relay-deepseek-flash", "max", false},
		{"接口声明 max 的模型 xhigh 原样", "relay-deepseek-flash", "xhigh", false},
		{"接口声明 max 的模型 ultra 原样", "relay-deepseek-flash", "ultra", false},
		{"接口声明 max 的模型 ultra 原样（zstd）", "relay-deepseek-flash", "ultra", true},
		{"接口声明 max 的模型 high 原样", "relay-deepseek-flash", "high", false},
		{"接口没声明 max 的模型 ultra 原样", "relay-no-max", "ultra", false},
		{"接口没给思考等级的同名模型 ultra 原样", "relay-gpt-a", "ultra", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.relay.setModelsJSON(relayModelJSON("deepseek-flash", "low", "high", "max"), relayModelJSON("no-max", "low", "high"), `{"id":"gpt-a"}`)
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
			if sent.Reasoning.Effort != tt.effort {
				t.Errorf("中转收到的 effort = %q, want %q（%s）", sent.Reasoning.Effort, tt.effort, bodies[0])
			}
		})
	}
}

func TestRelayOnly_ConfigEnabledEffortsKeepMaxWithoutUltra(t *testing.T) {
	e := newEnv(t, true, withDetect(codexauth.NotLoggedIn))
	e.relay.setModelsJSON(relayModelJSON("deepseek-flash", "low", "high", "max"))
	e.initRelay(t)
	desktop, _ := parseConfig(e.config(t))["desktop"].(map[string]any)
	got, _ := desktop["enabled-reasoning-efforts"].([]any)
	want := []any{"low", "high", "max"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enabled-reasoning-efforts = %v, want %v", got, want)
	}
}

func TestForwarding_ModelWithMaxEffort_NoReasoningFieldsAdded(t *testing.T) {
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
			e.relay.setModelsJSON(relayModelJSON("deepseek-flash", "low", "high", "max"))
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
	full := accessOf(t, st)
	got := map[string]any{"mode": full["mode"], "target": full["target"], "detect": full["detect"]}
	if !reflect.DeepEqual(got, want) {
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

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(c.now)
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

func TestCheckUpdate_LaterNotNewerClearsStoredUpdate(t *testing.T) {
	for _, manual := range []bool{true, false} {
		t.Run(fmt.Sprintf("manual=%v", manual), func(t *testing.T) {
			e := newEnv(t, false)
			g := newGitHubStub(t, "v1.3.0")
			svc := control.NewService(withUpdate(e.opts, g, nil))
			t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

			if _, err := svc.CheckUpdate(context.Background(), manual); err != nil {
				t.Fatal(err)
			}
			if svc.Status().Update.LatestVersion == nil {
				t.Fatal("前置：第一次检查后 Status.Update 应有值")
			}
			g.setBody(`{"tag_name":"v1.2.3","html_url":"https://github.com/acme/model-bridge/releases/tag/v1.2.3"}`)
			if _, err := svc.CheckUpdate(context.Background(), manual); err != nil {
				t.Fatal(err)
			}
			if up := svc.Status().Update; up.LatestVersion != nil || up.URL != nil {
				t.Errorf("Status.Update = %+v, want 空", up)
			}
		})
	}
}

func TestCheckUpdate_LaterFailureKeepsStoredUpdate(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	if _, err := svc.CheckUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	g.setStatus(500)
	_, err := svc.CheckUpdate(context.Background(), true)
	requireCode(t, err, "update_unavailable")
	up := svc.Status().Update
	if up.LatestVersion == nil || *up.LatestVersion != "1.3.0" || up.URL == nil {
		t.Errorf("Status.Update = %+v, 失败时应保留旧值", up)
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

func TestCheckUpdate_DevVersionSeesStableRelease(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	opts := withUpdate(e.opts, g, nil)
	opts.Version = "0.0.0-dev"
	svc := control.NewService(opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil || !res.IsNewer {
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

func withMissingCodex(e *env) {
	e.codexPath = filepath.Join(filepath.Dir(e.configPath), "no-such-codex")
}

func installCodex(t *testing.T, e *env) {
	t.Helper()
	script, err := os.ReadFile(writeFakeCodex(t, officialJSON, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.codexPath, script, 0o755); err != nil {
		t.Fatal(err)
	}
}

func holdPort(t *testing.T) (addr string, release func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func nested(t *testing.T, res map[string]any, key, field string) any {
	t.Helper()
	obj, ok := res[key].(map[string]any)
	if !ok {
		t.Fatalf("结果缺少 %s 对象: %v", key, res)
	}
	return obj[field]
}

func TestStatus_ForwardingOff_RedetectsChatGPT(t *testing.T) {
	e := newEnv(t, false, withMissingCodex)
	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	first := c.result("status", nil)
	if nested(t, first, "chatgpt", "found") != false || nested(t, first, "chatgpt", "error") != "chatgpt_not_found" {
		t.Fatalf("装上之前 chatgpt = %v", first["chatgpt"])
	}

	installCodex(t, e)
	st := c.result("status", nil)
	if nested(t, st, "chatgpt", "found") != true || nested(t, st, "chatgpt", "error") != nil {
		t.Errorf("装上之后 chatgpt = %v, want found=true error=null", st["chatgpt"])
	}
}

func TestStatus_ForwardingOff_RedetectsRestoreBlocked(t *testing.T) {
	e := newEnv(t, true)
	e.initRelay(t)
	e.shutDown = true
	if err := e.svc.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	enabledText := e.config(t)

	c := startServe(t, e.opts)
	c.result("init", initParams(e))
	blocked := func() []any {
		res := c.result("status", nil)
		list, _ := res["restore_blocked"].([]any)
		return list
	}
	if got := blocked(); len(got) != 0 {
		t.Fatalf("没改过配置时 restore_blocked = %v", got)
	}

	e.handEditBaseURL(t)
	if got := blocked(); !reflect.DeepEqual(got, []any{"openai_base_url"}) {
		t.Errorf("手改之后 restore_blocked = %v, want [openai_base_url]", got)
	}

	if err := os.WriteFile(e.configPath, []byte(enabledText), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := blocked(); len(got) != 0 {
		t.Errorf("改回去之后 restore_blocked = %v, want 空", got)
	}
}

func TestStatus_ForwardingOff_RestoreBlocked(t *testing.T) {
	setup := func(t *testing.T) (*env, *client, string) {
		t.Helper()
		e := newEnv(t, true)
		e.initRelay(t)
		e.shutDown = true
		if err := e.svc.Shutdown(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
			t.Fatal(err)
		}
		enabledText := e.config(t)
		statePath := filepath.Join(e.dataDir, "access-state.json")
		if !exists(statePath) {
			t.Fatal("前置条件：转发打开过，状态文件应存在")
		}
		c := startServe(t, e.opts)
		c.result("init", initParams(e))
		return e, c, enabledText
	}

	t.Run("手改项改回后 status 还原配置并删除状态文件", func(t *testing.T) {
		e, c, enabledText := setup(t)
		e.handEditBaseURL(t)
		c.result("status", nil)
		if err := os.WriteFile(e.configPath, []byte(enabledText), 0o644); err != nil {
			t.Fatal(err)
		}

		res := c.result("status", nil)

		if list, _ := res["restore_blocked"].([]any); len(list) != 0 {
			t.Errorf("restore_blocked = %v, want 空", list)
		}
		if got := e.config(t); got != originalConfig {
			t.Errorf("配置没还原到开启前: %q", got)
		}
		if exists(filepath.Join(e.dataDir, "access-state.json")) {
			t.Error("状态文件应已删除")
		}
	})

	t.Run("仍被手改时继续列出 restore_blocked 且文件不动", func(t *testing.T) {
		e, c, _ := setup(t)
		edited := e.handEditBaseURL(t)

		res := c.result("status", nil)

		if got, _ := res["restore_blocked"].([]any); !reflect.DeepEqual(got, []any{"openai_base_url"}) {
			t.Errorf("restore_blocked = %v, want [openai_base_url]", got)
		}
		if got := e.config(t); got != edited {
			t.Errorf("配置不该有任何改动: %q", got)
		}
		if !exists(filepath.Join(e.dataDir, "access-state.json")) {
			t.Error("状态文件应保留")
		}
	})
}

func TestForwardingOn_ProxyNotRunning_MethodRestartsProxy(t *testing.T) {
	causes := []struct {
		name  string
		setup func(t *testing.T) (mod func(*env), fix func(e *env))
	}{
		{"端口被占用", func(t *testing.T) (func(*env), func(*env)) {
			addr, release := holdPort(t)
			return func(e *env) { e.listenAddr = addr }, func(*env) { release() }
		}},
		{"启动时没找到 ChatGPT", func(t *testing.T) (func(*env), func(*env)) {
			return withMissingCodex, func(e *env) { installCodex(t, e) }
		}},
	}
	for _, method := range []string{"status", "refresh_models"} {
		for _, cause := range causes {
			t.Run(method+"/"+cause.name, func(t *testing.T) {
				mod, fix := cause.setup(t)
				e := newEnv(t, true, mod)
				c := startServe(t, e.opts)
				first := c.result("init", initParams(e))
				if nested(t, first, "proxy", "running") != false {
					t.Fatalf("初始状态应是转发服务没运行: %v", first)
				}
				fix(e)

				st := c.result(method, nil)
				if st["forwarding"] != true || nested(t, st, "proxy", "running") != true || nested(t, st, "proxy", "error") != nil {
					t.Errorf("proxy = %v, forwarding = %v, want 运行中且无错误", st["proxy"], st["forwarding"])
				}
				if nested(t, st, "chatgpt", "found") != true || nested(t, st, "chatgpt", "error") != nil {
					t.Errorf("chatgpt = %v, want found=true error=null", st["chatgpt"])
				}
				if !proxyReachable(e.listenAddr) {
					t.Error("转发服务应已在监听")
				}
				if !strings.Contains(e.config(t), proxyURLInConfig) {
					t.Errorf("配置应已接管: %q", e.config(t))
				}
			})
		}
	}
}

func TestStartFailure_RestoresPreviouslyTakenOverConfig(t *testing.T) {
	ctx := context.Background()
	prepare := func(t *testing.T, forwardingAfter bool) *env {
		e := newEnv(t, true)
		e.initRelay(t)
		e.shutDown = true
		if err := e.svc.Shutdown(ctx, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(e.config(t), proxyURLInConfig) {
			t.Fatalf("上次会话应留下接管后的配置: %q", e.config(t))
		}
		if !forwardingAfter {
			if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return e
	}
	requireRestored := func(t *testing.T, e *env, st control.Status, wantCode string) {
		t.Helper()
		if got := e.config(t); got != originalConfig {
			t.Errorf("配置应还原, 实际 %q", got)
		}
		if st.Proxy.Running || st.Proxy.Error == nil || *st.Proxy.Error != wantCode {
			t.Errorf("Proxy = %+v, want 未运行且 error=%s", st.Proxy, wantCode)
		}
	}

	t.Run("Init 时转发服务启动失败", func(t *testing.T) {
		e := prepare(t, true)
		addr, _ := holdPort(t)
		e.opts.ListenAddr = addr
		svc := control.NewService(e.opts)
		t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })
		st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
		if err != nil {
			t.Fatal(err)
		}
		requireRestored(t, e, st, "proxy_port_in_use")
	})

	t.Run("开启转发时转发服务启动失败", func(t *testing.T) {
		e := prepare(t, false)
		addr, _ := holdPort(t)
		e.opts.ListenAddr = addr
		svc := control.NewService(e.opts)
		t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })
		if _, err := svc.Init(ctx, initParamsOf(t, initParams(e))); err != nil {
			t.Fatal(err)
		}
		_, err := svc.SetForwarding(ctx, true)
		requireCode(t, err, "proxy_port_in_use")
		requireRestored(t, e, svc.Status(), "proxy_port_in_use")
	})

	t.Run("生成目录失败", func(t *testing.T) {
		e := prepare(t, true)
		if err := os.Remove(e.catalogPath()); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(e.catalogPath(), 0o755); err != nil {
			t.Fatal(err)
		}
		svc := control.NewService(e.opts)
		t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })
		st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
		if err != nil {
			t.Fatal(err)
		}
		if got := e.config(t); got != originalConfig {
			t.Errorf("配置应还原, 实际 %q", got)
		}
		if st.Proxy.Error == nil {
			t.Errorf("Proxy.Error 应带失败原因: %+v", st.Proxy)
		}
	})
}

func leaveTakenOverConfig(t *testing.T, e *env, forwardingAfter bool) {
	t.Helper()
	e.initRelay(t)
	e.shutDown = true
	if err := e.svc.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.config(t), proxyURLInConfig) {
		t.Fatalf("上次会话应留下接管后的配置: %q", e.config(t))
	}
	if !forwardingAfter {
		if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartFailure_ConfigWithNothingTakenOverIsRestored(t *testing.T) {
	ctx := context.Background()
	const plain = "sandbox = \"read-only\"\n"
	withPlainConfig := func(e *env) {
		if err := os.WriteFile(e.configPath, []byte(plain), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name            string
		forwardingAfter bool
		start           func(t *testing.T, svc *control.Service, e *env) control.Status
	}{
		{"Init 时转发服务启动失败", true, func(t *testing.T, svc *control.Service, e *env) control.Status {
			st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
			if err != nil {
				t.Fatal(err)
			}
			return st
		}},
		{"开启转发时转发服务启动失败", false, func(t *testing.T, svc *control.Service, e *env) control.Status {
			if _, err := svc.Init(ctx, initParamsOf(t, initParams(e))); err != nil {
				t.Fatal(err)
			}
			_, err := svc.SetForwarding(ctx, true)
			requireCode(t, err, "proxy_port_in_use")
			return svc.Status()
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, withPlainConfig)
			leaveTakenOverConfig(t, e, tc.forwardingAfter)
			addr, _ := holdPort(t)
			e.opts.ListenAddr = addr
			svc := control.NewService(e.opts)
			t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })

			st := tc.start(t, svc, e)
			if got := e.config(t); got != plain {
				t.Errorf("配置应恢复成原样, 实际 %q", got)
			}
			if st.Proxy.Running || st.Proxy.Error == nil || *st.Proxy.Error != "proxy_port_in_use" {
				t.Errorf("Proxy = %+v, want 未运行且 error=proxy_port_in_use", st.Proxy)
			}
		})
	}
}

func TestStartFailure_RestoreBlockedIsListedInStatus(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	leaveTakenOverConfig(t, e, true)
	e.handEditBaseURL(t)
	addr, _ := holdPort(t)
	e.opts.ListenAddr = addr
	svc := control.NewService(e.opts)
	t.Cleanup(func() { _ = svc.Shutdown(ctx, false) })

	st, err := svc.Init(ctx, initParamsOf(t, initParams(e)))
	if err != nil {
		t.Fatal(err)
	}
	if st.Proxy.Error == nil || *st.Proxy.Error != "proxy_port_in_use" {
		t.Errorf("Proxy = %+v, want error=proxy_port_in_use", st.Proxy)
	}
	if !reflect.DeepEqual(st.RestoreBlocked, []string{"openai_base_url"}) {
		t.Errorf("RestoreBlocked = %v, want [openai_base_url]", st.RestoreBlocked)
	}
}

func twoRelayParams(e *env, second *fakeRelay) map[string]any {
	return initParamsFor(
		relayJSON("1", "中转", e.relay.baseURL(), keyA),
		relayJSON("2", "备用", second.baseURL(), keyA),
	)
}

func TestRefreshModels_RestartingProxyDoesNotFetchRelaysTwice(t *testing.T) {
	addr, release := holdPort(t)
	e := newEnv(t, true, func(e *env) { e.listenAddr = addr })
	second := newFakeRelay(t, []string{"deepseek-flash"}, keyA)
	c := startServe(t, e.opts)
	c.result("init", twoRelayParams(e, second))
	release()
	before1, before2 := e.relay.hits(), second.hits()

	st := c.result("refresh_models", nil)
	if nested(t, st, "proxy", "running") != true {
		t.Fatalf("转发服务应已重启: %v", st["proxy"])
	}
	if got := e.relay.hits() - before1; got != 1 {
		t.Errorf("中转 1 被拉取 %d 次, want 1", got)
	}
	if got := second.hits() - before2; got != 1 {
		t.Errorf("中转 2 被拉取 %d 次, want 1", got)
	}
}

func TestRefreshModels_CorruptCacheShowsErrorOnThatRelay(t *testing.T) {
	for _, forwarding := range []bool{false, true} {
		name := "转发关着"
		if forwarding {
			name = "转发开着"
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, forwarding)
			second := newFakeRelay(t, []string{"deepseek-flash"}, keyA)
			c := startServe(t, e.opts)
			c.result("init", twoRelayParams(e, second))
			if err := os.WriteFile(filepath.Join(e.dataDir, "relay-models-2.json"), []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}

			res := c.result("refresh_models", map[string]any{"id": "1"})
			for _, where := range []struct {
				name string
				res  map[string]any
			}{{"refresh_models", res}, {"之后的 status", c.result("status", nil)}} {
				relays := relayList(t, where.res)
				if len(relays) != 2 {
					t.Fatalf("%s relays = %v", where.name, relays)
				}
				if relays[1]["error"] == nil {
					t.Errorf("%s: 缓存损坏的中转 2 应带错误: %v", where.name, relays[1])
				}
				if relays[0]["error"] != nil {
					t.Errorf("%s: 中转 1 不该带错误: %v", where.name, relays[0])
				}
			}
		})
	}
}

func newHangingRelay(t *testing.T) string {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(srv.Close)
	trustCert(t, srv)
	t.Cleanup(func() { close(done) })
	return srv.URL + "/v1"
}

func hangingInitParams(hangURL string) map[string]any {
	return initParamsFor(relayJSON("1", "中转", hangURL, keyA))
}

func setRestartTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := control.RestartTimeout
	control.RestartTimeout = d
	t.Cleanup(func() { control.RestartTimeout = old })
}

func TestStatus_RestartIsLimitedByRestartTimeout(t *testing.T) {
	setRestartTimeout(t, time.Second)
	addr, release := holdPort(t)
	e := newEnv(t, true, func(e *env) { e.listenAddr = addr })
	hang := newHangingRelay(t)
	c := startServe(t, e.opts)
	c.result("init", hangingInitParams(hang))
	release()

	start := time.Now()
	st := c.result("status", nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("status 耗时 %v, 重启应在 RestartTimeout(1 秒)后放弃", elapsed)
	}
	if nested(t, st, "proxy", "running") != false {
		t.Errorf("上游挂住时转发服务不该显示运行中: %v", st["proxy"])
	}
}

func TestInit_UpstreamHangIsLimitedByRestartTimeout(t *testing.T) {
	setRestartTimeout(t, time.Second)
	e := newEnv(t, true)
	hang := newHangingRelay(t)

	start := time.Now()
	_, err := e.svc.Init(context.Background(), initParamsOf(t, hangingInitParams(hang)))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Init 耗时 %v, 上游挂住时应在 RestartTimeout(1 秒)后返回", elapsed)
	}
}

func TestStatus_ChatGPTUnsupported_PersistsUntilRedetectSucceeds(t *testing.T) {
	healCodex := func(t *testing.T, e *env) {
		t.Helper()
		out := filepath.Join(filepath.Dir(e.codexPath), "out.json")
		body := "#!/bin/sh\ncat '" + out + "'\n"
		if err := os.WriteFile(e.codexPath, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	broken := func(t *testing.T) func(*env) {
		return func(e *env) { e.codexPath = writeFakeCodex(t, officialJSON, 1) }
	}
	requireUnsupported := func(t *testing.T, st control.Status) {
		t.Helper()
		if !st.ChatGPT.Found || st.ChatGPT.Error == nil || *st.ChatGPT.Error != "chatgpt_unsupported" {
			t.Errorf("ChatGPT = %+v, want Found 且 Error=chatgpt_unsupported", st.ChatGPT)
		}
	}

	t.Run("打开转发失败后转发关着轮询 status 不清掉", func(t *testing.T) {
		e := newEnv(t, false, broken(t))
		e.initRelay(t)
		_, err := e.svc.SetForwarding(context.Background(), true)
		requireCode(t, err, "chatgpt_unsupported")

		for range 3 {
			st := e.svc.Status()
			if st.Forwarding {
				t.Fatalf("Forwarding = true, want 保持关闭")
			}
			requireUnsupported(t, st)
		}
	})

	t.Run("下一次重新检测成功后清掉", func(t *testing.T) {
		e := newEnv(t, false, broken(t))
		e.initRelay(t)
		_, err := e.svc.SetForwarding(context.Background(), true)
		requireCode(t, err, "chatgpt_unsupported")
		requireUnsupported(t, e.svc.Status())

		healCodex(t, e)
		st := e.svc.Status()

		if !st.ChatGPT.Found || st.ChatGPT.Error != nil {
			t.Errorf("ChatGPT = %+v, want Found 且 Error=nil", st.ChatGPT)
		}
	})

	t.Run("启动时转发应开但读不到清单 status 一直保留", func(t *testing.T) {
		e := newEnv(t, true, broken(t))
		requireUnsupported(t, e.initRelay(t))

		for range 3 {
			requireUnsupported(t, e.svc.Status())
		}
	})

	t.Run("重新进入检测流程后仍读不到则继续保留", func(t *testing.T) {
		e := newEnv(t, false, broken(t))
		e.initRelay(t)
		_, err := e.svc.SetForwarding(context.Background(), true)
		requireCode(t, err, "chatgpt_unsupported")

		st, _ := e.svc.SetForwarding(context.Background(), true)

		requireUnsupported(t, st)
	})
}

func leftoverEnabledEnv(t *testing.T) (*env, string) {
	t.Helper()
	e := newEnv(t, true)
	e.initRelay(t)
	e.shutDown = true
	if err := e.svc.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dataDir, "settings.json"), []byte(`{"forwarding":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	enabledText := e.config(t)
	if !exists(filepath.Join(e.dataDir, "access-state.json")) {
		t.Fatal("前置条件：转发打开过，状态文件应存在")
	}
	return e, enabledText
}

func TestStatus_ForwardingOff_RestoreFailsForOtherReason_KeepsRestoreBlockedAndRetries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录只读限制")
	}
	e, enabledText := leftoverEnabledEnv(t)
	svc := control.NewService(e.opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })
	e.handEditBaseURL(t)
	if _, err := svc.Init(context.Background(), initParamsOf(t, initParams(e))); err != nil {
		t.Fatal(err)
	}
	if got := svc.Status().RestoreBlocked; !reflect.DeepEqual(got, []string{"openai_base_url"}) {
		t.Fatalf("前置条件：restore_blocked = %v", got)
	}
	if err := os.WriteFile(e.configPath, []byte(enabledText), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(e.configPath)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	st := svc.Status()

	if !reflect.DeepEqual(st.RestoreBlocked, []string{"openai_base_url"}) {
		t.Errorf("还原失败后 restore_blocked = %v, want 保留 [openai_base_url]", st.RestoreBlocked)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	st = svc.Status()

	if len(st.RestoreBlocked) != 0 {
		t.Errorf("目录恢复可写后 restore_blocked = %v, want 空", st.RestoreBlocked)
	}
	if got := e.config(t); got != originalConfig {
		t.Errorf("配置没还原到开启前: %q", got)
	}
	if exists(filepath.Join(e.dataDir, "access-state.json")) {
		t.Error("状态文件应已删除")
	}
}

func TestStatus_FirstStatusAfterInit_DoesNotTouchConfigOrState(t *testing.T) {
	for _, handEdited := range []bool{false, true} {
		t.Run("手改过="+strconv.FormatBool(handEdited), func(t *testing.T) {
			e, _ := leftoverEnabledEnv(t)
			if handEdited {
				e.handEditBaseURL(t)
			}
			svc := control.NewService(e.opts)
			t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })
			if _, err := svc.Init(context.Background(), initParamsOf(t, initParams(e))); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(e.dataDir, "access-state.json")
			cfgBefore, stateBefore := readFile(t, e.configPath), readFile(t, statePath)

			svc.Status()

			if got := readFile(t, e.configPath); got != cfgBefore {
				t.Errorf("config.toml 变了: %q -> %q", cfgBefore, got)
			}
			if got := readFile(t, statePath); got != stateBefore {
				t.Errorf("状态文件变了: %q -> %q", stateBefore, got)
			}
		})
	}
}

func TestForwardingOn_ChatGPTUnavailable_RelayOperationsDoNotActivate(t *testing.T) {
	variants := []struct {
		name string
		mod  func(t *testing.T) func(*env)
	}{
		{"官方清单读不到", func(t *testing.T) func(*env) {
			return func(e *env) { e.codexPath = writeFakeCodex(t, officialJSON, 1) }
		}},
		{"找不到 ChatGPT", func(t *testing.T) func(*env) {
			return func(e *env) { e.codexPath = filepath.Join(t.TempDir(), "no-such-codex") }
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t, true, v.mod(t))
			e.relay.keys[keyB] = true
			st := e.initRelay(t)
			if !st.Forwarding || st.Proxy.Running {
				t.Fatalf("前置条件：core 应报告 forwarding=true 且转发服务没运行: %+v", st)
			}
			requireUntouched := func(step string, st control.Status) {
				t.Helper()
				if !st.Forwarding || st.Proxy.Running {
					t.Errorf("%s 后 Forwarding=%v Proxy.Running=%v, want true/false", step, st.Forwarding, st.Proxy.Running)
				}
				if got := e.config(t); got != originalConfig {
					t.Errorf("%s 改动了 Codex 配置: %q", step, got)
				}
				if exists(e.catalogPath()) {
					t.Errorf("%s 写了合并目录", step)
				}
			}

			saved, err := e.svc.SaveRelay(ctx, control.SaveRelayParams{Name: "备用", BaseURL: e.relay.baseURL(), APIKey: keyB})
			if err != nil {
				t.Fatalf("SaveRelay: %v", err)
			}
			requireUntouched("save_relay", e.svc.Status())

			st, err = e.svc.RefreshModels(ctx, nil)
			if err != nil {
				t.Fatalf("RefreshModels: %v", err)
			}
			requireUntouched("refresh_models", st)

			st, err = e.svc.DeleteRelay(ctx, saved.ID)
			if err != nil {
				t.Fatalf("DeleteRelay: %v", err)
			}
			requireUntouched("delete_relay", st)
			if len(st.Relays) != 1 {
				t.Errorf("relays = %d, want 1", len(st.Relays))
			}
		})
	}
}

func TestForwardingOn_ChatGPTUnsupported_RestoreFailsForOtherReason_NotReportedAsProxyError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录只读限制")
	}
	e := newEnv(t, true)
	e.initRelay(t)
	e.shutDown = true
	if err := e.svc.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	enabledText := e.config(t)
	if enabledText == originalConfig || !exists(filepath.Join(e.dataDir, "access-state.json")) {
		t.Fatal("前置条件：配置应处于开启状态且状态文件存在")
	}
	out := filepath.Join(filepath.Dir(e.codexPath), "out.json")
	if err := os.WriteFile(e.codexPath, []byte("#!/bin/sh\ncat '"+out+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(e.configPath)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	svc := control.NewService(e.opts)
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	st, err := svc.Init(context.Background(), initParamsOf(t, initParams(e)))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if st.ChatGPT.Error == nil || *st.ChatGPT.Error != "chatgpt_unsupported" {
		t.Errorf("ChatGPT = %+v, want Error=chatgpt_unsupported", st.ChatGPT)
	}
	if st.Proxy.Error != nil {
		t.Errorf("Proxy.Error = %q, 还原失败不能显示成「本机转发服务没能启动」", *st.Proxy.Error)
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st = svc.Status()

	if got := e.config(t); got != originalConfig {
		t.Errorf("目录恢复可写后 status 没有重试还原: %q", got)
	}
	if exists(filepath.Join(e.dataDir, "access-state.json")) {
		t.Error("状态文件应已删除")
	}
	if st.Proxy.Error != nil {
		t.Errorf("Proxy.Error = %q, want nil", *st.Proxy.Error)
	}
}

func TestStatus_RestartTimeoutReportsProxyStartTimeout(t *testing.T) {
	setRestartTimeout(t, time.Second)
	addr, release := holdPort(t)
	e := newEnv(t, true, func(e *env) { e.listenAddr = addr })
	hang := newHangingRelay(t)
	c := startServe(t, e.opts)
	c.result("init", hangingInitParams(hang))
	release()

	st := c.result("status", nil)
	if got := nested(t, st, "proxy", "error"); got != "proxy_start_timeout" {
		t.Errorf("proxy.error = %v, want proxy_start_timeout", got)
	}
}

func TestInit_RestartTimeoutReportsProxyStartTimeout(t *testing.T) {
	setRestartTimeout(t, time.Second)
	e := newEnv(t, true)
	hang := newHangingRelay(t)

	st, err := e.svc.Init(context.Background(), initParamsOf(t, hangingInitParams(hang)))
	if err != nil {
		t.Fatal(err)
	}
	if st.Proxy.Error == nil || *st.Proxy.Error != "proxy_start_timeout" {
		t.Errorf("Proxy.Error = %v, want proxy_start_timeout", st.Proxy.Error)
	}
}

func relayErrorOf(t *testing.T, addr, model string) (code, message string) {
	t.Helper()
	status, body := postStatusBody(t, addr, model)
	if status < 400 {
		t.Fatalf("状态码 = %d, 响应 %q", status, body)
	}
	var m struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("响应不是错误体: %q", body)
	}
	return m.Error.Code, m.Error.Message
}

func TestForwarding_RelayModelErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		prepare     func(t *testing.T, e *env, second *fakeRelay)
		model       string
		wantCode    string
		wantMessage string
	}{
		{"配置已删除", func(t *testing.T, e *env, _ *fakeRelay) {
			if _, err := e.svc.DeleteRelay(ctx, "2"); err != nil {
				t.Fatal(err)
			}
		}, "relay2-deepseek-flash", "relay_deleted", "这个模型所在的中转配置已删除，请换一个模型。"},
		{"配置还在但模型列表从没获取到", func(t *testing.T, e *env, second *fakeRelay) {
			second.setStatus(http.StatusInternalServerError)
		}, "relay2-deepseek-flash", "relay_not_fetched", "还没获取到「备用」的模型列表。请打开模型接入助手，在「备用」那一行点「重试」，或者换一个模型。"},
		{"配置还在但列表里没有这个模型", func(*testing.T, *env, *fakeRelay) {}, "relay-no-such-model", "relay_model_missing", "「中转」的模型列表里现在没有这个模型，请换一个模型。"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			second := newFakeRelay(t, []string{"deepseek-flash"}, keyA)
			if tc.wantCode == "relay_not_fetched" {
				tc.prepare(t, e, second)
			}
			if _, err := e.svc.Init(ctx, initParamsOf(t, twoRelayParams(e, second))); err != nil {
				t.Fatal(err)
			}
			if tc.wantCode != "relay_not_fetched" {
				tc.prepare(t, e, second)
			}

			code, msg := relayErrorOf(t, e.listenAddr, tc.model)
			if code != tc.wantCode || msg != tc.wantMessage {
				t.Errorf("code = %q, message = %q\nwant code = %q, message = %q", code, msg, tc.wantCode, tc.wantMessage)
			}
		})
	}
}

func platformAssetSuffix() string {
	switch runtime.GOOS {
	case "darwin":
		return "-macos.dmg"
	case "windows":
		return "-windows-setup.exe"
	}
	return ""
}

func releaseBodyWithInstaller(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	base := "https://github.com/acme/model-bridge/releases"
	return fmt.Sprintf(`{"tag_name":%q,"html_url":"%s/tag/%s","assets":[`+
		`{"name":"model-bridge-%s-macos.dmg","browser_download_url":"%s/download/%s/model-bridge-%s-macos.dmg"},`+
		`{"name":"model-bridge-%s-windows-setup.exe","browser_download_url":"%s/download/%s/model-bridge-%s-windows-setup.exe"}]}`,
		tag, base, tag, v, base, tag, v, v, base, tag, v)
}

func wantInstallerURL(tag string) (url string, installer bool) {
	base := "https://github.com/acme/model-bridge/releases"
	v := strings.TrimPrefix(tag, "v")
	if suffix := platformAssetSuffix(); suffix != "" {
		return base + "/download/" + tag + "/model-bridge-" + v + suffix, true
	}
	return base + "/tag/" + tag, false
}

func TestCheckUpdate_InstallerFollowsPlatform(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	g.setBody(releaseBodyWithInstaller("v1.3.0"))
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	wantURL, wantInstaller := wantInstallerURL("v1.3.0")
	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if !res.IsNewer || res.Installer != wantInstaller || res.URL == nil || *res.URL != wantURL {
		t.Errorf("result = %+v, want URL %q installer %v", res, wantURL, wantInstaller)
	}
	up := svc.Status().Update
	if up.Installer != wantInstaller || up.URL == nil || *up.URL != wantURL {
		t.Errorf("Status.Update = %+v, want URL %q installer %v", up, wantURL, wantInstaller)
	}
}

func TestCheckUpdate_NoInstallerAssetFallsBackToReleasePage(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.3.0")
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	wantURL := "https://github.com/acme/model-bridge/releases/tag/v1.3.0"
	if !res.IsNewer || res.Installer || res.URL == nil || *res.URL != wantURL {
		t.Errorf("result = %+v", res)
	}
	if up := svc.Status().Update; up.Installer || up.URL == nil || *up.URL != wantURL {
		t.Errorf("Status.Update = %+v", up)
	}
}

func TestCheckUpdate_NoNewVersionInstallerFalse(t *testing.T) {
	e := newEnv(t, false)
	g := newGitHubStub(t, "v1.2.3")
	g.setBody(releaseBodyWithInstaller("v1.2.3"))
	svc := control.NewService(withUpdate(e.opts, g, nil))
	t.Cleanup(func() { _ = svc.Shutdown(context.Background(), false) })

	res, err := svc.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsNewer || res.Installer || svc.Status().Update.Installer {
		t.Errorf("result = %+v, Status.Update = %+v", res, svc.Status().Update)
	}
}

func settingsBytes(t *testing.T, e *env) string {
	t.Helper()
	return readFile(t, filepath.Join(e.dataDir, "settings.json"))
}

func TestRelayOnly_OnWhileForwardingOpen(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.SetRelayOnly(ctx, true)
	if err != nil {
		t.Fatalf("SetRelayOnly: %v", err)
	}
	requireAccess(t, st, "relay", "relay", "logged_in")
	acc := accessOf(t, st)
	if acc["relay_only"] != true || acc["logged_in"] != true {
		t.Errorf("access = %v, want relay_only=true logged_in=true", acc)
	}
	if cfg := e.config(t); !isRelayConfig(cfg) {
		t.Errorf("配置不是只用中转方式: %q", cfg)
	}
	if got := settingsJSON(t, e)["relay_only"]; got != true {
		t.Errorf("settings relay_only = %v, want true", got)
	}
}

func TestRelayOnly_SwitchMatchesReopenAndCloseRestores(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	original := e.config(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}

	st, err := e.svc.SetRelayOnly(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	relayCfg := e.config(t)
	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := e.config(t); got != relayCfg {
		t.Errorf("只用中转: 切换后配置 %q 与关闭再打开 %q 不一致", relayCfg, got)
	}

	st, err = e.svc.SetRelayOnly(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	requireAccess(t, st, "both", "both", "logged_in")
	if acc := accessOf(t, st); acc["relay_only"] != false {
		t.Errorf("relay_only = %v, want false", acc["relay_only"])
	}
	bothCfg := e.config(t)
	if !isBothConfig(bothCfg) {
		t.Errorf("配置不是官方加中转方式: %q", bothCfg)
	}
	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := e.config(t); got != bothCfg {
		t.Errorf("官方加中转: 切换后配置 %q 与关闭再打开 %q 不一致", bothCfg, got)
	}

	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := e.config(t); got != original {
		t.Errorf("关闭转发后配置 = %q, want 打开前 %q", got, original)
	}
}

func TestRelayOnly_OnDetectChangesKeepTargetEqualMode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetRelayOnly(ctx, true); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		result       codexauth.Result
		wantDetect   string
		wantLast     string
		wantLoggedIn bool
	}{
		{codexauth.NotLoggedIn, "not_logged_in", "relay", false},
		{codexauth.LoggedIn, "logged_in", "both", true},
		{codexauth.Failed, "failed", "both", true},
	}
	for _, s := range steps {
		e.detect.set(s.result)
		st, err := e.svc.DetectAccess(ctx)
		if err != nil {
			t.Fatalf("DetectAccess: %v", err)
		}
		requireAccess(t, st, "relay", "relay", s.wantDetect)
		if acc := accessOf(t, st); acc["logged_in"] != s.wantLoggedIn || acc["relay_only"] != true {
			t.Errorf("%s: access = %v", s.result, acc)
		}
		if got := settingsJSON(t, e)["last_detected_mode"]; got != s.wantLast {
			t.Errorf("%s: last_detected_mode = %v, want %s", s.result, got, s.wantLast)
		}
	}
}

func TestRelayOnly_PersistsAcrossRestartAndReopen(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true, withDetect(codexauth.LoggedIn), withSettings(`{"forwarding":true,"relay_only":true}`))
	st := e.initRelay(t)
	requireAccess(t, st, "relay", "relay", "logged_in")
	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.SetForwarding(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	requireAccess(t, st, "relay", "relay", "logged_in")
	if acc := accessOf(t, st); acc["relay_only"] != true {
		t.Errorf("relay_only = %v, want true", acc["relay_only"])
	}
	if got := settingsJSON(t, e)["relay_only"]; got != true {
		t.Errorf("settings relay_only = %v, want true", got)
	}
}

func TestRelayOnly_DefaultsWhenFieldMissing(t *testing.T) {
	e := newEnv(t, true, withDetect(codexauth.Failed))
	st := e.initRelay(t)
	acc := accessOf(t, st)
	if acc["relay_only"] != false {
		t.Errorf("relay_only = %v, want false", acc["relay_only"])
	}
	if acc["logged_in"] != false {
		t.Errorf("logged_in = %v, want false", acc["logged_in"])
	}
	if len(acc) != 5 {
		t.Errorf("access 键数 = %d, want 5: %v", len(acc), acc)
	}
}

func TestRelayOnly_ForwardingOffOnlySavesFlag(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	before := e.config(t)
	if _, err := e.svc.SetRelayOnly(ctx, true); err != nil {
		t.Fatalf("SetRelayOnly: %v", err)
	}
	if got := e.config(t); got != before {
		t.Errorf("配置被改动: %q", got)
	}
	if got := settingsJSON(t, e)["relay_only"]; got != true {
		t.Errorf("settings relay_only = %v, want true", got)
	}
	if acc := accessOf(t, e.svc.Status()); acc["relay_only"] != true {
		t.Errorf("Status relay_only = %v, want true", acc["relay_only"])
	}
}

func TestRelayOnly_WriteFailureKeepsEverything(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgBefore := e.config(t)
	settingsBefore := settingsBytes(t, e)
	accBefore := accessOf(t, e.svc.Status())

	_, err := e.svc.SetRelayOnly(ctx, true)
	requireCode(t, err, "config_invalid")

	accAfter := accessOf(t, e.svc.Status())
	for _, k := range []string{"mode", "target", "relay_only"} {
		if accAfter[k] != accBefore[k] {
			t.Errorf("%s = %v, want %v", k, accAfter[k], accBefore[k])
		}
	}
	if got := settingsBytes(t, e); got != settingsBefore {
		t.Errorf("settings.json 被改动: %q -> %q", settingsBefore, got)
	}
	if got := e.config(t); got != cfgBefore {
		t.Errorf("Codex 配置被改动: %q", got)
	}
}

func TestRelayOnly_WriteFailureCodeMatchesApplyAccess(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.detect.set(codexauth.NotLoggedIn)
	if _, err := e.svc.DetectAccess(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.ApplyAccess(ctx)
	requireCode(t, err, "config_invalid")
}

func TestRelayOnly_NeverDetectedCloseFallsBackToRelay(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.Failed))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got, ok := settingsJSON(t, e)["last_detected_mode"]; ok && got != nil && got != "" {
		t.Fatalf("前提不成立: last_detected_mode = %v", got)
	}
	if _, err := e.svc.SetRelayOnly(ctx, true); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.SetRelayOnly(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	requireAccess(t, st, "relay", "relay", "failed")
}

func TestRelayOnly_SettingsSaveFailureKeepsState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 chmod 不能让目录不可写")
	}
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	cfgBefore := e.config(t)
	accBefore := accessOf(t, e.svc.Status())

	if err := os.Chmod(e.dataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.dataDir, 0o755) })

	if _, err := e.svc.SetRelayOnly(ctx, true); err == nil {
		t.Fatal("数据目录不可写时应返回错误")
	}

	accAfter := accessOf(t, e.svc.Status())
	for _, k := range []string{"mode", "target", "relay_only"} {
		if accAfter[k] != accBefore[k] {
			t.Errorf("%s = %v, want %v", k, accAfter[k], accBefore[k])
		}
	}
	if got := e.config(t); got != cfgBefore {
		t.Errorf("Codex 配置被改动: %q", got)
	}
}

func TestRelayOnly_ForwardingOffNeverDetectedKeepsTargetNull(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.Failed))
	e.initRelay(t)
	st, err := e.svc.SetRelayOnly(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if acc := accessOf(t, st); acc["target"] != nil {
		t.Errorf("target = %v, want null", acc["target"])
	}
}

func lockDataDir(t *testing.T, e *env) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 chmod 不能让目录不可写")
	}
	if err := os.Chmod(e.dataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.dataDir, 0o755) })
}

func requireStateKept(t *testing.T, e *env, stBefore control.Status, cfgBefore string) {
	t.Helper()
	stAfter := e.svc.Status()
	if stAfter.Forwarding != stBefore.Forwarding {
		t.Errorf("forwarding = %v, want %v", stAfter.Forwarding, stBefore.Forwarding)
	}
	accBefore, accAfter := accessOf(t, stBefore), accessOf(t, stAfter)
	for _, k := range []string{"mode", "relay_only", "logged_in"} {
		if accAfter[k] != accBefore[k] {
			t.Errorf("access.%s = %v, want %v", k, accAfter[k], accBefore[k])
		}
	}
	if got := e.config(t); got != cfgBefore {
		t.Errorf("Codex 配置被改动: %q", got)
	}
}

func TestSettingsSaveFailure_ApplyAccessKeepsState(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	e.detect.set(codexauth.NotLoggedIn)
	if _, err := e.svc.DetectAccess(ctx); err != nil {
		t.Fatal(err)
	}
	stBefore, cfgBefore := e.svc.Status(), e.config(t)
	lockDataDir(t, e)

	if _, err := e.svc.ApplyAccess(ctx); err == nil {
		t.Fatal("设置文件保存失败时应返回错误")
	}
	requireStateKept(t, e, stBefore, cfgBefore)
}

func TestSettingsSaveFailure_SetForwardingOnKeepsState(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.LoggedIn))
	e.initRelay(t)
	stBefore, cfgBefore := e.svc.Status(), e.config(t)
	lockDataDir(t, e)

	if _, err := e.svc.SetForwarding(ctx, true); err == nil {
		t.Fatal("设置文件保存失败时应返回错误")
	}
	requireStateKept(t, e, stBefore, cfgBefore)
}

func TestSettingsSaveFailure_DetectAccessKeepsLastDetectedMode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, false, withDetect(codexauth.NotLoggedIn))
	e.initRelay(t)
	if _, err := e.svc.DetectAccess(ctx); err != nil {
		t.Fatal(err)
	}
	e.detect.set(codexauth.LoggedIn)
	stBefore, cfgBefore := e.svc.Status(), e.config(t)
	lockDataDir(t, e)

	if _, err := e.svc.DetectAccess(ctx); err == nil {
		t.Fatal("设置文件保存失败时应返回错误")
	}
	requireStateKept(t, e, stBefore, cfgBefore)
}

func TestSetForwardingFalse_AlreadyOffWithInvalidConfigKeepsOff(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	e.initRelay(t)
	e.handEditBaseURL(t)
	_, err := e.svc.SetForwarding(ctx, false)
	requireCode(t, err, "restore_blocked")
	if e.svc.Status().Forwarding {
		t.Fatal("restore_blocked 后 forwarding 应为 false")
	}
	if err := os.WriteFile(e.configPath, []byte("not = = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.SetForwarding(ctx, false); err == nil {
		t.Fatal("配置不合法时应返回错误")
	}
	if e.svc.Status().Forwarding {
		t.Error("Forwarding 应仍为 false")
	}
	if e.settingsForwarding(t) {
		t.Error("settings.json 的 forwarding 应仍为 false")
	}
}
