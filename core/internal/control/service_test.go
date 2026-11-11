package control_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
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
	st, err := e.svc.Init(context.Background(), control.InitParams{BaseURL: ptr(e.relay.baseURL()), APIKey: ptr(keyA)})
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

func TestInit(t *testing.T) {
	ctx := context.Background()

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
		if st.Relay.ModelCount != 1 || st.Relay.FetchedAt == nil || st.Relay.Stale || st.Relay.Error != nil {
			t.Errorf("Relay = %+v", st.Relay)
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
			t.Error("合并目录里没有中台模型")
		}
	})

	t.Run("转发关着时只检测 ChatGPT 不拉中台", func(t *testing.T) {
		e := newEnv(t, false)
		st := e.initRelay(t)
		if st.Forwarding || st.Proxy.Running {
			t.Errorf("转发应关闭: %+v", st)
		}
		if !st.ChatGPT.Found {
			t.Errorf("ChatGPT = %+v", st.ChatGPT)
		}
		if e.relay.hits() != 0 {
			t.Errorf("转发关着不该拉中台, 实际 %d 次", e.relay.hits())
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

	t.Run("base_url 为 null 时不拉中台", func(t *testing.T) {
		e := newEnv(t, true)
		st, err := e.svc.Init(ctx, control.InitParams{})
		if err != nil {
			t.Fatal(err)
		}
		if e.relay.hits() != 0 {
			t.Errorf("未设置中台不该拉取, 实际 %d 次", e.relay.hits())
		}
		if !st.Proxy.Running || st.Relay.ModelCount != 0 || st.Relay.FetchedAt != nil {
			t.Errorf("Status = %+v", st)
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

func TestSaveConnection_InvalidBaseURL(t *testing.T) {
	for _, base := range []string{"", "http://relay.example.com/v1", "relay.example.com/v1", "ftp://relay.example.com"} {
		t.Run("地址="+base, func(t *testing.T) {
			e := newEnv(t, false)
			_, err := e.svc.SaveConnection(context.Background(), base, keyA)
			requireCode(t, err, "invalid_base_url")
			if e.relay.hits() != 0 {
				t.Error("地址非法时不该访问中台")
			}
		})
	}
}

func TestSaveConnection_RelayErrors(t *testing.T) {
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
			e.relay.setStatus(tt.status)
			_, err := e.svc.SaveConnection(context.Background(), e.relay.baseURL(), keyB)
			requireCode(t, err, tt.wantCode)
		})
	}

	t.Run("relay_unreachable", func(t *testing.T) {
		e := newEnv(t, false)
		base := e.relay.baseURL()
		e.relay.Close()
		_, err := e.svc.SaveConnection(context.Background(), base, keyB)
		requireCode(t, err, "relay_unreachable")
	})
}

func TestSaveConnection_FailureKeepsOldConnection(t *testing.T) {
	e := newEnv(t, true)
	e.initRelay(t)

	_, err := e.svc.SaveConnection(context.Background(), e.relay.baseURL(), "sk-example-bad")
	requireCode(t, err, "relay_401")

	if code := postViaProxy(t, e.listenAddr, "deepseek-flash"); code != http.StatusOK {
		t.Fatalf("转发状态码 = %d", code)
	}
	auths := e.relay.forwardedAuths()
	if len(auths) != 1 || auths[0] != "Bearer "+keyA {
		t.Errorf("中台收到的 Authorization = %v, want 旧 Key", auths)
	}
	st, err := e.svc.RefreshModels(context.Background())
	if err != nil || st.Relay.Error != nil {
		t.Errorf("旧连接应仍可用: %v %+v", err, st.Relay)
	}
}

func TestSaveConnection_ModelsChanged(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	e.initRelay(t)

	res, err := e.svc.SaveConnection(ctx, e.relay.baseURL(), keyA)
	if err != nil || res.ModelsChanged || res.RelayModelCount != 1 {
		t.Fatalf("集合没变: %+v, %v", res, err)
	}

	e.relay.setIDs("deepseek-flash", "deepseek-v4-pro")
	res, err = e.svc.SaveConnection(ctx, e.relay.baseURL(), keyA)
	if err != nil || !res.ModelsChanged || res.RelayModelCount != 2 {
		t.Fatalf("多一个模型: %+v, %v", res, err)
	}
	if !strings.Contains(readFile(t, e.catalogPath()), "deepseek-v4-pro") {
		t.Error("保存连接后合并目录没有重新生成")
	}
}

func TestSaveConnection_ProxyUsesNewConfig(t *testing.T) {
	e := newEnv(t, true)
	e.relay.keys[keyB] = true
	e.initRelay(t)

	if _, err := e.svc.SaveConnection(context.Background(), e.relay.baseURL(), keyB); err != nil {
		t.Fatal(err)
	}
	if code := postViaProxy(t, e.listenAddr, "deepseek-flash"); code != http.StatusOK {
		t.Fatalf("转发状态码 = %d", code)
	}
	auths := e.relay.forwardedAuths()
	if len(auths) != 1 || auths[0] != "Bearer "+keyB {
		t.Errorf("中台收到的 Authorization = %v, want 新 Key", auths)
	}
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

func TestRefreshModels(t *testing.T) {
	ctx := context.Background()

	t.Run("中台失败时用上次记录并标记过期", func(t *testing.T) {
		e := newEnv(t, true)
		first := e.initRelay(t)

		e.relay.setStatus(500)
		st, err := e.svc.RefreshModels(ctx)
		if err != nil {
			t.Fatalf("RefreshModels: %v", err)
		}
		if !st.Relay.Stale || st.Relay.Error == nil || *st.Relay.Error != "relay_5xx" {
			t.Errorf("Relay = %+v", st.Relay)
		}
		if st.Relay.ModelCount != first.Relay.ModelCount {
			t.Errorf("ModelCount = %d, want %d", st.Relay.ModelCount, first.Relay.ModelCount)
		}
		if st.Relay.FetchedAt == nil || !st.Relay.FetchedAt.Equal(*first.Relay.FetchedAt) {
			t.Errorf("FetchedAt = %v, want %v", st.Relay.FetchedAt, first.Relay.FetchedAt)
		}
	})

	t.Run("中台恢复后清除过期标记", func(t *testing.T) {
		e := newEnv(t, true)
		e.initRelay(t)
		e.relay.setStatus(500)
		if _, err := e.svc.RefreshModels(ctx); err != nil {
			t.Fatal(err)
		}
		e.relay.setStatus(0)
		e.relay.setIDs("deepseek-flash", "deepseek-v4-pro")
		st, err := e.svc.RefreshModels(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Relay.Stale || st.Relay.Error != nil || st.Relay.ModelCount != 2 {
			t.Errorf("Relay = %+v", st.Relay)
		}
	})

	t.Run("从未成功过时模型数为 0", func(t *testing.T) {
		e := newEnv(t, true)
		e.relay.setStatus(401)
		e.initRelay(t)
		st, err := e.svc.RefreshModels(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Relay.Stale || st.Relay.Error == nil || *st.Relay.Error != "relay_401" {
			t.Errorf("Relay = %+v", st.Relay)
		}
		if st.Relay.ModelCount != 0 || st.Relay.FetchedAt != nil {
			t.Errorf("Relay = %+v", st.Relay)
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
	toolFiles := []string{"model-bridge-catalog.json", "access-state.json", "relay-models.json", "relay-reasoning-ids.json", "settings.json", "model-bridge.log"}

	plant := func(t *testing.T, e *env) {
		t.Helper()
		for _, name := range []string{"relay-reasoning-ids.json", "model-bridge.log"} {
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

func TestRefreshModels_RewritesEntriesWhenForwardingOn(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	e.initRelay(t)
	if strings.Contains(e.config(t), `model = "auto"`) {
		t.Fatalf("初始配置不该有 auto: %q", e.config(t))
	}

	e.relay.setIDs("auto", "deepseek-flash")
	if _, err := e.svc.RefreshModels(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.config(t), `model = "auto"`) {
		t.Errorf("中台出现 auto 后应写入 model: %q", e.config(t))
	}

	e.relay.setIDs("deepseek-flash")
	if _, err := e.svc.RefreshModels(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.config(t), `model = "auto"`) {
		t.Errorf("auto 消失后应还原 model: %q", e.config(t))
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

func TestSaveConnection_RebuildFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, true)
	e.relay.keys[keyB] = true
	e.initRelay(t)

	if err := os.Chmod(e.codexPath, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.SaveConnection(ctx, e.relay.baseURL(), keyB)
	requireCode(t, err, "chatgpt_unsupported")
	if err := os.Chmod(e.codexPath, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := e.svc.RefreshModels(ctx)
	if err != nil || st.Relay.Error != nil {
		t.Errorf("回滚后应仍用旧 Key: %v %+v", err, st.Relay)
	}
	if code := postViaProxy(t, e.listenAddr, "deepseek-flash"); code != http.StatusOK {
		t.Fatalf("转发状态码 = %d", code)
	}
	if auths := e.relay.forwardedAuths(); len(auths) != 1 || auths[0] != "Bearer "+keyA {
		t.Errorf("中台收到的 Authorization = %v, want 旧 Key", auths)
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

func TestSaveConnection_FetchesRelayOnce(t *testing.T) {
	for _, forwarding := range []bool{true, false} {
		t.Run("forwarding="+strconv.FormatBool(forwarding), func(t *testing.T) {
			e := newEnv(t, forwarding)
			e.initRelay(t)
			before := e.relay.hits()
			if _, err := e.svc.SaveConnection(context.Background(), e.relay.baseURL(), keyA); err != nil {
				t.Fatal(err)
			}
			if got := e.relay.hits() - before; got != 1 {
				t.Errorf("一次保存请求了 %d 次 /models, want 1", got)
			}
		})
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
	st, err := svc.Init(ctx, control.InitParams{BaseURL: ptr(e.relay.baseURL()), APIKey: ptr(keyA)})
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
	st, err := svc.Init(ctx, control.InitParams{BaseURL: ptr(e.relay.baseURL()), APIKey: ptr(keyA)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.RestoreBlocked, []string{"openai_base_url"}) {
		t.Errorf("重启后 RestoreBlocked = %v", st.RestoreBlocked)
	}
}
