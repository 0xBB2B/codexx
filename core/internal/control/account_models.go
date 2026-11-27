package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"modelbridge/internal/catalog"
	"modelbridge/internal/proxy"
)

const (
	accountQueryInterval = time.Hour
	accountQueryTimeout  = 5 * time.Second
	maxAccountModelsBody = 4 << 20
)

func (s *Service) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

func (s *Service) onOfficialRequest(h http.Header) {
	account := h.Get("ChatGPT-Account-ID")
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	if s.accountQuerying {
		return
	}
	now := s.now()
	if s.accountQueried && account == s.accountID && now.Sub(s.accountQueriedAt) <= accountQueryInterval {
		return
	}
	s.accountQuerying, s.accountQueried, s.accountID, s.accountQueriedAt = true, true, account, now
	go s.syncAccountModels(h)
}

func (s *Service) syncAccountModels(h http.Header) {
	defer func() {
		s.accountMu.Lock()
		s.accountQuerying = false
		s.accountMu.Unlock()
	}()

	version, err := clientVersion(s.modelsCachePath())
	if err != nil {
		return
	}
	ctx := context.Background()
	baseURL := s.opts.OfficialChatGPTURL
	if baseURL == "" {
		baseURL = proxy.DefaultOfficialChatGPTURL
	}
	slugs, err := fetchAccountSlugs(ctx, baseURL, version, h)
	if err != nil {
		return
	}

	s.mu.Lock()
	active := s.forwarding && s.proxy != nil && s.accessMode == modeBoth
	var codex string
	if active {
		codex, err = s.locateCodex()
	}
	s.mu.Unlock()
	if !active || err != nil {
		return
	}
	official, err := catalog.LoadOfficial(ctx, codex)
	if err != nil {
		return
	}
	next := officialSlugSet(catalog.IntersectOfficial(official, slugs))
	if len(next) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.forwarding || s.proxy == nil || s.accessMode != modeBoth {
		return
	}
	if err := catalog.NewStore(s.opts.DataDir).SaveAccountModels(slugs); err != nil {
		return
	}
	if slices.Equal(next, s.officialSet) {
		return
	}
	if err := s.rebuild(ctx, cacheAll(s.relays)); err != nil {
		return
	}
	s.officialRevision++
	s.notifyLocked()
}

func officialSlugSet(entries []catalog.Entry) []string {
	var out []string
	for _, e := range entries {
		if slug, ok := e["slug"].(string); ok {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func fetchAccountSlugs(ctx context.Context, baseURL, version string, h http.Header) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, accountQueryTimeout)
	defer cancel()
	target := strings.TrimRight(baseURL, "/") + "/models?client_version=" + url.QueryEscape(version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Authorization", "ChatGPT-Account-ID", "originator", "User-Agent"} {
		if v := h.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询账号模型状态码 %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAccountModelsBody)).Decode(&body); err != nil {
		return nil, err
	}
	var slugs []string
	for _, m := range body.Models {
		if m.Slug != "" {
			slugs = append(slugs, m.Slug)
		}
	}
	if len(slugs) == 0 {
		return nil, errors.New("账号模型名单为空")
	}
	return slugs, nil
}

func clientVersion(modelsCachePath string) (string, error) {
	raw, err := os.ReadFile(modelsCachePath)
	if err != nil {
		return "", err
	}
	var file struct {
		ClientVersion string `json:"client_version"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", err
	}
	version, _, _ := strings.Cut(file.ClientVersion, "-")
	if version == "" {
		return "", errors.New("models_cache.json 缺 client_version")
	}
	return version, nil
}
