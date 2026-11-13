package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	"modelbridge/internal/catalog"
)

const (
	defaultOfficialAPIURL     = "https://api.openai.com/v1"
	defaultOfficialChatGPTURL = "https://chatgpt.com/backend-api/codex"
)

type RelayConfig struct {
	Models []string
	// ParamModels 是 Models 中来自参数表、需要把 reasoning.effort 的 ultra 改回 max 再发给中台的那部分
	ParamModels []string
	BaseURL     string
	Key         string
	RelayOnly   bool
}

type relayState struct {
	mu          sync.RWMutex
	models      map[string]struct{}
	paramModels map[string]struct{}
	base        string
	key         string
	relayOnly   bool
}

func (s *relayState) set(c RelayConfig) {
	models := make(map[string]struct{}, len(c.Models))
	for _, m := range c.Models {
		models[m] = struct{}{}
	}
	paramModels := make(map[string]struct{}, len(c.ParamModels))
	for _, m := range c.ParamModels {
		paramModels[m] = struct{}{}
	}
	s.mu.Lock()
	s.paramModels = paramModels
	s.models, s.base, s.key = models, c.BaseURL, c.Key
	s.relayOnly = c.RelayOnly
	s.mu.Unlock()
}

type route struct {
	relay      bool
	target     *url.URL
	key        string
	model      string
	ultraToMax bool
	reject     bool
}

func (s *Server) decideRoute(r *http.Request, urlPath, model string) (route, error) {
	rest := strings.TrimPrefix(urlPath, "/v1")

	s.relayState.mu.RLock()
	origName := strings.TrimPrefix(model, catalog.RelaySlugPrefix)
	_, isRelay := s.relayState.models[origName]
	isRelay = isRelay && origName != model
	_, ultraToMax := s.relayState.paramModels[origName]
	base, key := s.relayState.base, s.relayState.key
	relayOnly := s.relayState.relayOnly
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
	return route{relay: isRelay, target: u, key: key, model: origName, ultraToMax: isRelay && ultraToMax, reject: relayOnly && !isRelay}, nil
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
