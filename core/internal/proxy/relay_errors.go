package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func neutralRelayError(status int) (code, message string) {
	switch {
	case status == http.StatusUnauthorized:
		return "relay_401", "中转服务返回 401：API Key 无效或已过期"
	case status == http.StatusForbidden:
		return "relay_403", "中转服务返回 403：API Key 没有权限"
	case status == http.StatusNotFound:
		return "relay_404", "中转服务返回 404：Base URL 不正确"
	case status == http.StatusTooManyRequests:
		return "relay_429", "中转服务返回 429：请求太频繁，请稍后重试"
	case status >= 500:
		return "relay_5xx", fmt.Sprintf("中转服务返回 %d：中转服务暂时不可用", status)
	default:
		return "relay_4xx", fmt.Sprintf("中转服务返回 %d：请求未被中转服务接受", status)
	}
}

func unreachableRelayError() (code, message string) {
	return "relay_unreachable", "连不上中转服务，请检查网络和 Base URL"
}

func officialUnavailableError() (code, message string) {
	return "official_unavailable", "当前没有登录 ChatGPT，官方模型不可用，请切换到中转模型。"
}

func relayDeletedError() (code, message string) {
	return "relay_deleted", "这个模型所在的中转配置已删除，请换一个模型。"
}

func relayNotFetchedError(name string) (code, message string) {
	return "relay_not_fetched", fmt.Sprintf("还没获取到「%s」的模型列表。请打开模型接入助手，在「%s」那一行点「重试」，或者换一个模型。", name, name)
}

func relayModelMissingError(name string) (code, message string) {
	return "relay_model_missing", fmt.Sprintf("「%s」的模型列表里现在没有这个模型，请换一个模型。", name)
}

type relayErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func relayErrorBody(code, message string) []byte {
	b, _ := json.Marshal(struct {
		Error relayErrorDetail `json:"error"`
	}{relayErrorDetail{message, "model_bridge_error", code}})
	return b
}
