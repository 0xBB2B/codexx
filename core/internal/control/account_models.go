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
	"os/exec"
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

// 同一时间只有一个后台处理；忙时只留最新到的一个待办，最后到的请求说了算
func (s *Service) onOfficialRequest(h http.Header) {
	account := h.Get("ChatGPT-Account-ID")
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	now := s.now()
	if account == "" {
		if s.accountQuerying {
			if !s.accountDropRunning || s.accountPending != nil {
				s.accountPending = &pendingQuery{drop: true}
			}
			return
		}
		if now.Sub(s.accountDropAt) <= accountQueryInterval || catalog.NewStore(s.opts.DataDir).LoadAccountModels() == nil {
			return
		}
		s.startLocked(&pendingQuery{drop: true})
		return
	}
	if s.accountQuerying {
		if !s.accountDropRunning && account == s.accountID {
			s.accountPending = nil
			return
		}
		s.accountPending = &pendingQuery{account: account, header: h}
		return
	}
	if s.accountQueried && account == s.accountID && now.Sub(s.accountQueriedAt) <= accountQueryInterval {
		return
	}
	s.startLocked(&pendingQuery{account: account, header: h})
}

func (s *Service) startLocked(p *pendingQuery) {
	now := s.now()
	s.accountQuerying, s.accountDropRunning = true, p.drop
	if p.drop {
		s.accountQueried, s.accountDropAt = false, now
		go s.dropAccountModels()
		return
	}
	s.accountQueried, s.accountID, s.accountQueriedAt = true, p.account, now
	go s.syncAccountModels(p.header)
}

func (s *Service) finishQuery() {
	s.accountMu.Lock()
	if s.accountPending == nil {
		s.accountQuerying, s.accountDropRunning = false, false
		s.accountMu.Unlock()
		return
	}
	s.accountMu.Unlock()

	s.mu.Lock()
	active := s.forwardingBoth()
	s.mu.Unlock()

	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	p := s.accountPending
	s.accountPending = nil
	s.accountQuerying, s.accountDropRunning = false, false
	if p != nil && active {
		s.startLocked(p)
	}
}

// 不带账号标识的请求说明已退出账号登录，存下的名单不再适用
func (s *Service) dropAccountModels() {
	defer s.finishQuery()
	official, ok := s.loadOfficialIfActive()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.forwardingBoth() {
		s.clearDropThrottle()
		return
	}
	if !ok {
		return
	}
	store := catalog.NewStore(s.opts.DataDir)
	if err := s.rebuild(context.Background(), cacheAll(s.relays), official, nil); err != nil {
		return
	}
	if err := store.RemoveAccountModels(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	s.clearDropThrottle()
}

func (s *Service) clearDropThrottle() {
	s.accountMu.Lock()
	s.accountDropAt = time.Time{}
	s.accountMu.Unlock()
}

func (s *Service) forwardingBoth() bool {
	return s.forwarding && s.proxy != nil && s.accessMode == modeBoth
}

// codex 子进程运行期间不持 s.mu，所以先短暂加锁取路径
func (s *Service) loadOfficialIfActive() ([]catalog.Entry, bool) {
	s.mu.Lock()
	var codex string
	var err error
	active := s.forwardingBoth()
	if active {
		codex, err = s.locateCodex()
	}
	s.mu.Unlock()
	if !active || err != nil {
		return nil, false
	}
	official, err := catalog.LoadOfficial(context.Background(), codex)
	return official, err == nil
}

func (s *Service) syncAccountModels(h http.Header) {
	defer s.finishQuery()

	version, err := s.clientVersion()
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

	official, ok := s.loadOfficialIfActive()
	if !ok {
		return
	}
	next := officialSlugSet(catalog.IntersectOfficial(official, slugs))
	if len(next) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.forwardingBoth() {
		return
	}
	if err := catalog.NewStore(s.opts.DataDir).SaveAccountModels(slugs); err != nil {
		return
	}
	if slices.Equal(next, s.officialSet) {
		return
	}
	if err := s.rebuild(ctx, cacheAll(s.relays), official, slugs); err != nil {
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

func (s *Service) clientVersion() (string, error) {
	if raw, err := os.ReadFile(s.modelsCachePath()); err == nil {
		var file struct {
			ClientVersion string `json:"client_version"`
		}
		if json.Unmarshal(raw, &file) == nil {
			if version, _, _ := strings.Cut(file.ClientVersion, "-"); version != "" {
				return version, nil
			}
		}
	}
	s.mu.Lock()
	codex, err := s.locateCodex()
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, codex, "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", errors.New("codex --version 没有输出")
	}
	version, _, _ := strings.Cut(fields[len(fields)-1], "-")
	if version == "" {
		return "", errors.New("codex --version 输出无法解析")
	}
	return version, nil
}
