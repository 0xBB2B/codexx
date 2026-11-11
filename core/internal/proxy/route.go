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

type RelayConfig struct {
	Models  []string
	BaseURL string
	Key     string
}

type relayState struct {
	mu     sync.RWMutex
	models map[string]struct{}
	base   string
	key    string
}

func (s *relayState) set(c RelayConfig) {
	models := make(map[string]struct{}, len(c.Models))
	for _, m := range c.Models {
		models[m] = struct{}{}
	}
	s.mu.Lock()
	s.models, s.base, s.key = models, c.BaseURL, c.Key
	s.mu.Unlock()
}

type route struct {
	relay  bool
	target *url.URL
	key    string
}

func (s *Server) decideRoute(r *http.Request, urlPath, model string) (route, error) {
	rest := strings.TrimPrefix(urlPath, "/v1")

	s.relayState.mu.RLock()
	_, isRelay := s.relayState.models[model]
	base, key := s.relayState.base, s.relayState.key
	s.relayState.mu.RUnlock()

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
	return route{relay: isRelay, target: u, key: key}, nil
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
