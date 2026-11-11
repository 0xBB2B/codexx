package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

func relayServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func buildOpts(t *testing.T, codex, relayURL, dataDir string) catalog.BuildOptions {
	t.Helper()
	return catalog.BuildOptions{
		CodexPath:    codex,
		RelayBaseURL: relayURL,
		RelayKey:     "sk-example-0000",
		RelayTimeout: time.Second,
		DataDir:      dataDir,
		Params:       mergeParams,
	}
}

func slugs(t *testing.T, res *catalog.Result) []any {
	t.Helper()
	var out []any
	for _, m := range roundtrip(t, res.Models) {
		out = append(out, m["slug"])
	}
	return out
}

func TestBuild_RelaySuccess_MergesSavesRecordAndWritesCatalog(t *testing.T) {
	origHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(origHome, "auth.json"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", origHome)

	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 200, `{"data":[{"id":"auto"},{"id":"deepseek-v3.2"},{"id":"unknown-model-x"}]}`)
	dataDir := t.TempDir()

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.RelayErr != nil {
		t.Errorf("RelayErr = %v", res.RelayErr)
	}
	want := []any{"gpt-a", "gpt-b", "auto", "deepseek-v3.2"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	m := roundtrip(t, res.Models)
	if m[2]["priority"] != float64(8) || m[3]["priority"] != float64(9) {
		t.Errorf("priority = %v, %v", m[2]["priority"], m[3]["priority"])
	}

	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
	ids, _, ok, err := catalog.NewStore(dataDir).LoadRelayModels()
	if err != nil || !ok {
		t.Fatalf("上次成功记录没写出: %v %v", ok, err)
	}
	for _, id := range []string{"auto", "deepseek-v3.2"} {
		found := false
		for _, g := range ids {
			if g == id {
				found = true
			}
		}
		if !found {
			t.Errorf("记录里缺 %s: %v", id, ids)
		}
	}
}

func TestBuild_RelayFails_UsesLastSuccessfulRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := catalog.NewStore(dataDir).SaveRelayModels([]string{"deepseek-v3.2", "unknown-model-x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, secretBody)

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_5xx" {
		t.Errorf("RelayErr code = %q", got)
	}
	want := []any{"gpt-a", "gpt-b", "deepseek-v3.2"}
	if got := slugs(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	ids, _, _, _ := catalog.NewStore(dataDir).LoadRelayModels()
	if !reflect.DeepEqual(ids, []string{"deepseek-v3.2", "unknown-model-x"}) {
		t.Errorf("失败时不应覆盖上次记录: %v", ids)
	}
}

func TestBuild_RelayFails_NoRecord_OnlyOfficial(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	srv := relayServer(t, 401, "")

	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_401" {
		t.Errorf("RelayErr code = %q", got)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "relay-models.json")); !os.IsNotExist(err) {
		t.Errorf("从未成功过不应产生记录文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_OfficialErrors(t *testing.T) {
	srv := relayServer(t, 200, `{"data":[]}`)
	t.Run("codex 不存在", func(t *testing.T) {
		_, err := catalog.Build(context.Background(), buildOpts(t, filepath.Join(t.TempDir(), "nope"), srv.URL, t.TempDir()))
		if got := catalog.ErrorCode(err); got != "chatgpt_not_found" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
	})
	t.Run("输出非法 JSON", func(t *testing.T) {
		codex, _ := writeFakeCodex(t, "not json", 0)
		dataDir := t.TempDir()
		_, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
		if got := catalog.ErrorCode(err); got != "chatgpt_unsupported" {
			t.Errorf("code = %q (err=%v)", got, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); !os.IsNotExist(err) {
			t.Errorf("官方清单失败时不应写出合并目录")
		}
	})
}

func TestBuild_RelaySuccess_FillsFetchedAt(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 200, `{"data":[{"id":"auto"}]}`)
	before := time.Now().Add(-time.Second)
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if res.RelayFetchedAt.Before(before) || res.RelayFetchedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
}

func TestBuild_RelayFails_FetchedAtFromRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := catalog.NewStore(dataDir).SaveRelayModels([]string{"auto"}, at); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if !res.RelayFetchedAt.Equal(at) {
		t.Errorf("RelayFetchedAt = %v, want %v", res.RelayFetchedAt, at)
	}
}

func TestBuild_RelayFails_NoRecord_FetchedAtZero(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if !res.RelayFetchedAt.IsZero() {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
}

func TestBuild_RelayFails_CorruptRecord_TreatedAsNoRecord(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "relay-models.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 500, "")
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := catalog.ErrorCode(res.RelayErr); got != "relay_5xx" {
		t.Errorf("RelayErr code = %q", got)
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_RelaySuccess_SaveFails_StillWritesCatalog(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "relay-models.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := relayServer(t, 200, `{"data":[{"id":"auto"}]}`)
	res, err := catalog.Build(context.Background(), buildOpts(t, codex, srv.URL, dataDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.StoreErr == nil {
		t.Error("StoreErr 应不为空")
	}
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "auto"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "model-bridge-catalog.json")); err != nil {
		t.Errorf("合并目录没写出: %v", err)
	}
}

func TestBuild_PrefetchedRelayIDs_SkipsRequest(t *testing.T) {
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	dataDir := t.TempDir()
	opts := buildOpts(t, codex, srv.URL, dataDir)
	opts.RelayIDs = []string{"auto"}

	before := time.Now().Add(-time.Second)
	res, err := catalog.Build(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 || res.RelayErr != nil {
		t.Errorf("hits = %d, RelayErr = %v", hits, res.RelayErr)
	}
	if res.RelayFetchedAt.Before(before) {
		t.Errorf("RelayFetchedAt = %v", res.RelayFetchedAt)
	}
	if !reflect.DeepEqual(slugs(t, res)[len(slugs(t, res))-1], "auto") {
		t.Errorf("slugs = %v", slugs(t, res))
	}
	if !strings.Contains(readFileT(t, filepath.Join(dataDir, "model-bridge-catalog.json")), `"auto"`) {
		t.Error("合并目录里没有传入的模型")
	}
	if !strings.Contains(readFileT(t, filepath.Join(dataDir, "relay-models.json")), `"auto"`) {
		t.Error("没有保存 relay-models.json")
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
