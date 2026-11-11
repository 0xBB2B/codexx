package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var coreBinDir string

func TestMain(m *testing.M) {
	code := m.Run()
	if coreBinDir != "" {
		_ = os.RemoveAll(coreBinDir)
	}
	os.Exit(code)
}

func TestVersionFlag(t *testing.T) {
	tests := []struct {
		name      string
		ldflags   string
		wantPrint string
	}{
		{name: "未注入版本号打印开发版本", ldflags: "", wantPrint: "0.0.0-dev"},
		{name: "注入版本号后打印注入值", ldflags: "-X modelbridge/internal/version.value=1.3.0", wantPrint: "1.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "model-bridge-core")
			args := []string{"build", "-o", bin}
			if tt.ldflags != "" {
				args = append(args, "-ldflags", tt.ldflags)
			}
			args = append(args, ".")
			if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
				t.Fatalf("go build 失败: %v\n%s", err, out)
			}

			out, err := exec.Command(bin, "--version").Output()
			if err != nil {
				t.Fatalf("运行 --version 失败: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != tt.wantPrint {
				t.Errorf("--version 输出 = %q, want %q", got, tt.wantPrint)
			}
		})
	}
}

const (
	officialJSON     = `{"models":[{"slug":"gpt-a","priority":3,"display_name":"A","base_instructions":"x"},{"slug":"gpt-b","priority":7,"display_name":"B"}]}`
	originalConfig   = "sandbox = \"read-only\"\nopenai_base_url = \"https://old.example.com/v1\"\n"
	proxyURLInConfig = "http://127.0.0.1:47311/v1"
	secretKey        = "sk-example-secret-7788"
	otherKey         = "sk-example-other-9900"
)

var coreBuild struct {
	once sync.Once
	path string
	err  error
}

func buildCore(t *testing.T) string {
	t.Helper()
	coreBuild.once.Do(func() {
		dir, err := os.MkdirTemp("", "model-bridge-core-test-")
		if err != nil {
			coreBuild.err = err
			return
		}
		coreBinDir = dir
		coreBuild.path = filepath.Join(dir, "model-bridge-core")
		if out, err := exec.Command("go", "build", "-tags", "coretest", "-o", coreBuild.path, ".").CombinedOutput(); err != nil {
			coreBuild.err = &buildError{err: err, out: string(out)}
		}
	})
	if coreBuild.err != nil {
		t.Fatalf("go build 失败: %v", coreBuild.err)
	}
	return coreBuild.path
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

type fixture struct {
	dataDir    string
	home       string
	codexPath  string
	listenAddr string
	caFile     string
	relay      *httptest.Server

	mu        sync.Mutex
	forwarded []string
	hang      atomic.Bool
}

func newFixture(t *testing.T, forwarding bool) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell 脚本")
	}
	root := t.TempDir()
	f := &fixture{
		dataDir: filepath.Join(root, "data"),
		home:    filepath.Join(root, "codex-home"),
	}
	for _, d := range []string{f.dataDir, f.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(f.home, "config.toml"), originalConfig, 0o644)
	if forwarding {
		write(filepath.Join(f.dataDir, "settings.json"), `{"forwarding":true}`, 0o644)
	}
	write(filepath.Join(root, "out.json"), officialJSON, 0o644)
	f.codexPath = filepath.Join(root, "codex")
	write(f.codexPath, "#!/bin/sh\ncat '"+filepath.Join(root, "out.json")+"'\n", 0o755)

	f.relay = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			f.mu.Lock()
			f.forwarded = append(f.forwarded, auth)
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
			return
		}
		if f.hang.Load() {
			select {
			case <-time.After(14 * time.Second):
			case <-r.Context().Done():
			}
		}
		if auth != "Bearer "+secretKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"deepseek-flash"}]}`)
	}))
	t.Cleanup(f.relay.Close)
	f.caFile = filepath.Join(root, "relay-ca.pem")
	write(f.caFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.relay.Certificate().Raw})), 0o644)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.listenAddr = ln.Addr().String()
	_ = ln.Close()
	return f
}

func (f *fixture) relayBase() string { return f.relay.URL + "/v1" }

func (f *fixture) config(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f *fixture) initParams() map[string]any {
	return map[string]any{"base_url": f.relayBase(), "api_key": secretKey, "chatgpt_dir": nil}
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

type core struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	lines  chan map[string]any
	exited chan error
	stdout syncBuffer
	stderr syncBuffer
	nextID int
	events []map[string]any
}

func startCore(t *testing.T, f *fixture) *core {
	t.Helper()
	cmd := exec.Command(buildCore(t),
		"--data-dir", f.dataDir,
		"--listen", f.listenAddr,
		"--codex-path", f.codexPath,
		"--official-api-url", "https://api.example.com/v1",
		"--official-chatgpt-url", "https://chatgpt.example.com/backend-api/codex",
		"--relay-ca-file", f.caFile,
	)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "CODEX_HOME=" + f.home}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &core{t: t, cmd: cmd, in: in, lines: make(chan map[string]any, 64), exited: make(chan error, 1)}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			c.stdout.Write(append(append([]byte(nil), sc.Bytes()...), '\n'))
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("标准输出不是单行 JSON: %q", sc.Text())
				continue
			}
			c.lines <- m
		}
		close(c.lines)
		c.exited <- cmd.Wait()
	}()
	t.Cleanup(func() {
		_ = in.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})
	return c
}

func (c *core) call(method string, params any) map[string]any {
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
		select {
		case m, ok := <-c.lines:
			if !ok {
				c.t.Fatalf("核心提前退出, 没等到 id=%d 的响应; stderr=%s", id, c.stderr.String())
			}
			if _, isEvent := m["event"]; isEvent {
				c.events = append(c.events, m)
				continue
			}
			if got, _ := m["id"].(float64); int(got) != id {
				c.t.Fatalf("响应 id = %v, want %d", m["id"], id)
			}
			return m
		case <-time.After(15 * time.Second):
			c.t.Fatalf("等待 %s 响应超时", method)
		}
	}
}

func (c *core) result(method string, params any) map[string]any {
	c.t.Helper()
	m := c.call(method, params)
	res, ok := m["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s 应返回 result: %v", method, m)
	}
	return res
}

func (c *core) waitExit(within time.Duration) {
	c.t.Helper()
	select {
	case <-c.exited:
	case <-time.After(within):
		c.t.Fatalf("核心没有在 %s 内退出", within)
	}
}

func reachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestCore_InitWithForwardingOn(t *testing.T) {
	f := newFixture(t, true)
	c := startCore(t, f)

	st := c.result("init", f.initParams())
	proxy, _ := st["proxy"].(map[string]any)
	if proxy["running"] != true || proxy["error"] != nil {
		t.Fatalf("status = %v", st)
	}
	resp, err := http.Get("http://" + f.listenAddr + "/_health")
	if err != nil {
		t.Fatalf("转发服务不可达: %v", err)
	}
	_ = resp.Body.Close()

	cfg := f.config(t)
	catalogPath := filepath.Join(f.dataDir, "model-bridge-catalog.json")
	if !strings.Contains(cfg, proxyURLInConfig) || !strings.Contains(cfg, catalogPath) {
		t.Errorf("接入项没写入: %q", cfg)
	}
	if _, err := os.Stat(catalogPath); err != nil {
		t.Errorf("合并目录不存在: %v", err)
	}
}

func TestCore_StdinClosedExitsWithoutRestore(t *testing.T) {
	f := newFixture(t, true)
	c := startCore(t, f)
	c.result("init", f.initParams())
	taken := f.config(t)
	if taken == originalConfig {
		t.Fatal("init 后配置应已接入")
	}

	_ = c.in.Close()
	c.waitExit(5 * time.Second)

	if f.config(t) != taken {
		t.Errorf("父进程退出不该还原配置: %q", f.config(t))
	}
	if reachable(f.listenAddr) {
		t.Error("转发服务应已停止")
	}
}

func processInfo(t *testing.T, pid int) string {
	t.Helper()
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	info := string(out)
	if runtime.GOOS == "linux" {
		if env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ"); err == nil {
			info += "\n" + string(env)
		}
	} else if env, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid)).Output(); err == nil {
		info += "\n" + string(env)
	}
	return info
}

func TestCore_KeyNeverLeaks(t *testing.T) {
	f := newFixture(t, true)
	c := startCore(t, f)
	c.result("init", f.initParams())
	c.call("save_connection", map[string]any{"base_url": f.relayBase(), "api_key": otherKey})
	c.result("save_connection", map[string]any{"base_url": f.relayBase(), "api_key": secretKey})
	c.result("refresh_models", map[string]any{})
	resp, err := http.Post("http://"+f.listenAddr+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"deepseek-flash"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if info := processInfo(t, c.cmd.Process.Pid); strings.Contains(info, secretKey) || strings.Contains(info, otherKey) {
		t.Errorf("进程命令行或环境变量里出现了 Key: %q", info)
	}

	c.result("shutdown", map[string]any{"restore": false})
	c.waitExit(5 * time.Second)

	for _, k := range []string{secretKey, otherKey} {
		if strings.Contains(c.stdout.String(), k) || strings.Contains(c.stderr.String(), k) {
			t.Errorf("标准输出或标准错误里出现了 Key %q", k)
		}
	}
	entries, err := os.ReadDir(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(f.dataDir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{secretKey, otherKey} {
			if bytes.Contains(b, []byte(k)) {
				t.Errorf("数据目录文件 %s 里出现了 Key", ent.Name())
			}
		}
	}
}

func TestCore_Shutdown(t *testing.T) {
	for _, restore := range []bool{true, false} {
		t.Run("restore="+strconv.FormatBool(restore), func(t *testing.T) {
			f := newFixture(t, true)
			c := startCore(t, f)
			c.result("init", f.initParams())

			res := c.result("shutdown", map[string]any{"restore": restore})
			if len(res) != 0 {
				t.Errorf("result = %v, want {}", res)
			}
			c.waitExit(5 * time.Second)
			if restored := f.config(t) == originalConfig; restored != restore {
				t.Errorf("配置还原 = %v, want %v", restored, restore)
			}
			if reachable(f.listenAddr) {
				t.Error("转发服务应已停止")
			}
		})
	}
}

func TestCore_PrepareUninstall(t *testing.T) {
	t.Run("有手改项时保留数据目录文件", func(t *testing.T) {
		f := newFixture(t, true)
		c := startCore(t, f)
		c.result("init", f.initParams())
		cfgPath := filepath.Join(f.home, "config.toml")
		edited := strings.Replace(f.config(t), proxyURLInConfig, "http://proxy.example.org/v1", 1)
		if err := os.WriteFile(cfgPath, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}

		m := c.call("prepare_uninstall", map[string]any{})
		errObj, _ := m["error"].(map[string]any)
		if errObj["code"] != "restore_blocked" || !reflect.DeepEqual(errObj["keys"], []any{"openai_base_url"}) {
			t.Fatalf("响应 = %v", m)
		}
		for _, name := range []string{"model-bridge-catalog.json", "access-state.json", "relay-models.json", "settings.json"} {
			if _, err := os.Stat(filepath.Join(f.dataDir, name)); err != nil {
				t.Errorf("%s 应保留: %v", name, err)
			}
		}
	})

	t.Run("无手改项时还原并删除本工具文件", func(t *testing.T) {
		f := newFixture(t, true)
		c := startCore(t, f)
		c.result("init", f.initParams())

		c.result("prepare_uninstall", map[string]any{})
		if f.config(t) != originalConfig {
			t.Errorf("配置没还原: %q", f.config(t))
		}
		for _, name := range []string{"model-bridge-catalog.json", "access-state.json", "relay-models.json", "settings.json"} {
			if _, err := os.Stat(filepath.Join(f.dataDir, name)); !os.IsNotExist(err) {
				t.Errorf("%s 应被删除: %v", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(f.home, "config.toml.before-model-bridge")); err != nil {
			t.Errorf("备份应保留: %v", err)
		}
	})
}

func TestCore_SaveConnectionErrors(t *testing.T) {
	f := newFixture(t, false)
	c := startCore(t, f)
	c.result("init", f.initParams())

	tests := []struct {
		name     string
		baseURL  string
		key      string
		wantCode string
	}{
		{"地址不是 https", "http://relay.example.com/v1", secretKey, "invalid_base_url"},
		{"Key 被拒", f.relayBase(), otherKey, "relay_401"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := c.call("save_connection", map[string]any{"base_url": tt.baseURL, "api_key": tt.key})
			errObj, _ := m["error"].(map[string]any)
			if errObj["code"] != tt.wantCode {
				t.Fatalf("响应 = %v", m)
			}
			if _, has := errObj["keys"]; has {
				t.Errorf("非 restore_blocked 不该带 keys: %v", errObj)
			}
		})
	}
}

func TestCore_StdinClosedWhileRequestInFlight(t *testing.T) {
	f := newFixture(t, true)
	c := startCore(t, f)
	c.result("init", f.initParams())
	taken := f.config(t)

	f.hang.Store(true)
	line, _ := json.Marshal(map[string]any{"id": 99, "method": "refresh_models", "params": map[string]any{}})
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	_ = c.in.Close()
	c.waitExit(5 * time.Second)

	if f.config(t) != taken {
		t.Errorf("父进程退出不该还原配置: %q", f.config(t))
	}
	if reachable(f.listenAddr) {
		t.Error("转发服务应已停止")
	}
}

func TestReleaseBuild_HasNoTestFlags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "model-bridge-core")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build 失败: %v\n%s", err, out)
	}
	for _, name := range []string{"listen", "codex-path", "official-api-url", "official-chatgpt-url", "relay-ca-file"} {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command(bin, "--data-dir", t.TempDir(), "--"+name, "x").CombinedOutput()
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() == 0 {
				t.Fatalf("应以非零码退出: %v", err)
			}
			if !strings.Contains(string(out), "flag provided but not defined: -"+name) {
				t.Errorf("输出 = %q", out)
			}
		})
	}
}
