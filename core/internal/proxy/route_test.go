package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoute_Health(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	resp, body := e.do(t, http.MethodGet, "/_health", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil || got["ok"] != true || len(got) != 1 {
		t.Fatalf("响应 = %s", body)
	}
	if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
		t.Fatalf("健康检查不应转发，实际 %d 次", n)
	}
}

func TestRoute_RelayModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		model    string
		path     string
		wantPath string
	}{
		{"responses", relayModel, "/v1/responses", "/v1/responses"},
		{"多级路径", "relay-model-b", "/v1/responses/input_tokens", "/v1/responses/input_tokens"},
		{"chat completions", relayModel, "/v1/chat/completions", "/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			resp, body := e.do(t, http.MethodPost, tt.path, nil, []byte(`{"model":"`+tt.model+`"}`))
			if resp.StatusCode != http.StatusOK || string(body) != upstreamReply {
				t.Fatalf("响应 = %d %s", resp.StatusCode, body)
			}
			hit := e.relay.onlyHit(t)
			wantPath := strings.TrimPrefix(tt.path, "/v1")
			if hit.Path != "/v1"+wantPath {
				t.Fatalf("中台收到的路径 = %q，期望 Base URL(/v1) + %q", hit.Path, wantPath)
			}
			if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("中台模型不应发往官方，实际 %d 次", n)
			}
		})
	}
}

func TestRoute_RelayHeaders_OnlyAllowlistPlusAuthorization(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	hdr := map[string]string{
		"Accept":             "text/event-stream",
		"Content-Type":       "application/json",
		"OpenAI-Beta":        "responses=experimental",
		"User-Agent":         "agent-marker-0000",
		"Authorization":      officialKey,
		"Cookie":             cookieValue,
		"ChatGPT-Account-ID": accountID,
		"X-Custom":           "custom-marker-0000",
		"Originator":         "originator-marker-0000",
	}
	e.do(t, http.MethodPost, "/v1/responses", hdr, []byte(`{"model":"`+relayModel+`"}`))
	got := e.relay.onlyHit(t).Header

	wantValues := map[string]string{
		"Accept":        "text/event-stream",
		"Content-Type":  "application/json",
		"Openai-Beta":   "responses=experimental",
		"User-Agent":    "agent-marker-0000",
		"Authorization": "Bearer " + relayKey,
	}
	for k, v := range wantValues {
		if got.Get(k) != v {
			t.Errorf("中台收到的 %s = %q，期望 %q", k, got.Get(k), v)
		}
	}
	transportOnly := map[string]bool{"Accept-Encoding": true, "Content-Length": true}
	for k := range got {
		if _, ok := wantValues[http.CanonicalHeaderKey(k)]; !ok && !transportOnly[k] {
			t.Errorf("中台收到了不该有的请求头 %s", k)
		}
	}
	for k, vs := range got {
		for _, v := range vs {
			for _, secret := range []string{cookieValue, accountID, "sk-official-0000", "custom-marker-0000", "originator-marker-0000"} {
				if strings.Contains(v, secret) {
					t.Errorf("请求头 %s 把 %q 带到了中台", k, secret)
				}
			}
		}
	}
}

func TestRoute_Official(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"official-model","input":"hello"}`)
	tests := []struct {
		name         string
		path         string
		withAccount  bool
		wantChatGPT  bool
		wantPathSufx string
	}{
		{"带账号头走 chatgpt", "/v1/responses", true, true, "/backend-api/codex/responses"},
		{"不带账号头走 api", "/v1/responses", false, false, "/v1/responses"},
		{"chatgpt 多级路径", "/v1/responses/compact", true, true, "/backend-api/codex/responses/compact"},
		{"api 模型列表", "/v1/models", false, false, "/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			hdr := map[string]string{
				"Authorization": officialKey,
				"Cookie":        cookieValue,
				"Content-Type":  "application/json",
				"OpenAI-Beta":   "responses=experimental",
				"User-Agent":    "agent-marker-0000",
				"X-Custom":      "custom-marker-0000",
			}
			if tt.withAccount {
				hdr["ChatGPT-Account-ID"] = accountID
			}
			method, body := http.MethodPost, body
			if tt.path == "/v1/models" {
				method, body = http.MethodGet, nil
			}
			resp, got := e.do(t, method, tt.path, hdr, body)
			if resp.StatusCode != http.StatusOK || string(got) != upstreamReply {
				t.Fatalf("响应 = %d %s", resp.StatusCode, got)
			}

			target, other := e.api, e.chatgpt
			if tt.wantChatGPT {
				target, other = e.chatgpt, e.api
			}
			hit := target.onlyHit(t)
			if hit.Path != tt.wantPathSufx {
				t.Errorf("路径 = %q，期望 %q", hit.Path, tt.wantPathSufx)
			}
			for k, v := range hdr {
				if hit.Header.Get(k) != v {
					t.Errorf("请求头 %s = %q，期望原样 %q", k, hit.Header.Get(k), v)
				}
			}
			if string(hit.Body) != string(body) {
				t.Errorf("请求体 = %q，期望 %q", hit.Body, body)
			}
			if n := len(other.Hits()) + len(e.relay.Hits()); n != 0 {
				t.Errorf("不该收到请求的上游收到了 %d 次", n)
			}
		})
	}
}

func TestRoute_UnrecognizedModelGoesOfficial(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{"模型不在中台集合", http.MethodPost, `{"model":"official-model"}`},
		{"没有 model 字段", http.MethodPost, `{"input":"x"}`},
		{"不是合法 JSON", http.MethodPost, `not-json`},
		{"空请求体", http.MethodGet, ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			e.do(t, tt.method, "/v1/models", nil, []byte(tt.body))
			e.api.onlyHit(t)
			if n := len(e.relay.Hits()); n != 0 {
				t.Fatalf("中台不应收到请求，实际 %d 次", n)
			}
		})
	}
}

func TestRoute_WebSocketUpgrade_426(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		connection string
		upgrade    string
	}{
		{"标准写法", "Upgrade", "websocket"},
		{"小写", "upgrade", "WebSocket"},
		{"Connection 多值", "keep-alive, Upgrade", "websocket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			req.RemoteAddr = "127.0.0.1:50000"
			req.Host = "127.0.0.1:47311"
			req.Header.Set("Connection", tt.connection)
			req.Header.Set("Upgrade", tt.upgrade)
			rec := httptest.NewRecorder()
			e.srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUpgradeRequired {
				t.Fatalf("状态码 = %d，期望 426", rec.Code)
			}
			if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("升级请求不应转发，实际 %d 次", n)
			}
		})
	}
}

func TestRoute_NotV1Paths_404(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/foo", "/v1x", "/v1/../admin/keys", "/v1/%2e%2e/admin"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			resp, _ := e.do(t, http.MethodPost, path, nil, []byte(`{"model":"`+relayModel+`"}`))
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("状态码 = %d，期望 404", resp.StatusCode)
			}
			if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("不应转发，实际 %d 次", n)
			}
		})
	}
}
