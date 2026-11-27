package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modelbridge/internal/codexauth"
	"modelbridge/internal/control"
)

const (
	acctA  = "acct-example-aaa"
	acctB  = "acct-example-bbb"
	tokA   = "Bearer tok-example-aaa"
	tokB   = "Bearer tok-example-bbb"
	cacheV = `{"client_version":"0.130.0-alpha.2","models":[{"slug":"gpt-c","visibility":"list"},{"slug":"gpt-a","visibility":"list"}]}`
)

type modelsHit struct {
	auth, account, originator, userAgent, clientVersion string
}

type fakeOfficial struct {
	*httptest.Server
	mu        sync.Mutex
	lists     map[string]string
	code      int
	body      string
	abort     bool
	gate      chan struct{}
	hits      []modelsHit
	responses int
}

func modelsBody(slugs ...string) string {
	items := make([]string, 0, len(slugs))
	for _, s := range slugs {
		items = append(items, `{"slug":"`+s+`"}`)
	}
	return `{"models":[` + strings.Join(items, ",") + `]}`
}

func newFakeOfficial(t *testing.T) *fakeOfficial {
	t.Helper()
	o := &fakeOfficial{lists: map[string]string{
		acctA: modelsBody("gpt-a", "gpt-b", "ghost-not-in-full-set"),
		acctB: modelsBody("gpt-b", "gpt-c"),
	}}
	o.Server = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.Close)
	return o
}

func (o *fakeOfficial) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/responses") {
		_, _ = io.Copy(io.Discard, r.Body)
		o.mu.Lock()
		o.responses++
		o.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
		return
	}
	if r.URL.Path != "/backend-api/codex/models" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	account := r.Header.Get("ChatGPT-Account-ID")
	o.mu.Lock()
	o.hits = append(o.hits, modelsHit{
		auth: r.Header.Get("Authorization"), account: account,
		originator: r.Header.Get("originator"), userAgent: r.Header.Get("User-Agent"),
		clientVersion: r.URL.Query().Get("client_version"),
	})
	gate, abort, code, body := o.gate, o.abort, o.code, o.body
	if code == 0 {
		code, body = http.StatusOK, o.lists[account]
	}
	o.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if abort {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func (o *fakeOfficial) modelsHits() []modelsHit {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]modelsHit(nil), o.hits...)
}

func (o *fakeOfficial) setList(account string, slugs ...string) {
	o.mu.Lock()
	o.lists[account] = modelsBody(slugs...)
	o.mu.Unlock()
}

func (o *fakeOfficial) setOverride(code int, body string, abort bool) {
	o.mu.Lock()
	o.code, o.body, o.abort = code, body, abort
	o.mu.Unlock()
}

func (o *fakeOfficial) setGate(g chan struct{}) {
	o.mu.Lock()
	o.gate = g
	o.mu.Unlock()
}

type fakeNow struct {
	mu sync.Mutex
	d  time.Duration
}

func (c *fakeNow) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(c.d)
}

func (c *fakeNow) advance(d time.Duration) {
	c.mu.Lock()
	c.d += d
	c.mu.Unlock()
}

type acctEnv struct {
	*env
	official *fakeOfficial
	clock    *fakeNow
}

func newAcctEnv(t *testing.T, cache string, mods ...func(*env)) *acctEnv {
	t.Helper()
	o := newFakeOfficial(t)
	c := &fakeNow{}
	mods = append([]func(*env){
		withDetect(codexauth.LoggedIn),
		func(e *env) { e.codexPath = writeFakeCodex(t, threeOfficialJSON, 0) },
	}, mods...)
	e := newEnv(t, true, mods...)
	if cache != "" {
		writeModelsCache(t, e, cache)
	}
	e.opts.OfficialChatGPTURL = o.URL + "/backend-api/codex"
	e.opts.Now = c.Now
	e.svc = control.NewService(e.opts)
	return &acctEnv{env: e, official: o, clock: c}
}

func (e *acctEnv) restart(t *testing.T) {
	t.Helper()
	if err := e.svc.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	e.svc = control.NewService(e.opts)
}

func officialRequest(t *testing.T, addr, model, token, account string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/responses", strings.NewReader(`{"model":"`+model+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if account != "" {
		req.Header.Set("ChatGPT-Account-ID", account)
	}
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex-test/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func settle() { time.Sleep(300 * time.Millisecond) }

func revisionOf(t *testing.T, st control.Status) int {
	t.Helper()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["official_models_revision"].(float64)
	if !ok {
		t.Fatalf("status 里没有整数字段 official_models_revision: %s", raw)
	}
	return int(v)
}

func (e *acctEnv) revision(t *testing.T) int {
	t.Helper()
	return revisionOf(t, e.svc.Status())
}

func (e *acctEnv) pushedRevision(rev int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.updates {
		raw, _ := json.Marshal(s)
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil && m["official_models_revision"] == float64(rev) {
			return true
		}
	}
	return false
}

func (e *acctEnv) officialSlugs(t *testing.T) []any {
	t.Helper()
	var out []any
	for _, s := range catalogSlugs(catalogModels(t, e.env)) {
		if !strings.HasPrefix(s.(string), "relay-") {
			out = append(out, s)
		}
	}
	return out
}

func (e *acctEnv) waitIdle(t *testing.T) {
	t.Helper()
	e.svc.WaitAccountIdleForTest(t)
}

func (e *acctEnv) accountFile() string { return filepath.Join(e.dataDir, "account-models.json") }

func (e *acctEnv) waitHits(t *testing.T, n int) {
	t.Helper()
	waitUntil(t, "官方 /models 查询次数", func() bool { return len(e.official.modelsHits()) >= n })
}

func (e *acctEnv) waitRevision(t *testing.T, rev int) {
	t.Helper()
	waitUntil(t, "official_models_revision 变化", func() bool { return e.revision(t) == rev })
}

func (e *acctEnv) startBoth(t *testing.T) {
	t.Helper()
	e.initRelay(t)
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
		t.Fatalf("起始官方模型 = %v, want [gpt-a gpt-c]", got)
	}
}

func TestAccountModels_FirstRequestQueriesAndRebuildsCatalog(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	if got := e.revision(t); got != 0 {
		t.Fatalf("起始 revision = %d, want 0", got)
	}

	if code := officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA); code != http.StatusOK {
		t.Fatalf("原请求状态码 = %d, want 200", code)
	}
	e.waitHits(t, 1)
	e.waitRevision(t, 1)

	want := modelsHit{auth: tokA, account: acctA, originator: "codex_cli_rs", userAgent: "codex-test/1.0", clientVersion: "0.130.0"}
	if hits := e.official.modelsHits(); len(hits) != 1 || hits[0] != want {
		t.Errorf("查询 = %+v, want [%+v]（client_version 去掉预发布后缀）", hits, want)
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-b"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-b]（名单与自带清单的交集）", got)
	}
	waitUntil(t, "status 推送 revision=1", func() bool { return e.pushedRevision(1) })
}

func TestAccountModels_StoredListFile(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	var file map[string]any
	if err := json.Unmarshal([]byte(readFile(t, e.accountFile())), &file); err != nil {
		t.Fatal(err)
	}
	if len(file) != 1 {
		t.Fatalf("account-models.json 字段 = %v, want 只有 slugs", file)
	}
	slugs, _ := file["slugs"].([]any)
	for _, want := range []string{"gpt-a", "gpt-b"} {
		found := false
		for _, s := range slugs {
			found = found || s == want
		}
		if !found {
			t.Errorf("slugs = %v, 缺 %s", slugs, want)
		}
	}
}

func TestAccountModels_ClientVersionForms(t *testing.T) {
	tests := []struct {
		name, version, want string
	}{
		{"带预发布后缀", "0.130.0-alpha.2", "0.130.0"},
		{"已是主次修订", "1.2.3", "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, `{"client_version":"`+tt.version+`","models":[{"slug":"gpt-c"},{"slug":"gpt-a"}]}`)
			e.initRelay(t)
			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
			e.waitHits(t, 1)
			if got := e.official.modelsHits()[0].clientVersion; got != tt.want {
				t.Errorf("client_version = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccountModels_OriginalRequestForwardedNormally(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	gate := make(chan struct{})
	e.official.setGate(gate)

	if code := officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA); code != http.StatusOK {
		t.Fatalf("查询未完成时原请求状态码 = %d, want 200", code)
	}
	e.official.mu.Lock()
	n := e.official.responses
	e.official.mu.Unlock()
	if n != 1 {
		t.Errorf("官方 /responses 收到 %d 次, want 1", n)
	}
	e.waitHits(t, 1)
	close(gate)
	e.waitRevision(t, 1)
}

func TestAccountModels_WithinHourNoRequery_AfterHourRequeries(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	e.clock.advance(59 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitIdle(t)
	if n := len(e.official.modelsHits()); n != 1 {
		t.Fatalf("59 分钟内查询次数 = %d, want 1", n)
	}

	e.clock.advance(2 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
}

func TestAccountModels_SameSetKeepsCatalogAndRevision(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(e.catalogPath(), past, past); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, e.catalogPath())

	e.official.setList(acctA, "ghost-2", "gpt-b", "gpt-a")
	e.clock.advance(61 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
	e.waitIdle(t)

	if got := e.revision(t); got != 1 {
		t.Errorf("revision = %d, want 1（官方模型集合没变）", got)
	}
	info, err := os.Stat(e.catalogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) || readFile(t, e.catalogPath()) != before {
		t.Error("集合没变时合并目录文件不该改写")
	}
}

func TestAccountModels_AccountChangeQueriesImmediately(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	officialRequest(t, e.listenAddr, "gpt-a", tokB, acctB)
	e.waitHits(t, 2)
	e.waitRevision(t, 2)
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-b", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-b gpt-c]", got)
	}
	if hits := e.official.modelsHits(); hits[1].account != acctB || hits[1].auth != tokB {
		t.Errorf("第二次查询 = %+v, want 账号 B 的凭据", hits[1])
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 3)
}

func TestAccountModels_ConcurrentRequestsQueryOnce(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	gate := make(chan struct{})
	e.official.setGate(gate)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
		}()
	}
	wg.Wait()
	e.waitHits(t, 1)
	close(gate)
	e.waitIdle(t)
	if n := len(e.official.modelsHits()); n != 1 {
		t.Errorf("并发请求触发 %d 次查询, want 1", n)
	}
	e.waitRevision(t, 1)
}

func TestAccountModels_FailuresKeepEverything(t *testing.T) {
	tests := []struct {
		name  string
		code  int
		body  string
		abort bool
	}{
		{"500", http.StatusInternalServerError, `{"error":"x"}`, false},
		{"非 JSON", http.StatusOK, "not json", false},
		{"models 为空", http.StatusOK, `{"models":[]}`, false},
		{"交集为空", http.StatusOK, modelsBody("ghost-a", "ghost-b"), false},
		{"网络错误", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, cacheV)
			e.startBoth(t)
			before := readFile(t, e.catalogPath())
			e.official.setOverride(tt.code, tt.body, tt.abort)

			if code := officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA); code != http.StatusOK {
				t.Fatalf("原请求状态码 = %d, want 200", code)
			}
			e.waitHits(t, 1)
			e.waitIdle(t)

			if readFile(t, e.catalogPath()) != before {
				t.Error("合并目录不该变")
			}
			if exists(e.accountFile()) {
				t.Error("失败时不该写 account-models.json")
			}
			if got := e.revision(t); got != 0 {
				t.Errorf("revision = %d, want 0", got)
			}
		})
	}
}

func TestAccountModels_FailureCountsAsQueried(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	e.official.setOverride(http.StatusInternalServerError, "", false)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	e.waitIdle(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitIdle(t)
	if n := len(e.official.modelsHits()); n != 1 {
		t.Fatalf("失败后 1 小时内查询次数 = %d, want 1", n)
	}
	e.clock.advance(61 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
}

func (e *acctEnv) versionCodex(t *testing.T, versionOut string, versionExit int) {
	t.Helper()
	out := filepath.Join(filepath.Dir(e.codexPath), "out.json")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n"
	if versionOut != "" {
		script += "echo '" + versionOut + "'\n"
	}
	script += "exit " + strconv.Itoa(versionExit) + "\nfi\ncat '" + out + "'\n"
	if err := os.WriteFile(e.codexPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

const (
	cacheNoVersion = `{"models":[{"slug":"gpt-c"},{"slug":"gpt-a"}]}`
	codexVersion   = "codex-cli 0.171.0-alpha.13.2"
)

func TestAccountModels_NoVersionAnywhereNoQuery(t *testing.T) {
	tests := []struct {
		name        string
		cache       string
		versionOut  string
		versionExit int
	}{
		{"缓存缺 client_version，codex --version 失败", cacheNoVersion, "", 1},
		{"缓存文件不存在，codex --version 失败", "", "", 1},
		{"缓存缺 client_version，codex --version 没有输出", cacheNoVersion, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, tt.cache)
			e.versionCodex(t, tt.versionOut, tt.versionExit)
			e.initRelay(t)
			before := readFile(t, e.catalogPath())

			if code := officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA); code != http.StatusOK {
				t.Fatalf("原请求状态码 = %d, want 200", code)
			}
			e.waitIdle(t)
			if n := len(e.official.modelsHits()); n != 0 {
				t.Errorf("查询次数 = %d, want 0", n)
			}
			if readFile(t, e.catalogPath()) != before || exists(e.accountFile()) || e.revision(t) != 0 {
				t.Error("目录、名单文件、revision 都不该变")
			}
		})
	}
}

func TestAccountModels_VersionFromCodexWhenCacheHasNone(t *testing.T) {
	tests := []struct {
		name  string
		cache string
	}{
		{"缓存缺 client_version", cacheNoVersion},
		{"缓存文件不存在", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, tt.cache)
			e.versionCodex(t, codexVersion, 0)
			e.initRelay(t)

			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
			e.waitHits(t, 1)
			if got := e.official.modelsHits()[0].clientVersion; got != "0.171.0" {
				t.Errorf("client_version = %q, want 0.171.0（取 codex --version 最后一段并去掉预发布后缀）", got)
			}
		})
	}
}

func TestAccountModels_CacheVersionWinsOverCodexVersion(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.versionCodex(t, codexVersion, 0)
	e.initRelay(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	if got := e.official.modelsHits()[0].clientVersion; got != "0.130.0" {
		t.Errorf("client_version = %q, want 0.130.0", got)
	}
}

func TestAccountModels_NotQueried(t *testing.T) {
	t.Run("不带账号标识", func(t *testing.T) {
		e := newAcctEnv(t, cacheV)
		e.startBoth(t)
		officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
		e.waitIdle(t)
		if n := len(e.official.modelsHits()); n != 0 {
			t.Errorf("查询次数 = %d, want 0", n)
		}
	})

	t.Run("中转模型请求", func(t *testing.T) {
		e := newAcctEnv(t, cacheV)
		e.startBoth(t)
		if code := officialRequest(t, e.listenAddr, "relay-deepseek-flash", tokA, acctA); code != http.StatusOK {
			t.Fatalf("中转请求状态码 = %d", code)
		}
		e.waitIdle(t)
		if n := len(e.official.modelsHits()); n != 0 {
			t.Errorf("查询次数 = %d, want 0", n)
		}
		if e.revision(t) != 0 {
			t.Error("revision 不该变")
		}
	})

	t.Run("只用中转方式", func(t *testing.T) {
		e := newAcctEnv(t, cacheV)
		e.initRelay(t)
		if _, err := e.svc.SetRelayOnly(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
		officialRequest(t, e.listenAddr, "relay-deepseek-flash", tokA, acctA)
		e.waitIdle(t)
		if n := len(e.official.modelsHits()); n != 0 {
			t.Errorf("查询次数 = %d, want 0", n)
		}
	})
}

func TestAccountModels_RelayNeverSeesQuery(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	modelHitsBefore := e.relay.hits()
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	e.waitIdle(t)

	if auths := e.relay.forwardedAuths(); len(auths) != 0 {
		t.Errorf("中转收到请求: %v", auths)
	}
	if got := e.relay.hits(); got != modelHitsBefore {
		t.Errorf("中转 /models 次数 %d -> %d, 不该增加", modelHitsBefore, got)
	}
}

func TestAccountModels_CredentialsNotPersisted(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	e.waitIdle(t)

	secrets := []string{strings.TrimPrefix(tokA, "Bearer "), acctA}
	err := filepath.Walk(e.dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range secrets {
			if strings.Contains(string(b), s) {
				t.Errorf("%s 里出现了 %q", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccountModels_RebuildsUseStoredListWithoutRevisionChange(t *testing.T) {
	ctx := context.Background()
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-b"}) {
		t.Errorf("重开转发后官方模型 = %v, want [gpt-a gpt-b]（按存下的名单，不按 models_cache.json）", got)
	}
	if got := e.revision(t); got != 1 {
		t.Errorf("revision = %d, want 1（重建不改 revision）", got)
	}
}

func TestAccountModels_RestartUsesStoredListAndResetsRevision(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	hits := len(e.official.modelsHits())

	e.restart(t)
	e.initRelay(t)
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-b"}) {
		t.Errorf("重启后官方模型 = %v, want [gpt-a gpt-b]", got)
	}
	if got := e.revision(t); got != 0 {
		t.Errorf("重启后 revision = %d, want 0", got)
	}
	if n := len(e.official.modelsHits()); n != hits {
		t.Errorf("启动本身不该查询: %d -> %d", hits, n)
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, hits+1)
}

func TestAccountModels_PrepareUninstallRemovesList(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	if !exists(e.accountFile()) {
		t.Fatal("名单文件应已存在")
	}
	if err := e.svc.PrepareUninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists(e.accountFile()) {
		t.Error("PrepareUninstall 后 account-models.json 应被删除")
	}
}

func TestAccountModels_HangingQueryGivesUpAfterFiveSeconds(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	before := readFile(t, e.catalogPath())
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	e.official.setGate(gate)

	if code := officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA); code != http.StatusOK {
		t.Fatalf("原请求状态码 = %d, want 200", code)
	}
	e.waitHits(t, 1)
	begin := time.Now()
	e.waitIdle(t)
	if d := time.Since(begin); d < 4500*time.Millisecond || d > 6500*time.Millisecond {
		t.Errorf("挂住的查询放弃用了 %v, want 约 5 秒（4.5s 到 6.5s）", d)
	}
	release()

	if readFile(t, e.catalogPath()) != before {
		t.Error("合并目录不该变")
	}
	if exists(e.accountFile()) {
		t.Error("放弃后不该写 account-models.json")
	}
	if got := e.revision(t); got != 0 {
		t.Errorf("revision = %d, want 0（5 秒后应已放弃，不能在挂住的查询放开后才生效）", got)
	}

	e.clock.advance(30 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitIdle(t)
	if n := len(e.official.modelsHits()); n != 1 {
		t.Fatalf("放弃后 1 小时内查询次数 = %d, want 1", n)
	}
	e.clock.advance(31 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
}

func (e *acctEnv) slowCodex(t *testing.T) (started, finished string) {
	t.Helper()
	dir := filepath.Dir(e.codexPath)
	started, finished = filepath.Join(dir, "started"), filepath.Join(dir, "finished")
	script := "#!/bin/sh\ntouch '" + started + "'\nsleep 1\ntouch '" + finished + "'\ncat '" + filepath.Join(dir, "out.json") + "'\n"
	if err := os.WriteFile(e.codexPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return started, finished
}

func TestAccountModels_SameSetDoesNotBlockControlCommands(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	e.official.setList(acctA, "gpt-a", "gpt-c")
	started, finished := e.slowCodex(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	waitUntil(t, "后台查询启动 codex", func() bool { return exists(started) })

	begin := time.Now()
	e.svc.Status()
	if d := time.Since(begin); d > 500*time.Millisecond {
		t.Errorf("后台查询期间 Status 耗时 %v, want < 500ms", d)
	}
	waitUntil(t, "codex 结束", func() bool { return exists(finished) })
	e.waitIdle(t)
}

func TestAccountModels_ModeChangeDuringQueryDiscardsResult(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, e *acctEnv)
	}{
		{"关闭转发", func(t *testing.T, e *acctEnv) {
			if _, err := e.svc.SetForwarding(context.Background(), false); err != nil {
				t.Fatal(err)
			}
		}},
		{"只用中转", func(t *testing.T, e *acctEnv) {
			if _, err := e.svc.SetRelayOnly(context.Background(), true); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, cacheV)
			e.startBoth(t)
			e.official.setList(acctA, "gpt-a", "gpt-b")
			started, finished := e.slowCodex(t)

			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
			waitUntil(t, "后台查询启动 codex", func() bool { return exists(started) })
			tt.change(t, e)
			waitUntil(t, "codex 结束", func() bool { return exists(finished) })
			e.waitIdle(t)

			if exists(e.accountFile()) {
				t.Error("查询期间模式变了，不该写 account-models.json")
			}
			if got := e.revision(t); got != 0 {
				t.Errorf("revision = %d, want 0", got)
			}
		})
	}
}

type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return nil, errors.New("blocked in test")
}

func TestAccountModels_EmptyOfficialURLQueriesDefaultAddress(t *testing.T) {
	rec := &recordingTransport{}
	orig := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = orig })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cache := `{"client_version":"0.130.0-alpha.2","models":[]}`
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	s := control.NewService(control.Options{DataDir: filepath.Join(dir, "data"), ConfigPath: configPath})

	h := http.Header{}
	h.Set("Authorization", "Bearer tok-example-aaa")
	h.Set("ChatGPT-Account-ID", "acct-example-aaa")
	control.SyncAccountModels(s, h)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := "https://chatgpt.com/backend-api/codex/models?client_version=0.130.0"
	if len(rec.urls) != 1 || rec.urls[0] != want {
		t.Errorf("查询地址 = %v, want [%s]", rec.urls, want)
	}
}

func TestAccountModels_ChangedListDoesNotBlockControlCommands(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	started, _ := e.slowCodex(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	waitUntil(t, "后台查询启动 codex", func() bool { return exists(started) })

	var slowest time.Duration
	waitUntil(t, "official_models_revision 变为 1", func() bool {
		begin := time.Now()
		st := e.svc.Status()
		slowest = max(slowest, time.Since(begin))
		return revisionOf(t, st) == 1
	})
	if slowest > 500*time.Millisecond {
		t.Errorf("名单变了的后台处理期间 Status 最慢 %v, want < 500ms", slowest)
	}
}

func TestAccountModels_StoredListWithoutIntersectionFallsBackToModelsCache(t *testing.T) {
	tests := []struct {
		name   string
		action func(t *testing.T, e *acctEnv)
	}{
		{"启动转发", func(t *testing.T, e *acctEnv) { e.initRelay(t) }},
		{"重开转发", func(t *testing.T, e *acctEnv) {
			e.initRelay(t)
			ctx := context.Background()
			if _, err := e.svc.SetForwarding(ctx, false); err != nil {
				t.Fatal(err)
			}
			if _, err := e.svc.SetForwarding(ctx, true); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func(stored string) (slugs []any, relayHits int) {
				e := newAcctEnv(t, cacheV)
				if stored != "" {
					if err := os.WriteFile(e.accountFile(), []byte(stored), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				tt.action(t, e)
				return e.officialSlugs(t), e.relay.hits()
			}
			_, baseHits := run("")
			got, hits := run(`{"slugs":["ghost-x","ghost-y"]}`)

			if want := []any{"gpt-a", "gpt-c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("官方模型 = %v, want %v（按 models_cache.json 过滤）", got, want)
			}
			if hits != baseHits {
				t.Errorf("中转 /models 请求 %d 次, want %d（不重复请求）", hits, baseHits)
			}
		})
	}
}

func TestAccountModels_RequestWithoutAccountDropsStoredList(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	hits := len(e.official.modelsHits())

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "account-models.json 被删除", func() bool { return !exists(e.accountFile()) })
	waitUntil(t, "官方模型回到按 models_cache.json 过滤", func() bool {
		return reflect.DeepEqual(e.officialSlugs(t), []any{"gpt-a", "gpt-c"})
	})
	e.waitIdle(t)
	if n := len(e.official.modelsHits()); n != hits {
		t.Errorf("官方 /models 查询 %d -> %d, 不该再查", hits, n)
	}
	if got := e.revision(t); got != 1 {
		t.Errorf("revision = %d, want 1", got)
	}
}

func TestAccountModels_RequestWithoutAccountAndNoStoredListDoesNothing(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	before := readFile(t, e.catalogPath())
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(e.catalogPath(), past, past); err != nil {
		t.Fatal(err)
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	e.waitIdle(t)

	info, err := os.Stat(e.catalogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) || readFile(t, e.catalogPath()) != before {
		t.Error("没存过名单时合并目录不该改写")
	}
	if exists(e.accountFile()) || e.revision(t) != 0 || len(e.official.modelsHits()) != 0 {
		t.Error("名单文件、revision、官方查询都不该有变化")
	}
}

func TestAccountModels_OtherAccountDuringQueryIsQueriedAfterwards(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	gate := make(chan struct{})
	e.official.setGate(gate)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	officialRequest(t, e.listenAddr, "gpt-a", tokB, acctB)
	settle()
	if n := len(e.official.modelsHits()); n != 1 {
		t.Fatalf("账号 A 查询进行中时查询次数 = %d, want 1（同一时间只有一个查询）", n)
	}

	close(gate)
	e.waitHits(t, 2)
	if hits := e.official.modelsHits(); hits[1].account != acctB || hits[1].auth != tokB {
		t.Errorf("第二次查询 = %+v, want 账号 B 的凭据", hits[1])
	}
	e.waitRevision(t, 2)
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-b", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-b gpt-c]", got)
	}
}

func TestAccountModels_RollbackRestoresOfficialSet(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要用目录只读制造写配置失败")
	}
	tests := []struct {
		name         string
		list         []string
		wantRevision int
	}{
		{"与失败前集合相同的名单不重建", []string{"gpt-a", "gpt-c"}, 0},
		{"与失败那次重建的集合相同的名单要重建", []string{"gpt-b", "gpt-c"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, cacheV)
			e.startBoth(t)
			if err := os.WriteFile(e.accountFile(), []byte(`{"slugs":["gpt-b","gpt-c"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(e.configPath)
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if _, err := e.svc.RefreshModels(context.Background(), nil); err == nil {
				t.Fatal("前置条件：配置目录只读时 RefreshModels 应失败并回滚")
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
				t.Fatalf("前置条件：回滚后官方模型 = %v, want [gpt-a gpt-c]", got)
			}

			e.official.setList(acctA, append(tt.list, "ghost-not-in-full-set")...)
			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
			e.waitHits(t, 1)
			if tt.wantRevision == 1 {
				e.waitRevision(t, 1)
			} else {
				e.waitIdle(t)
			}
			if got := e.revision(t); got != tt.wantRevision {
				t.Errorf("revision = %d, want %d", got, tt.wantRevision)
			}
		})
	}
}

func TestAccountModels_QueuedQuerySkippedWhenModeChangesBeforeStart(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, e *acctEnv)
	}{
		{"关闭转发", func(t *testing.T, e *acctEnv) {
			if _, err := e.svc.SetForwarding(context.Background(), false); err != nil {
				t.Fatal(err)
			}
		}},
		{"只用中转", func(t *testing.T, e *acctEnv) {
			if _, err := e.svc.SetRelayOnly(context.Background(), true); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAcctEnv(t, cacheV)
			e.startBoth(t)
			gate := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(release)
			e.official.setGate(gate)

			officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
			e.waitHits(t, 1)
			officialRequest(t, e.listenAddr, "gpt-a", tokB, acctB)
			tt.change(t, e)
			release()
			e.waitIdle(t)

			for _, h := range e.official.modelsHits() {
				if h.account == acctB {
					t.Errorf("模式变了之后不该再查账号 B: %+v", h)
				}
			}
			if n := len(e.official.modelsHits()); n != 1 {
				t.Errorf("官方 /models 查询次数 = %d, want 1", n)
			}
		})
	}
}

func TestAccountModels_FailedDropOfStoredListNotRetriedWithinHour(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	counter := filepath.Join(filepath.Dir(e.codexPath), "calls")
	script := "#!/bin/sh\necho x >> '" + counter + "'\nexit 1\n"
	if err := os.WriteFile(e.codexPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := func() int {
		b, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(b), "\n")
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "第一次删除名单尝试调用 codex", func() bool { return calls() >= 1 })
	e.waitIdle(t)
	first := calls()

	for range 3 {
		officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	}
	e.waitIdle(t)
	if n := calls(); n != first {
		t.Fatalf("1 小时内 codex 被调用 %d -> %d 次, 不该反复尝试", first, n)
	}

	e.clock.advance(61 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "61 分钟后再试一次", func() bool { return calls() > first })
}

func TestAccountModels_SuccessfulDropOfStoredListNotRateLimited(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "第一次删除名单", func() bool { return !exists(e.accountFile()) })

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
	waitUntil(t, "账号 A 再次存下名单", func() bool { return exists(e.accountFile()) })

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "同一小时内第二次删除名单", func() bool { return !exists(e.accountFile()) })
	waitUntil(t, "官方模型按 models_cache.json 过滤", func() bool {
		return reflect.DeepEqual(e.officialSlugs(t), []any{"gpt-a", "gpt-c"})
	})
}

type codexRuns struct{ log string }

func (e *acctEnv) loggedSlowCodex(t *testing.T) codexRuns {
	t.Helper()
	dir := filepath.Dir(e.codexPath)
	log := filepath.Join(dir, "runs.log")
	script := "#!/bin/sh\necho start >> '" + log + "'\nsleep 1\necho end >> '" + log + "'\ncat '" + filepath.Join(dir, "out.json") + "'\n"
	if err := os.WriteFile(e.codexPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return codexRuns{log: log}
}

func (r codexRuns) events() []string {
	b, err := os.ReadFile(r.log)
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

func (r codexRuns) started() int {
	n := 0
	for _, ev := range r.events() {
		if ev == "start" {
			n++
		}
	}
	return n
}

func (r codexRuns) running() bool {
	depth := 0
	for _, ev := range r.events() {
		if ev == "start" {
			depth++
		} else {
			depth--
		}
	}
	return depth > 0
}

func (r codexRuns) maxConcurrent() int {
	depth, peak := 0, 0
	for _, ev := range r.events() {
		if ev == "start" {
			depth++
		} else {
			depth--
		}
		peak = max(peak, depth)
	}
	return peak
}

func TestAccountModels_LatestRequestWins_AccountRequestAfterDrop(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	runs := e.loggedSlowCodex(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "删名单的后台处理在跑 codex", func() bool { return runs.running() })
	hits := len(e.official.modelsHits())
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	settle()
	if n := len(e.official.modelsHits()); n != hits {
		t.Errorf("删名单还在跑 codex 时官方 /models 查询 %d -> %d, 同一时间只能做一件事", hits, n)
	}

	e.waitHits(t, hits+1)
	waitUntil(t, "后台处理全部结束", func() bool { return runs.started() >= 2 && !runs.running() })
	e.waitIdle(t)

	if runs.running() {
		t.Error("最后到的账号 A 请求之后仍有后台处理在跑")
	}
	if !exists(e.accountFile()) {
		t.Error("最后到的是账号 A 的请求，account-models.json 应存在")
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-b"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-b]（按账号 A 的名单）", got)
	}
	if got := runs.maxConcurrent(); got != 1 {
		t.Errorf("codex 同时运行 %d 份, want 1", got)
	}
}

func TestAccountModels_LatestRequestWins_DropRequestAfterAccountQuery(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	runs := e.loggedSlowCodex(t)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	e.official.setGate(gate)

	e.clock.advance(61 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 2)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	settle()
	if runs.started() != 0 {
		t.Error("账号 A 的查询还挂在官方 /models 上时，不该同时开始处理删名单")
	}
	release()

	waitUntil(t, "名单被删除", func() bool { return !exists(e.accountFile()) })
	waitUntil(t, "官方模型按 models_cache.json 过滤", func() bool {
		return reflect.DeepEqual(e.officialSlugs(t), []any{"gpt-a", "gpt-c"})
	})
	waitUntil(t, "后台处理全部结束", func() bool { return !runs.running() })
	e.waitIdle(t)

	if exists(e.accountFile()) {
		t.Error("最后到的是不带账号标识的请求，名单应保持删除")
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-c]", got)
	}
	if got := runs.maxConcurrent(); got > 1 {
		t.Errorf("codex 同时运行 %d 份, want 1", got)
	}
}

func TestAccountModels_LatestRequestWins_DropAfterFirstQueryNeverStored(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	e.official.setGate(gate)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	release()
	e.waitIdle(t)

	if exists(e.accountFile()) {
		t.Error("最后到的是不带账号标识的请求，不该留下名单")
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-c]（按 models_cache.json 过滤）", got)
	}
}

func TestAccountModels_LatestRequestWins_DropRunningThenAccountThenDrop(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	runs := e.loggedSlowCodex(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "删名单的后台处理在跑 codex", func() bool { return runs.running() })
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	e.waitIdle(t)

	if exists(e.accountFile()) {
		t.Error("最后到的是不带账号标识的请求，不该留下名单")
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-c]（按 models_cache.json 过滤）", got)
	}
}

func TestAccountModels_SameAccountDuringStartOfQueuedWorkClearsPending(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	runs := e.loggedSlowCodex(t)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	e.official.setGate(gate)
	e.official.setOverride(http.StatusInternalServerError, "", false)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := e.svc.RefreshModels(context.Background(), nil); err != nil {
			t.Errorf("RefreshModels: %v", err)
		}
	}()
	waitUntil(t, "RefreshModels 在跑 codex 并占着控制锁", func() bool { return runs.running() })
	release()
	settle()
	e.official.setOverride(0, "", false)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	wg.Wait()
	e.waitIdle(t)

	if hits := e.official.modelsHits(); len(hits) != 1 || hits[0].account != acctA {
		t.Errorf("官方 /models 查询 = %+v, want 只有账号 A 的 1 次（A 刚查过，失败也按 1 小时算）", hits)
	}
	if exists(e.accountFile()) {
		t.Error("没有新的查询成功，不该存下名单")
	}
	if got := e.officialSlugs(t); !reflect.DeepEqual(got, []any{"gpt-a", "gpt-c"}) {
		t.Errorf("官方模型 = %v, want [gpt-a gpt-c]", got)
	}
}

func TestAccountModels_ForwardingClosedDuringDropLeavesNoThrottle(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	runs := e.loggedSlowCodex(t)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "删名单的后台处理在跑 codex", func() bool { return runs.running() })
	ctx := context.Background()
	if _, err := e.svc.SetForwarding(ctx, false); err != nil {
		t.Fatal(err)
	}
	e.waitIdle(t)
	if _, err := e.svc.SetForwarding(ctx, true); err != nil {
		t.Fatal(err)
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "重新打开转发后名单被删除", func() bool { return !exists(e.accountFile()) })
	waitUntil(t, "官方模型按 models_cache.json 过滤", func() bool {
		return reflect.DeepEqual(e.officialSlugs(t), []any{"gpt-a", "gpt-c"})
	})
}

func TestAccountModels_DropRebuildFailureRestoresListAndRetriesAfterThrottle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用目录占住目录文件路径制造写盘失败")
	}
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitRevision(t, 1)
	listBefore := readFile(t, e.accountFile())

	if err := os.Remove(e.catalogPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.catalogPath(), "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	e.waitIdle(t)
	if !exists(e.accountFile()) {
		t.Fatal("重建失败后名单文件应被恢复")
	}
	if got := readFile(t, e.accountFile()); got != listBefore {
		t.Errorf("名单内容 = %q, want 删除前的 %q", got, listBefore)
	}

	if err := os.RemoveAll(e.catalogPath()); err != nil {
		t.Fatal(err)
	}
	e.clock.advance(61 * time.Minute)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, "")
	waitUntil(t, "节流过后再试并删除名单", func() bool { return !exists(e.accountFile()) })
	waitUntil(t, "官方模型按 models_cache.json 过滤", func() bool {
		return reflect.DeepEqual(e.officialSlugs(t), []any{"gpt-a", "gpt-c"})
	})
}

func TestAccountModels_PendingSameAccountAsJustQueriedIsNotQueriedAgain(t *testing.T) {
	e := newAcctEnv(t, cacheV)
	e.startBoth(t)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	e.official.setGate(gate)

	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	e.waitHits(t, 1)
	officialRequest(t, e.listenAddr, "gpt-a", tokB, acctB)
	officialRequest(t, e.listenAddr, "gpt-a", tokA, acctA)
	release()
	e.waitRevision(t, 1)
	e.waitIdle(t)

	hits := e.official.modelsHits()
	if len(hits) != 1 || hits[0].account != acctA {
		t.Errorf("官方 /models 查询 = %+v, want 只有账号 A 的 1 次", hits)
	}
}
