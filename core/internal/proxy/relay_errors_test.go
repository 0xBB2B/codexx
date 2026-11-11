package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const leakMarker = "LEAK-MARKER-9f3a"

type relayErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func assertNeutralError(t *testing.T, e *testEnv, resp *http.Response, body []byte, wantStatus int, wantMessage, wantCode string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("状态码 = %d，期望 %d", resp.StatusCode, wantStatus)
	}
	var got relayErrorBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("响应不是 JSON：%s", body)
	}
	if got.Error.Message != wantMessage || got.Error.Type != "model_bridge_error" || got.Error.Code != wantCode {
		t.Fatalf("错误体 = %+v，期望 message=%q code=%q", got.Error, wantMessage, wantCode)
	}
	if strings.Contains(string(body), leakMarker) {
		t.Fatalf("响应里出现了中台的原始内容：%s", body)
	}
	waitLog(t, e.log, "relay")
	if strings.Contains(e.log.String(), leakMarker) {
		t.Fatalf("日志里出现了中台的原始内容：%s", e.log.String())
	}
}

func TestRelayError_StatusMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status      int
		wantMessage string
		wantCode    string
	}{
		{401, "中台服务返回 401：API Key 无效或已过期", "relay_401"},
		{403, "中台服务返回 403：API Key 没有权限", "relay_403"},
		{404, "中台服务返回 404：Base URL 不正确", "relay_404"},
		{429, "中台服务返回 429：请求太频繁，请稍后重试", "relay_429"},
		{500, "中台服务返回 500：中台服务暂时不可用", "relay_5xx"},
		{502, "中台服务返回 502：中台服务暂时不可用", "relay_5xx"},
		{503, "中台服务返回 503：中台服务暂时不可用", "relay_5xx"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Leak", leakMarker)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":{"message":"` + leakMarker + `"}}`))
			})
			resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
			assertNeutralError(t, e, resp, body, tt.status, tt.wantMessage, tt.wantCode)
			if resp.Header.Get("X-Leak") != "" {
				t.Fatalf("中台的响应头被透传：%v", resp.Header)
			}
		})
	}
}

func TestRelayError_Unreachable(t *testing.T) {
	t.Parallel()
	const wantMessage = "连不上中台，请检查网络和 Base URL"

	t.Run("连接被中台直接断开", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		})
		resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
		assertNeutralError(t, e, resp, body, http.StatusBadGateway, wantMessage, "relay_unreachable")
	})

	t.Run("中台端口无人监听", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, nil)
		e.relay.Close()
		resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
		assertNeutralError(t, e, resp, body, http.StatusBadGateway, wantMessage, "relay_unreachable")
	})
}

func TestOfficialError_ForwardedAsIs(t *testing.T) {
	t.Parallel()
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			want := []byte(`{"error":{"message":"official-says-no","code":"official_code"}}`)
			e := newEnv(t, nil)
			e.api.setHandler(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Official", "kept")
				w.WriteHeader(status)
				_, _ = w.Write(want)
			})
			resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"official-model"}`))
			if resp.StatusCode != status {
				t.Fatalf("状态码 = %d，期望 %d", resp.StatusCode, status)
			}
			if !bytes.Equal(body, want) {
				t.Fatalf("官方错误体被改动：%s", body)
			}
			if resp.Header.Get("X-Official") != "kept" {
				t.Fatalf("官方响应头丢失：%v", resp.Header)
			}
		})
	}
}

func TestRelayError_Other4xx(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(leakMarker))
			})
			resp, body := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
			assertNeutralError(t, e, resp, body, status, fmt.Sprintf("中台服务返回 %d：请求未被中台接受", status), "relay_4xx")
		})
	}
}

func TestRelayError_TrailerNotLeaked(t *testing.T) {
	t.Parallel()
	const trailerName = "X-Leak-Marker-0000"
	e := newEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", trailerName)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(leakMarker))
		w.Header().Set(trailerName, "v")
	})
	resp, _ := e.do(t, http.MethodPost, "/v1/responses", nil, []byte(`{"model":"`+relayModel+`"}`))
	if _, ok := resp.Trailer[trailerName]; ok {
		t.Fatalf("trailer 被透传：%v", resp.Trailer)
	}
	for k, vs := range resp.Header {
		if strings.Contains(strings.ToLower(k+strings.Join(vs, ",")), strings.ToLower(trailerName)) {
			t.Fatalf("响应头里出现了中台的 trailer 声明：%v", resp.Header)
		}
	}
}
