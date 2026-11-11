package control

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"modelbridge/internal/catalog"
	"modelbridge/internal/codexconfig"
	"modelbridge/internal/proxy"
)

const logFile = "model-bridge.log"

var uninstallFiles = []string{
	"model-bridge-catalog.json",
	"access-state.json",
	"relay-models.json",
	"relay-reasoning-ids.json",
	settingsFile,
	logFile,
}

type Options struct {
	DataDir            string
	Version            string
	ListenAddr         string
	OfficialAPIURL     string
	OfficialChatGPTURL string
	CodexPath          string
	ConfigPath         string
	OnStatus           func(Status)
}

type Error struct {
	Code string   `json:"code"`
	Keys []string `json:"keys,omitempty"`
}

func (e *Error) Error() string { return e.Code }

type ProxyStatus struct {
	Running bool    `json:"running"`
	Error   *string `json:"error"`
}

type ChatGPTStatus struct {
	Found bool    `json:"found"`
	Error *string `json:"error"`
}

type RelayStatus struct {
	ModelCount int        `json:"model_count"`
	FetchedAt  *time.Time `json:"fetched_at"`
	Stale      bool       `json:"stale"`
	Error      *string    `json:"error"`
}

type UpdateInfo struct {
	LatestVersion *string `json:"latest_version"`
	URL           *string `json:"url"`
}

type Status struct {
	Version        string        `json:"version"`
	Forwarding     bool          `json:"forwarding"`
	Proxy          ProxyStatus   `json:"proxy"`
	ChatGPT        ChatGPTStatus `json:"chatgpt"`
	Relay          RelayStatus   `json:"relay"`
	TakenOver      []string      `json:"taken_over"`
	RestoreBlocked []string      `json:"restore_blocked"`
	ConfigPath     string        `json:"config_path"`
	Update         UpdateInfo    `json:"update"`
}

type InitParams struct {
	BaseURL    *string `json:"base_url"`
	APIKey     *string `json:"api_key"`
	ChatGPTDir *string `json:"chatgpt_dir"`
}

type SaveResult struct {
	ModelsChanged   bool `json:"models_changed"`
	RelayModelCount int  `json:"relay_model_count"`
}

type UpdateResult struct {
	LatestVersion *string `json:"latest_version"`
	URL           *string `json:"url"`
	IsNewer       bool    `json:"is_newer"`
}

type Service struct {
	opts       Options
	configPath string

	mu             sync.Mutex
	baseURL        string
	apiKey         string
	chatgptDir     string
	forwarding     bool
	proxy          *proxy.Server
	proxyLog       *os.File
	proxyErr       string
	chatgptFound   bool
	chatgptErr     string
	relayIDs       []string
	relayErr       string
	fetchedAt      time.Time
	levels         []string
	takenOver      []string
	restoreBlocked []string
}

func NewService(opts Options) *Service {
	path := opts.ConfigPath
	if path == "" {
		path, _ = codexconfig.ConfigPath()
	}
	return &Service{opts: opts, configPath: path}
}

func (s *Service) Init(ctx context.Context, p InitParams) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set, err := LoadSettings(s.opts.DataDir)
	if err != nil {
		return s.statusLocked(), toError(err)
	}
	s.forwarding = set.Forwarding
	s.baseURL, s.apiKey, s.chatgptDir = deref(p.BaseURL), deref(p.APIKey), deref(p.ChatGPTDir)
	s.takenOver, _ = codexconfig.TakenOverKeys(s.configCfg())
	s.restoreBlocked = nil
	if !s.forwarding {
		s.restoreBlocked, _ = codexconfig.CheckTampered(s.configCfg())
	}

	if s.forwarding {
		_ = s.activate(ctx)
	} else {
		_, err := s.locateCodex()
		s.chatgptFound, s.chatgptErr = err == nil, errCode(err)
	}
	s.notifyLocked()
	return s.statusLocked(), nil
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) SaveConnection(ctx context.Context, baseURL, apiKey string) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if u, err := url.Parse(baseURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return SaveResult{}, &Error{Code: "invalid_base_url"}
	}
	ids, err := catalog.FetchRelayIDs(ctx, baseURL, apiKey, catalog.RelayTimeout)
	if err != nil {
		return SaveResult{}, toError(err)
	}
	prev := s.snapshot()
	before := slices.Clone(s.relayIDs)
	s.baseURL, s.apiKey = baseURL, apiKey
	if err := s.refreshRelay(ctx, ids); err != nil {
		s.rollback(prev)
		return SaveResult{}, toError(err)
	}
	s.notifyLocked()
	return SaveResult{
		ModelsChanged:   !sameSet(before, s.relayIDs),
		RelayModelCount: len(s.relayIDs),
	}, nil
}

func (s *Service) SetForwarding(ctx context.Context, enabled bool) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifyLocked()

	if enabled {
		if err := s.activate(ctx); err != nil {
			return s.statusLocked(), toError(err)
		}
		s.forwarding = true
		if err := SaveSettings(s.opts.DataDir, Settings{Forwarding: true}); err != nil {
			return s.statusLocked(), toError(err)
		}
		return s.statusLocked(), nil
	}

	restoreErr := codexconfig.Restore(s.configCfg())
	var ce *codexconfig.Error
	blocked := errors.As(restoreErr, &ce) && ce.Code == codexconfig.CodeRestoreBlocked
	if restoreErr != nil && !blocked {
		return s.statusLocked(), toError(restoreErr)
	}
	s.stopProxy()
	s.forwarding = false
	s.takenOver = nil
	s.restoreBlocked = nil
	if blocked {
		s.restoreBlocked = ce.Keys
	}
	if err := SaveSettings(s.opts.DataDir, Settings{Forwarding: false}); err != nil {
		return s.statusLocked(), toError(err)
	}
	if restoreErr != nil {
		return s.statusLocked(), toError(restoreErr)
	}
	return s.statusLocked(), nil
}

func (s *Service) RefreshModels(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refreshRelay(ctx, nil); err != nil {
		return s.statusLocked(), toError(err)
	}
	s.notifyLocked()
	return s.statusLocked(), nil
}

func (s *Service) CheckUpdate(_ context.Context, _ bool) (UpdateResult, error) {
	return UpdateResult{}, nil
}

func (s *Service) PrepareUninstall(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := codexconfig.RestoreAll(s.configCfg()); err != nil {
		return toError(err)
	}
	s.stopProxy()
	for _, name := range uninstallFiles {
		if err := os.Remove(filepath.Join(s.opts.DataDir, name)); err != nil && !os.IsNotExist(err) {
			return toError(err)
		}
	}
	return nil
}

func (s *Service) Shutdown(_ context.Context, restore bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopProxy()
	if restore {
		if err := codexconfig.Restore(s.configCfg()); err != nil {
			return toError(err)
		}
	}
	return nil
}

func (s *Service) activate(ctx context.Context) error {
	s.chatgptErr, s.proxyErr = "", ""
	err := s.tryActivate(ctx)
	if err == nil {
		return nil
	}
	s.stopProxy()
	if code := errCode(err); strings.HasPrefix(code, "chatgpt_") {
		s.chatgptErr = code
	} else {
		s.proxyErr = code
	}
	return err
}

func (s *Service) tryActivate(ctx context.Context) error {
	_, err := s.locateCodex()
	s.chatgptFound = err == nil
	if err != nil {
		return err
	}
	if s.proxy == nil {
		if err := s.startProxy(); err != nil {
			return err
		}
	}
	if err := s.rebuild(ctx, nil); err != nil {
		return err
	}
	return s.enable()
}

func (s *Service) enable() error {
	cfg := s.configCfg()
	taken, err := codexconfig.Enable(cfg, codexconfig.EnableInput{
		CatalogPath:  filepath.Join(s.opts.DataDir, "model-bridge-catalog.json"),
		HasAutoModel: slices.Contains(s.relayIDs, "auto"),
		Efforts:      s.levels,
	})
	if err != nil {
		return err
	}
	recorded, err := codexconfig.TakenOverKeys(cfg)
	if err != nil {
		return err
	}
	s.takenOver = slices.Compact(slices.Sorted(slices.Values(append(taken, recorded...))))
	s.restoreBlocked = nil
	return nil
}

// fetched 为 nil 表示还没拉过中台列表，由本函数自己去拉；调用方已拉过就直接传入，避免重复请求
func (s *Service) refreshRelay(ctx context.Context, fetched []string) error {
	if s.forwarding && s.proxy != nil {
		if err := s.rebuild(ctx, fetched); err != nil {
			return err
		}
		return s.enable()
	}
	if fetched == nil {
		if s.baseURL == "" {
			return nil
		}
		ids, err := catalog.FetchRelayIDs(ctx, s.baseURL, s.apiKey, catalog.RelayTimeout)
		if err != nil {
			s.relayErr = errCode(err)
			return nil
		}
		fetched = ids
	}
	selected, err := selectRelay(fetched)
	if err != nil {
		return err
	}
	s.relayIDs, s.relayErr, s.fetchedAt = selected, "", time.Now().UTC().Truncate(time.Second)
	return nil
}

func (s *Service) rebuild(ctx context.Context, fetched []string) error {
	codex, err := s.locateCodex()
	if err != nil {
		return err
	}
	store := catalog.NewStore(s.opts.DataDir)
	var res *catalog.Result
	if s.baseURL == "" {
		official, err := catalog.LoadOfficial(ctx, codex)
		if err != nil {
			return err
		}
		if res, err = catalog.Merge(official, nil); err != nil {
			return err
		}
		if err := store.WriteCatalog(res.Models); err != nil {
			return err
		}
	} else {
		params, err := catalog.BuiltinParams()
		if err != nil {
			return err
		}
		res, err = catalog.Build(ctx, catalog.BuildOptions{
			CodexPath:    codex,
			RelayBaseURL: s.baseURL,
			RelayKey:     s.apiKey,
			RelayTimeout: catalog.RelayTimeout,
			DataDir:      s.opts.DataDir,
			Params:       params,
			RelayIDs:     fetched,
		})
		if err != nil {
			return err
		}
	}

	var ids []string
	if s.baseURL != "" {
		ids, _, _, _ = store.LoadRelayModels()
	}
	selected, err := selectRelay(ids)
	if err != nil {
		return err
	}
	s.relayIDs, s.relayErr, s.levels = selected, errCode(res.RelayErr), res.ReasoningLevels
	s.fetchedAt = time.Time{}
	if !res.RelayFetchedAt.IsZero() {
		s.fetchedAt = res.RelayFetchedAt.UTC().Truncate(time.Second)
	}
	s.pushRelay()
	return nil
}

func (s *Service) pushRelay() {
	if s.proxy != nil {
		s.proxy.SetRelay(proxy.RelayConfig{Models: s.relayIDs, BaseURL: s.baseURL, Key: s.apiKey})
	}
}

type connection struct {
	baseURL, apiKey string
	relayIDs        []string
	relayErr        string
	fetchedAt       time.Time
	levels          []string
}

func (s *Service) snapshot() connection {
	return connection{s.baseURL, s.apiKey, s.relayIDs, s.relayErr, s.fetchedAt, s.levels}
}

func (s *Service) rollback(c connection) {
	s.baseURL, s.apiKey, s.relayIDs, s.relayErr, s.fetchedAt, s.levels = c.baseURL, c.apiKey, c.relayIDs, c.relayErr, c.fetchedAt, c.levels
	s.pushRelay()
}

func (s *Service) startProxy() error {
	f, err := os.OpenFile(filepath.Join(s.opts.DataDir, logFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	p := proxy.New(proxy.Config{
		ListenAddr:         s.opts.ListenAddr,
		OfficialAPIURL:     s.opts.OfficialAPIURL,
		OfficialChatGPTURL: s.opts.OfficialChatGPTURL,
		LogOutput:          f,
		DataDir:            s.opts.DataDir,
	})
	if err := p.Start(); err != nil {
		_ = f.Close()
		return err
	}
	s.proxy, s.proxyLog = p, f
	return nil
}

func (s *Service) stopProxy() {
	if s.proxy == nil {
		return
	}
	_ = s.proxy.Close()
	_ = s.proxyLog.Close()
	s.proxy, s.proxyLog = nil, nil
}

func (s *Service) locateCodex() (string, error) {
	if s.opts.CodexPath != "" {
		if st, err := os.Stat(s.opts.CodexPath); err != nil || st.IsDir() {
			return "", &Error{Code: "chatgpt_not_found"}
		}
		return s.opts.CodexPath, nil
	}
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return catalog.FindCodexDarwin(catalog.DefaultDarwinRoots(home))
	case "windows":
		return catalog.FindCodexWindows(s.chatgptDir)
	}
	return "", &Error{Code: "chatgpt_not_found"}
}

func (s *Service) configCfg() codexconfig.Config {
	return codexconfig.Config{ConfigPath: s.configPath, DataDir: s.opts.DataDir}
}

func (s *Service) notifyLocked() {
	if s.opts.OnStatus != nil {
		s.opts.OnStatus(s.statusLocked())
	}
}

func (s *Service) statusLocked() Status {
	st := Status{
		Version:        s.opts.Version,
		Forwarding:     s.forwarding,
		Proxy:          ProxyStatus{Running: s.proxy != nil, Error: optional(s.proxyErr)},
		ChatGPT:        ChatGPTStatus{Found: s.chatgptFound, Error: optional(s.chatgptErr)},
		Relay:          RelayStatus{ModelCount: len(s.relayIDs), Stale: s.relayErr != "", Error: optional(s.relayErr)},
		TakenOver:      append([]string{}, s.takenOver...),
		RestoreBlocked: append([]string{}, s.restoreBlocked...),
		ConfigPath:     s.configPath,
	}
	if !s.fetchedAt.IsZero() {
		at := s.fetchedAt
		st.Relay.FetchedAt = &at
	}
	return st
}

func selectRelay(ids []string) ([]string, error) {
	params, err := catalog.BuiltinParams()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range catalog.SelectRelay(ids, params) {
		out = append(out, p.ID)
	}
	return out, nil
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func errCode(err error) string {
	if err == nil {
		return ""
	}
	return toError(err).Code
}

func toError(err error) *Error {
	var ce *Error
	if errors.As(err, &ce) {
		return ce
	}
	var cfg *codexconfig.Error
	if errors.As(err, &cfg) {
		return &Error{Code: cfg.Code, Keys: cfg.Keys}
	}
	var pe *proxy.Error
	if errors.As(err, &pe) {
		return &Error{Code: pe.Code}
	}
	if code := catalog.ErrorCode(err); code != "" {
		return &Error{Code: code}
	}
	return &Error{Code: "internal_error"}
}
