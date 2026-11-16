package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	defaultOfficialAPIURL     = "https://api.openai.com/v1"
	defaultOfficialChatGPTURL = "https://chatgpt.com/backend-api/codex"
)

type RelayTarget struct {
	BaseURL string
	Key     string
	// Model 是发给中转服务的原模型名
	Model string
	// FromParams 为真时，reasoning.effort 的 ultra 要改回 max 再发给中转
	FromParams bool
}

type RelayConfig struct {
	Routes    map[string]RelayTarget
	RelayOnly bool
}

type relayState struct {
	mu        sync.RWMutex
	routes    map[string]RelayTarget
	relayOnly bool
}

func (s *relayState) set(c RelayConfig) {
	routes := make(map[string]RelayTarget, len(c.Routes))
	for k, v := range c.Routes {
		routes[k] = v
	}
	s.mu.Lock()
	s.routes, s.relayOnly = routes, c.RelayOnly
	s.mu.Unlock()
}

type route struct {
	relay      bool
	target     *url.URL
	key        string
	model      string
	ultraToMax bool
	reject     bool
	deleted    bool
}

func (s *Server) decideRoute(r *http.Request, urlPath, model string) (route, error) {
	rest := strings.TrimPrefix(urlPath, "/v1")

	s.relayState.mu.RLock()
	t, isRelay := s.relayState.routes[model]
	relayOnly := s.relayState.relayOnly
	s.relayState.mu.RUnlock()

	if !isRelay && looksLikeRelaySlug(model) {
		return route{deleted: true}, nil
	}
	base := t.BaseURL
	if !isRelay {
		base = s.officialAPIURL
		if r.Header.Get("ChatGPT-Account-ID") != "" {
			base = s.officialChatGPTURL
		}
	}
	u, err := url.Parse(base)
	if err != nil {
		return route{relay: isRelay}, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + rest
	u.RawPath = ""
	u.RawQuery = r.URL.RawQuery
	return route{relay: isRelay, target: u, key: t.Key, model: t.Model, ultraToMax: t.FromParams, reject: relayOnly && !isRelay}, nil
}

func looksLikeRelaySlug(model string) bool {
	rest, ok := strings.CutPrefix(model, "relay")
	if !ok {
		return false
	}
	if strings.HasPrefix(rest, "-") {
		return true
	}
	digits := strings.TrimLeft(rest, "0123456789")
	return len(digits) < len(rest) && strings.HasPrefix(digits, "-")
}

func rewriteRelayHeaders(h http.Header, key string) {
	keep := map[string][]string{}
	for _, k := range []string{"Accept", "Content-Type", "OpenAI-Beta", "User-Agent"} {
		if v := h.Values(k); len(v) > 0 {
			keep[http.CanonicalHeaderKey(k)] = v
		}
	}
	for k := range h {
		delete(h, k)
	}
	for k, v := range keep {
		h[k] = v
	}
	h.Set("Authorization", "Bearer "+key)
}
