package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"modelbridge/internal/catalog"
)

const secretBody = "SECRET-BODY-MARKER-9f3a"

func TestFetchRelayIDs_Success(t *testing.T) {
	t.Parallel()
	var gotPath, gotAuth, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotMethod = r.URL.Path, r.Header.Get("Authorization"), r.Method
		_, _ = w.Write([]byte(`{"data":[{"id":"auto"},{"id":"deepseek-v3.2"},{"id":"unknown-model-x"}]}`))
	}))
	defer srv.Close()

	ids, err := catalog.FetchRelayIDs(context.Background(), srv.URL+"/v1", "sk-example-0000", time.Second)
	if err != nil {
		t.Fatalf("FetchRelayIDs: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/models" || gotAuth != "Bearer sk-example-0000" {
		t.Errorf("请求不对: %s %s %q", gotMethod, gotPath, gotAuth)
	}
	want := []string{"auto", "deepseek-v3.2", "unknown-model-x"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v", ids)
	}
}

func TestFetchRelayIDs_StatusCodes(t *testing.T) {
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(secretBody))
			}))
			defer srv.Close()
			_, err := catalog.FetchRelayIDs(context.Background(), srv.URL, "sk-example-0000", time.Second)
			if got := catalog.ErrorCode(err); got != tt.code {
				t.Fatalf("code = %q (err=%v), want %q", got, err, tt.code)
			}
			if strings.Contains(err.Error(), secretBody) {
				t.Errorf("错误值里出现了中台返回正文")
			}
		})
	}
}

func TestFetchRelayIDs_Timeout(t *testing.T) {
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
	_, err := catalog.FetchRelayIDs(context.Background(), srv.URL, "sk-example-0000", 100*time.Millisecond)
	if got := catalog.ErrorCode(err); got != "relay_unreachable" {
		t.Fatalf("code = %q (err=%v)", got, err)
	}
}

func TestFetchRelayIDs_NetworkError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := catalog.FetchRelayIDs(context.Background(), url, "sk-example-0000", time.Second)
	if got := catalog.ErrorCode(err); got != "relay_unreachable" {
		t.Fatalf("code = %q (err=%v)", got, err)
	}
}

func TestFetchRelayIDs_ErrorDoesNotContainModelNames(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v3.2"}]}`))
	}))
	defer srv.Close()
	_, err := catalog.FetchRelayIDs(context.Background(), srv.URL, "sk-example-0000", time.Second)
	if err == nil || strings.Contains(err.Error(), "deepseek-v3.2") {
		t.Fatalf("err = %v", err)
	}
}

func TestRelayTimeout_Is15Seconds(t *testing.T) {
	t.Parallel()
	if catalog.RelayTimeout != 15*time.Second {
		t.Errorf("RelayTimeout = %v", catalog.RelayTimeout)
	}
}

func TestSelectRelay(t *testing.T) {
	t.Parallel()
	params := []catalog.Param{
		{ID: "auto", Source: "s"},
		{ID: "deepseek-v3.2", Source: "s"},
		{ID: "only-in-params", Source: "s"},
	}
	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{"取交集并保持中台顺序", []string{"deepseek-v3.2", "unknown-model-x", "auto"}, []string{"deepseek-v3.2", "auto"}},
		{"必须完全相等", []string{"Auto", "deepseek-v3.2-extra", "auto "}, nil},
		{"中台为空", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalog.SelectRelay(tt.ids, params)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d 条: %v", len(got), got)
			}
			for i, p := range got {
				if p.ID != tt.want[i] {
					t.Errorf("[%d] = %s, want %s", i, p.ID, tt.want[i])
				}
			}
		})
	}
}

func TestFetchRelayIDs_InvalidBody(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"not json", `{"data":null}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			_, err := catalog.FetchRelayIDs(context.Background(), srv.URL, "sk-example-0000", time.Second)
			if got := catalog.ErrorCode(err); got != "relay_404" {
				t.Fatalf("code = %q (err=%v)", got, err)
			}
		})
	}
}

func TestFetchRelayIDs_BodyOver8MBRejected(t *testing.T) {
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
	_, err := catalog.FetchRelayIDs(context.Background(), srv.URL, "sk-example-0000", 5*time.Second)
	if got := catalog.ErrorCode(err); got != "relay_404" {
		t.Fatalf("code = %q (err=%v)", got, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("超过 8MB 后应立即放弃读取")
	}
}
