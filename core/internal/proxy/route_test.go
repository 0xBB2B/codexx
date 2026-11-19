package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"modelbridge/internal/proxy"
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
			var sent map[string]any
			if err := json.Unmarshal(hit.Body, &sent); err != nil || sent["model"] != strings.TrimPrefix(tt.model, "relay-") {
				t.Fatalf("中转收到的请求体 = %q，model 应是原名", hit.Body)
			}
			wantPath := strings.TrimPrefix(tt.path, "/v1")
			if hit.Path != "/v1"+wantPath {
				t.Fatalf("中转收到的路径 = %q，期望 Base URL(/v1) + %q", hit.Path, wantPath)
			}
			if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("中转模型不应发往官方，实际 %d 次", n)
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
			t.Errorf("中转收到的 %s = %q，期望 %q", k, got.Get(k), v)
		}
	}
	transportOnly := map[string]bool{"Accept-Encoding": true, "Content-Length": true}
	for k := range got {
		if _, ok := wantValues[http.CanonicalHeaderKey(k)]; !ok && !transportOnly[k] {
			t.Errorf("中转收到了不该有的请求头 %s", k)
		}
	}
	for k, vs := range got {
		for _, v := range vs {
			for _, secret := range []string{cookieValue, accountID, "sk-official-0000", "custom-marker-0000", "originator-marker-0000"} {
				if strings.Contains(v, secret) {
					t.Errorf("请求头 %s 把 %q 带到了中转", k, secret)
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
		{"模型不在中转集合", http.MethodPost, `{"model":"official-model"}`},
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
				t.Fatalf("中转不应收到请求，实际 %d 次", n)
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

func TestRoute_PrefixedRelayModel_RewrittenToOriginalName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		internal  string
		wantModel string
	}{
		{"带点的模型名", "relay-glm-5.3", "glm-5.3"},
		{"和官方同名的模型", "relay-gpt-x", "gpt-x"},
		{"原名本身以 relay- 开头", "relay-relay-x", "relay-x"},
		{"编号 2 的配置", "relay2-auto", "auto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{tt.internal: e.target(tt.wantModel)}})
			body := `{"model":"` + tt.internal + `","input":"hello","stream":true}`
			resp, _ := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(body))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("状态码 = %d", resp.StatusCode)
			}
			var sent map[string]any
			if err := json.Unmarshal(e.relay.onlyHit(t).Body, &sent); err != nil {
				t.Fatal(err)
			}
			if sent["model"] != tt.wantModel || sent["input"] != "hello" || sent["stream"] != true || len(sent) != 3 {
				t.Fatalf("中转收到的请求体 = %v，期望 model=%s 且其余字段不变", sent, tt.wantModel)
			}
			if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("不应发往官方，实际 %d 次", n)
			}
		})
	}
}

func TestRoute_MultiConfig_EachGoesToOwnBaseURLAndKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	relayB := newFakeUpstream(t, nil)
	const keyB = "sk-example-b-0000"
	e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{
		"relay-auto":  e.target("auto"),
		"relay2-auto": {BaseURL: relayB.URL + "/v1", Key: keyB, Model: "auto"},
	}})
	for _, internal := range []string{"relay-auto", "relay2-auto"} {
		e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+internal+`","input":"hi"}`))
	}
	hitA, hitB := e.relay.onlyHit(t), relayB.onlyHit(t)
	if got := hitA.Header.Get("Authorization"); got != "Bearer "+relayKey {
		t.Errorf("中转 A 收到的 Authorization = %q", got)
	}
	if got := hitB.Header.Get("Authorization"); got != "Bearer "+keyB {
		t.Errorf("中转 B 收到的 Authorization = %q", got)
	}
	for name, hit := range map[string]upstreamHit{"A": hitA, "B": hitB} {
		var sent map[string]any
		if err := json.Unmarshal(hit.Body, &sent); err != nil || sent["model"] != "auto" {
			t.Errorf("中转 %s 收到的请求体 = %q，期望 model=auto", name, hit.Body)
		}
	}
	if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
		t.Fatalf("不应发往官方，实际 %d 次", n)
	}
}

func TestRoute_ConcurrentSetRelay_KeysNeverCross(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	relayB := newFakeUpstream(t, nil)
	const keyB = "sk-example-b-0000"
	cfgA := proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{"relay-auto": e.target("auto")}}
	cfgB := proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{"relay-auto": {BaseURL: relayB.URL + "/v1", Key: keyB, Model: "auto"}}}
	e.srv.SetRelay(cfgA)

	stop := make(chan struct{})
	var swapper sync.WaitGroup
	swapper.Add(1)
	go func() {
		defer swapper.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				e.srv.SetRelay(cfgB)
			} else {
				e.srv.SetRelay(cfgA)
			}
		}
	}()
	var senders sync.WaitGroup
	for i := 0; i < 8; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			for j := 0; j < 10; j++ {
				e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"relay-auto","input":"hi"}`))
			}
		}()
	}
	senders.Wait()
	close(stop)
	swapper.Wait()

	for _, hit := range e.relay.Hits() {
		if got := hit.Header.Get("Authorization"); got != "Bearer "+relayKey {
			t.Errorf("中转 A 收到的 Authorization = %q，只应是自己的 Key", got)
		}
	}
	for _, hit := range relayB.Hits() {
		if got := hit.Header.Get("Authorization"); got != "Bearer "+keyB {
			t.Errorf("中转 B 收到的 Authorization = %q，只应是自己的 Key", got)
		}
	}
	if n := len(e.relay.Hits()) + len(relayB.Hits()); n != 80 {
		t.Errorf("两个中转共收到 %d 次请求，期望 80", n)
	}
}

func TestRoute_UltraToMax_UltraRewrittenToMax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		ultraToMax bool
		wantEffort string
	}{
		{"UltraToMax 为真时 ultra 改回 max", true, "max"},
		{"UltraToMax 为假时 ultra 不变", false, "ultra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			target := e.target("auto")
			target.UltraToMax = tt.ultraToMax
			e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{"relay-auto": target}})
			e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"relay-auto","reasoning":{"effort":"ultra"}}`))
			var sent struct {
				Reasoning struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
			}
			if err := json.Unmarshal(e.relay.onlyHit(t).Body, &sent); err != nil {
				t.Fatal(err)
			}
			if sent.Reasoning.Effort != tt.wantEffort {
				t.Fatalf("中转收到的 effort = %q，期望 %q", sent.Reasoning.Effort, tt.wantEffort)
			}
		})
	}
}

func TestRoute_RelayShapedNameNotInTable_RejectedAsDeleted(t *testing.T) {
	t.Parallel()
	const wantMessage = "这个模型所在的中转配置已删除，请换一个模型。"
	for _, relayOnly := range []bool{false, true} {
		for _, model := range []string{"relay3-auto", "relay-unknown", "relay12-x"} {
			t.Run(fmt.Sprintf("%s/relayOnly=%v", model, relayOnly), func(t *testing.T) {
				t.Parallel()
				e := newEnv(t, nil)
				relayB := newFakeUpstream(t, nil)
				e.srv.SetRelay(proxy.RelayConfig{
					Routes: map[string]proxy.RelayTarget{
						"relay-auto":  e.target("auto"),
						"relay2-auto": {BaseURL: relayB.URL + "/v1", Key: "sk-example-b-0000", Model: "auto"},
					},
					RelayOnly: relayOnly,
				})
				resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+model+`"}`))
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("状态码 = %d，期望 400", resp.StatusCode)
				}
				var got relayErrorBody
				if err := json.Unmarshal(body, &got); err != nil || got.Error.Message != wantMessage ||
					got.Error.Type != "model_bridge_error" || got.Error.Code != "relay_deleted" {
					t.Fatalf("响应 = %s，期望 message=%q type=model_bridge_error code=relay_deleted", body, wantMessage)
				}
				if n := e.upstreamHitCount() + len(relayB.Hits()); n != 0 {
					t.Fatalf("不应有任何上游收到请求，实际 %d 次", n)
				}
				waitLog(t, e.log, "rejected")
			})
		}
	}
}

func TestRoute_NonRelayShapedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		relayOnly bool
		model     string
		wantOff   bool
	}{
		{"官方加中转方式下 gpt-x 走官方", false, "gpt-x", true},
		{"官方加中转方式下 relayx-foo 走官方", false, "relayx-foo", true},
		{"只用中转方式下 gpt-x 被拒绝", true, "gpt-x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			e.srv.SetRelay(proxy.RelayConfig{Routes: map[string]proxy.RelayTarget{"relay-auto": e.target("auto")}, RelayOnly: tt.relayOnly})
			body := `{"model":"` + tt.model + `","input":"hello"}`
			resp, got := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(body))
			if n := len(e.relay.Hits()); n != 0 {
				t.Fatalf("中转不应收到请求，实际 %d 次", n)
			}
			if tt.wantOff {
				if hit := e.api.onlyHit(t); string(hit.Body) != body {
					t.Fatalf("官方收到的请求体 = %q，期望逐字节不变", hit.Body)
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest || string(got) != officialUnavailableBody {
				t.Fatalf("响应 = %d %s", resp.StatusCode, got)
			}
			if n := len(e.api.Hits()); n != 0 {
				t.Fatalf("官方不应收到请求，实际 %d 次", n)
			}
		})
	}
}

func TestRoute_OfficialRequestWithUltraEffortUnchanged(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	body := `{"model":"official-model","reasoning":{"effort":"ultra"}}`
	e.do(t, http.MethodPost, "/v1/responses", nil, []byte(body))
	if got := e.api.onlyHit(t).Body; string(got) != body {
		t.Fatalf("官方收到的请求体 = %q，期望逐字节不变", got)
	}
}
