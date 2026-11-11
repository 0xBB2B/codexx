package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func neutralRelayError(status int) (code, message string) {
	switch {
	case status == http.StatusUnauthorized:
		return "relay_401", "中台服务返回 401：API Key 无效或已过期"
	case status == http.StatusForbidden:
		return "relay_403", "中台服务返回 403：API Key 没有权限"
	case status == http.StatusNotFound:
		return "relay_404", "中台服务返回 404：Base URL 不正确"
	case status == http.StatusTooManyRequests:
		return "relay_429", "中台服务返回 429：请求太频繁，请稍后重试"
	case status >= 500:
		return "relay_5xx", fmt.Sprintf("中台服务返回 %d：中台服务暂时不可用", status)
	default:
		return "relay_4xx", fmt.Sprintf("中台服务返回 %d：请求未被中台接受", status)
	}
}

func unreachableRelayError() (code, message string) {
	return "relay_unreachable", "连不上中台，请检查网络和 Base URL"
}

func relayErrorBody(code, message string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{
		"message": message,
		"type":    "model_bridge_error",
		"code":    code,
	}})
	return b
}
