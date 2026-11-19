package catalog_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

const secretBody = "SECRET-BODY-MARKER-9f3a"

func fetchOnce(t *testing.T, url string, timeout time.Duration) *catalog.Result {
	t.Helper()
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	opts := buildOpts(t, codex, t.TempDir(), source("1", "主力", url))
	opts.RelayTimeout = timeout
	return build(t, opts)
}

func TestFetch_Success_RequestShapeAndModelsParsed(t *testing.T) {
	t.Parallel()
	var gotPath, gotAuth, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		_, _ = w.Write([]byte(modelsBody(idItems("auto", "deepseek-v3.2", "unknown-model-x")...)))
	}))
	defer srv.Close()

	res := fetchOnce(t, srv.URL+"/v1", time.Second)
	if gotMethod != http.MethodGet || gotPath != "/v1/models" || gotAuth != "Bearer "+relayKey {
		t.Errorf("请求不对: %s %s %q", gotMethod, gotPath, gotAuth)
	}
	if r := relayResult(t, res, "1"); r.Err != nil || r.ModelCount != 3 {
		t.Errorf("配置 1 = %+v", r)
	}
}

func TestFetch_ModelsWithoutIDAreSkipped(t *testing.T) {
	t.Parallel()
	srv := newRelayStub(t, 200, modelsBody(`{"id":""}`, `{"context_length":1000}`, `{"id":"model-a"}`))
	res := fetchOnce(t, srv.URL, time.Second)
	if got, want := slugs(t, res), []any{"gpt-a", "gpt-b", "relay-model-a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("slugs = %v, want %v", got, want)
	}
	if got := relayResult(t, res, "1").ModelCount; got != 1 {
		t.Errorf("ModelCount = %d, want 1", got)
	}
}

func TestFetch_StatusCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		code   string
	}{
		{401, "relay_401"},
		{403, "relay_403"},
		{400, "relay_4xx"},
		{404, "relay_404"},
		{418, "relay_4xx"},
		{429, "relay_429"},
		{500, "relay_5xx"},
		{502, "relay_5xx"},
		{503, "relay_5xx"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			srv := newRelayStub(t, tt.status, secretBody)
			r := relayResult(t, fetchOnce(t, srv.URL, time.Second), "1")
			if got := catalog.ErrorCode(r.Err); got != tt.code {
				t.Fatalf("code = %q (err=%v), want %q", got, r.Err, tt.code)
			}
			if strings.Contains(r.Err.Error(), secretBody) {
				t.Errorf("错误值里出现了中转返回正文")
			}
			if r.ModelCount != 0 {
				t.Errorf("ModelCount = %d, want 0", r.ModelCount)
			}
		})
	}
}

func TestFetch_Timeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(release)
	r := relayResult(t, fetchOnce(t, srv.URL, 100*time.Millisecond), "1")
	if got := catalog.ErrorCode(r.Err); got != "relay_unreachable" {
		t.Fatalf("code = %q (err=%v)", got, r.Err)
	}
}

func TestFetch_NetworkError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	r := relayResult(t, fetchOnce(t, url, time.Second), "1")
	if got := catalog.ErrorCode(r.Err); got != "relay_unreachable" {
		t.Fatalf("code = %q (err=%v)", got, r.Err)
	}
}

func TestFetch_ErrorDoesNotContainModelNames(t *testing.T) {
	t.Parallel()
	srv := newRelayStub(t, 500, modelsBody(`{"id":"deepseek-v3.2"}`))
	r := relayResult(t, fetchOnce(t, srv.URL, time.Second), "1")
	if r.Err == nil || strings.Contains(r.Err.Error(), "deepseek-v3.2") {
		t.Fatalf("err = %v", r.Err)
	}
}

func TestRelayTimeout_Is15Seconds(t *testing.T) {
	t.Parallel()
	if catalog.RelayTimeout != 15*time.Second {
		t.Errorf("RelayTimeout = %v", catalog.RelayTimeout)
	}
}

func TestFetch_InvalidBody(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"not json", `{"data":null}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			srv := newRelayStub(t, 200, body)
			r := relayResult(t, fetchOnce(t, srv.URL, time.Second), "1")
			if got := catalog.ErrorCode(r.Err); got != "relay_bad_response" {
				t.Fatalf("code = %q (err=%v)", got, r.Err)
			}
		})
	}
}

func TestFetch_BodyOver8MBRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"`))
		chunk := []byte(strings.Repeat("a", 64*1024))
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	start := time.Now()
	r := relayResult(t, fetchOnce(t, srv.URL, 5*time.Second), "1")
	if got := catalog.ErrorCode(r.Err); got != "relay_bad_response" {
		t.Fatalf("code = %q (err=%v)", got, r.Err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("超过 8MB 后应立即放弃读取")
	}
}

func TestFetch_FieldWithWrongTypeTreatedAsMissing(t *testing.T) {
	t.Parallel()
	srv := newRelayStub(t, 200, modelsBody(
		`{"id":"m-float","context_length":131072.0,"architecture":{"input_modalities":["text"]},"reasoning":{"supported_efforts":["low"]}}`,
		`{"id":"m-string","context_length":"131072","architecture":{"input_modalities":"text"},"reasoning":{"supported_efforts":"low"}}`,
		`{"id":"m-fraction","context_length":1.5,"architecture":"text","reasoning":["low"]}`,
	))
	codex, _ := writeFakeCodex(t, officialJSON, 0)
	dataDir := t.TempDir()
	res := build(t, buildOpts(t, codex, dataDir, source("1", "主力", srv.URL)))
	if r := relayResult(t, res, "1"); r.Err != nil || r.ModelCount != 3 {
		t.Fatalf("配置 1 = %+v", r)
	}
	models := catalogFileModels(t, dataDir)
	first := models[0]

	float := bySlug(t, models, "relay-m-float")
	if got := float["context_window"]; got != float64(131072) {
		t.Errorf("m-float context_window = %v, want 131072", got)
	}
	for _, slug := range []string{"relay-m-string", "relay-m-fraction"} {
		e := bySlug(t, models, slug)
		t.Run(slug, func(t *testing.T) {
			if e["context_window"] != first["context_window"] {
				t.Errorf("context_window = %v, want 官方第一个条目的 %v", e["context_window"], first["context_window"])
			}
			if !reflect.DeepEqual(e["input_modalities"], first["input_modalities"]) {
				t.Errorf("input_modalities = %v, want %v", e["input_modalities"], first["input_modalities"])
			}
			if got := efforts(t, e); len(got) != 0 {
				t.Errorf("等级 = %v, want 无", got)
			}
		})
	}
}
