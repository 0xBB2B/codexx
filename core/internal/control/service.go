package control

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"modelbridge/internal/catalog"
	"modelbridge/internal/codexauth"
	"modelbridge/internal/codexconfig"
	"modelbridge/internal/proxy"
	"modelbridge/internal/update"
)

const (
	logFile           = "model-bridge.log"
	maxRelays         = 10
	maxRelayNameRunes = 16
)

var uninstallFiles = []string{
	"model-bridge-catalog.json",
	"access-state.json",
	"relay-models.json",
	"relay-reasoning-ids.json",
	settingsFile,
	logFile,
}

func uninstallPaths(dir string) []string {
	paths := make([]string, 0, len(uninstallFiles))
	for _, name := range uninstallFiles {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
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
	UpdateAPIBase      string
	UpdateRepository   string
	UpdateClock        update.Clock
	DetectLogin        func(ctx context.Context, codexPath string) codexauth.Result
}

const (
	modeBoth  = "both"
	modeRelay = "relay"
)

type AccessStatus struct {
	Mode   *string `json:"mode"`
	Target *string `json:"target"`
	Detect *string `json:"detect"`
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

type RelayEntryStatus struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ModelCount int        `json:"model_count"`
	FetchedAt  *time.Time `json:"fetched_at"`
	Stale      bool       `json:"stale"`
	Error      *string    `json:"error"`
}

type UpdateInfo struct {
	LatestVersion *string `json:"latest_version"`
	URL           *string `json:"url"`
	Installer     bool    `json:"installer"`
}

type Status struct {
	Version        string             `json:"version"`
	Forwarding     bool               `json:"forwarding"`
	Proxy          ProxyStatus        `json:"proxy"`
	ChatGPT        ChatGPTStatus      `json:"chatgpt"`
	Relays         []RelayEntryStatus `json:"relays"`
	DefaultRelay   *string            `json:"default_relay"`
	TakenOver      []string           `json:"taken_over"`
	RestoreBlocked []string           `json:"restore_blocked"`
	ConfigPath     string             `json:"config_path"`
	Update         UpdateInfo         `json:"update"`
	Access         AccessStatus       `json:"access"`
}

type RelayInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type InitParams struct {
	Relays     []RelayInput `json:"relays"`
	ChatGPTDir *string      `json:"chatgpt_dir"`
}

type SaveRelayParams struct {
	ID      *string `json:"id"`
	Name    string  `json:"name"`
	BaseURL string  `json:"base_url"`
	APIKey  string  `json:"api_key"`
}

type SaveRelayResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ModelCount    int    `json:"model_count"`
	ModelsChanged bool   `json:"models_changed"`
}

type UpdateResult struct {
	LatestVersion *string `json:"latest_version"`
	URL           *string `json:"url"`
	Installer     bool    `json:"installer"`
	IsNewer       bool    `json:"is_newer"`
}

type Service struct {
	opts       Options
	configPath string

	mu             sync.Mutex
	relays         []relayConfig
	relayInfos     map[string]relayInfo
	chatgptDir     string
	forwarding     bool
	proxy          *proxy.Server
	proxyLog       *os.File
	proxyErr       string
	restoreFailed  bool
	chatgptFound   bool
	chatgptErr     string
	relayShown     []string
	shownKnown     bool
	routes         map[string]catalog.RelayRoute
	defaultModel   string
	defaultRelay   string
	levels         []string
	takenOver      []string
	restoreBlocked []string
	update         UpdateInfo
	settings       Settings
	accessMode     string
	accessTarget   string
	accessDetect   string
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

	relays, err := relaysFromInput(p.Relays)
	if err != nil {
		return s.statusLocked(), err
	}
	set, err := LoadSettings(s.opts.DataDir)
	if err != nil {
		return s.statusLocked(), toError(err)
	}
	s.settings = set
	s.settings.NextRelayID = max(set.NextRelayID, maxRelayNum(relays)+1)
	s.forwarding = set.Forwarding
	s.accessMode = ""
	s.relays, s.relayInfos, s.relayShown, s.shownKnown, s.routes, s.defaultModel, s.defaultRelay = relays, nil, nil, false, nil, "", ""
	s.chatgptDir = deref(p.ChatGPTDir)
	s.takenOver, _ = codexconfig.TakenOverKeys(s.configCfg())
	s.restoreBlocked = nil
	if s.forwarding {
		s.accessMode = modeBoth
		if set.AccessMode != nil && *set.AccessMode == modeRelay {
			s.accessMode = modeRelay
		}
	}
	restartCtx, cancel := context.WithTimeout(ctx, RestartTimeout)
	defer cancel()
	s.redetect(restartCtx)
	s.notifyLocked()
	return s.statusLocked(), nil
}

func (s *Service) redetect(ctx context.Context) {
	if s.forwarding {
		_ = s.activate(ctx)
		_ = s.detect(ctx)
		return
	}
	s.restoreOnRedetect()
	codex, err := s.locateCodex()
	s.chatgptFound = err == nil
	if err != nil {
		s.chatgptErr = errCode(err)
		return
	}
	if s.chatgptErr == "chatgpt_unsupported" {
		if _, err := catalog.LoadOfficial(ctx, codex); err != nil {
			return
		}
	}
	s.chatgptErr = ""
}

func (s *Service) restoreOnRedetect() {
	cfg := s.configCfg()
	prevBlocked := s.restoreBlocked
	wasBlocked := len(prevBlocked) > 0
	s.restoreBlocked, _ = codexconfig.CheckTampered(cfg)
	if len(s.restoreBlocked) > 0 || !wasBlocked {
		return
	}
	err := codexconfig.Restore(cfg)
	if err == nil {
		s.takenOver = nil
		return
	}
	var ce *codexconfig.Error
	if errors.As(err, &ce) && ce.Code == codexconfig.CodeRestoreBlocked {
		s.restoreBlocked = ce.Keys
		return
	}
	s.restoreBlocked = prevBlocked
}

var RestartTimeout = 30 * time.Second

func (s *Service) Status() Status { return s.StatusContext(context.Background()) }

func (s *Service) StatusContext(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.forwarding && s.proxy == nil:
		ctx, cancel := context.WithTimeout(ctx, RestartTimeout)
		defer cancel()
		if s.activate(ctx) == nil {
			_ = s.detect(ctx)
		}
		s.notifyLocked()
	case !s.forwarding:
		s.redetect(ctx)
	}
	return s.statusLocked()
}

func (s *Service) SaveRelay(ctx context.Context, p SaveRelayParams) (SaveRelayResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	if p.ID != nil {
		if err := validateRelayID(*p.ID); err != nil {
			return SaveRelayResult{}, err
		}
		if idx = s.relayIndex(*p.ID); idx < 0 {
			return SaveRelayResult{}, &Error{Code: "relay_not_found"}
		}
	} else if len(s.relays) >= maxRelays {
		return SaveRelayResult{}, &Error{Code: "relay_limit"}
	}
	name := strings.TrimSpace(p.Name)
	if err := s.validateRelayName(name, idx); err != nil {
		return SaveRelayResult{}, err
	}
	if u, err := url.Parse(p.BaseURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return SaveRelayResult{}, &Error{Code: "invalid_base_url"}
	}
	if p.APIKey == "" {
		return SaveRelayResult{}, &Error{Code: "bad_params"}
	}
	models, err := catalog.FetchRelayModels(ctx, p.BaseURL, p.APIKey, catalog.RelayTimeout)
	if err != nil {
		return SaveRelayResult{}, toError(err)
	}

	prev, prevNext := s.snapshot(), s.settings.NextRelayID
	before := slices.Clone(s.relayShown)
	cfg := relayConfig{name: name, baseURL: p.BaseURL, apiKey: p.APIKey}
	candidate := slices.Clone(s.relays)
	if idx >= 0 {
		cfg.id = s.relays[idx].id
		candidate[idx] = cfg
	} else {
		cfg.id = strconv.Itoa(max(s.settings.NextRelayID, maxRelayNum(s.relays)+1))
		candidate = append(candidate, cfg)
		s.settings.NextRelayID, _ = strconv.Atoi(cfg.id)
		s.settings.NextRelayID++
		if err := s.saveSettings(); err != nil {
			s.settings.NextRelayID = prevNext
			return SaveRelayResult{}, toError(err)
		}
	}
	shownKnown := s.shownKnown
	plan := cacheAll(candidate)
	plan.mode[cfg.id], plan.models[cfg.id] = catalog.UseGiven, models
	s.relays = candidate
	if err := s.refreshRelay(ctx, plan); err != nil {
		s.rollback(prev)
		if s.settings.NextRelayID != prevNext {
			s.settings.NextRelayID = prevNext
			_ = s.saveSettings()
		}
		return SaveRelayResult{}, toError(err)
	}
	s.notifyLocked()
	return SaveRelayResult{
		ID:            cfg.id,
		Name:          name,
		ModelCount:    s.relayInfos[cfg.id].modelCount,
		ModelsChanged: (idx < 0 || shownKnown) && !sameSet(before, s.relayShown),
	}, nil
}

func (s *Service) DeleteRelay(ctx context.Context, id string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.relayIndex(id)
	if idx < 0 {
		return s.statusLocked(), &Error{Code: "relay_not_found"}
	}
	if len(s.relays) == 1 {
		return s.statusLocked(), &Error{Code: "relay_last"}
	}
	prev := s.snapshot()
	s.relays = slices.Delete(slices.Clone(s.relays), idx, idx+1)
	if err := s.refreshRelay(ctx, cacheAll(s.relays)); err != nil {
		s.rollback(prev)
		return s.statusLocked(), toError(err)
	}
	s.notifyLocked()
	return s.statusLocked(), nil
}

func (s *Service) SetForwarding(ctx context.Context, enabled bool) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifyLocked()

	if enabled {
		if err := s.detect(ctx); err != nil {
			return s.statusLocked(), toError(err)
		}
		prev := s.accessMode
		s.accessMode = s.accessTarget
		if err := s.activate(ctx); err != nil {
			s.accessMode = prev
			if s.forwarding {
				_ = s.activate(ctx)
			}
			return s.statusLocked(), toError(err)
		}
		s.forwarding = true
		if err := s.saveSettings(); err != nil {
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
	s.accessMode = ""
	s.takenOver = nil
	s.restoreBlocked = nil
	if blocked {
		s.restoreBlocked = ce.Keys
	}
	if err := s.saveSettings(); err != nil {
		return s.statusLocked(), toError(err)
	}
	if restoreErr != nil {
		return s.statusLocked(), toError(restoreErr)
	}
	return s.statusLocked(), nil
}

func (s *Service) DetectAccess(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifyLocked()

	if err := s.detect(ctx); err != nil {
		return s.statusLocked(), toError(err)
	}
	return s.statusLocked(), nil
}

func (s *Service) ApplyAccess(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notifyLocked()

	if !s.forwarding || s.accessTarget == "" || s.accessTarget == s.accessMode {
		return s.statusLocked(), nil
	}
	prev := s.accessMode
	s.accessMode = s.accessTarget
	if err := s.activate(ctx); err != nil {
		s.accessMode = prev
		_ = s.activate(ctx)
		return s.statusLocked(), toError(err)
	}
	if err := s.saveSettings(); err != nil {
		return s.statusLocked(), toError(err)
	}
	return s.statusLocked(), nil
}

func (s *Service) detect(ctx context.Context) error {
	result := codexauth.Failed
	if codex, err := s.locateCodex(); err == nil {
		detect := s.opts.DetectLogin
		if detect == nil {
			detect = codexauth.Detect
		}
		result = detect(ctx, codex)
	}
	s.accessDetect = string(result)
	switch result {
	case codexauth.LoggedIn:
		s.accessTarget = modeBoth
	case codexauth.NotLoggedIn:
		s.accessTarget = modeRelay
	default:
		s.accessTarget = modeRelay
		if last := s.settings.LastDetectedMode; last != nil && (*last == modeBoth || *last == modeRelay) {
			s.accessTarget = *last
		}
		return nil
	}
	last := s.accessTarget
	s.settings.LastDetectedMode = &last
	return s.saveSettings()
}

func (s *Service) saveSettings() error {
	s.settings.Forwarding = s.forwarding
	s.settings.AccessMode = nil
	if s.forwarding {
		mode := s.accessMode
		s.settings.AccessMode = &mode
	}
	return SaveSettings(s.opts.DataDir, s.settings)
}

func (s *Service) RefreshModels(ctx context.Context, id *string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan := fetchAll(s.relays)
	if id != nil {
		if s.relayIndex(*id) < 0 {
			return s.statusLocked(), &Error{Code: "relay_not_found"}
		}
		plan = cacheAll(s.relays)
		plan.mode[*id] = catalog.FetchRemote
	}
	prev := s.snapshot()
	if err := s.refreshRelay(ctx, plan); err != nil {
		s.rollback(prev)
		return s.statusLocked(), toError(err)
	}
	s.notifyLocked()
	return s.statusLocked(), nil
}

func (s *Service) CheckUpdate(ctx context.Context, manual bool) (UpdateResult, error) {
	var assetSuffix string
	switch runtime.GOOS {
	case "darwin":
		assetSuffix = "-macos.dmg"
	case "windows":
		assetSuffix = "-windows-setup.exe"
	}
	res, err := update.Check(ctx, update.Config{
		APIBase:        s.opts.UpdateAPIBase,
		Repository:     s.opts.UpdateRepository,
		CurrentVersion: s.opts.Version,
		AssetSuffix:    assetSuffix,
	})
	if err != nil {
		if manual {
			return UpdateResult{}, &Error{Code: "update_unavailable"}
		}
		s.logUpdateFailure(err)
		return UpdateResult{}, nil
	}
	if !res.IsNewer {
		s.mu.Lock()
		if s.update != (UpdateInfo{}) {
			s.update = UpdateInfo{}
			s.notifyLocked()
		}
		s.mu.Unlock()
		return UpdateResult{}, nil
	}
	s.mu.Lock()
	s.update = UpdateInfo{LatestVersion: &res.LatestVersion, URL: &res.URL, Installer: res.Installer}
	s.notifyLocked()
	s.mu.Unlock()
	return UpdateResult{LatestVersion: &res.LatestVersion, URL: &res.URL, Installer: res.Installer, IsNewer: true}, nil
}

func (s *Service) logUpdateFailure(err error) {
	f, openErr := os.OpenFile(filepath.Join(s.opts.DataDir, logFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "更新检查失败：%v\n", err)
}

func (s *Service) runUpdateLoop(ctx context.Context) {
	var clock update.Clock = update.RealClock{}
	if s.opts.UpdateClock != nil {
		clock = s.opts.UpdateClock
	}
	update.Loop(ctx, clock, func(ctx context.Context) { _, _ = s.CheckUpdate(ctx, false) })
}

func (s *Service) PrepareUninstall(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := codexconfig.RestoreAll(s.configCfg()); err != nil {
		return toError(err)
	}
	s.stopProxy()
	perRelay, _ := filepath.Glob(filepath.Join(s.opts.DataDir, "relay-models-*.json"))
	for _, path := range slices.Concat(perRelay, uninstallPaths(s.opts.DataDir)) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
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
	s.chatgptErr, s.proxyErr, s.restoreFailed = "", "", false
	err := s.tryActivate(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = &Error{Code: "proxy_start_timeout"}
	}
	s.stopProxy()
	s.restoreAfterFailedStart()
	if code := errCode(err); strings.HasPrefix(code, "chatgpt_") {
		s.chatgptErr = code
	} else {
		s.proxyErr = code
	}
	return err
}

func (s *Service) restoreAfterFailedStart() {
	cfg := s.configCfg()
	err := codexconfig.Restore(cfg)
	if err == nil {
		s.takenOver = nil
		return
	}
	s.takenOver, _ = codexconfig.TakenOverKeys(cfg)
	var ce *codexconfig.Error
	if errors.As(err, &ce) && ce.Code == codexconfig.CodeRestoreBlocked {
		s.restoreBlocked = ce.Keys
	}
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
	if err := s.rebuild(ctx, fetchAll(s.relays)); err != nil {
		return err
	}
	return s.enable()
}

func (s *Service) enable() error {
	cfg := s.configCfg()
	taken, err := codexconfig.Enable(cfg, codexconfig.EnableInput{
		CatalogPath:  filepath.Join(s.opts.DataDir, "model-bridge-catalog.json"),
		DefaultModel: s.defaultModel,
		Efforts:      s.levels,
		RelayOnly:    s.accessMode == modeRelay,
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

func (s *Service) refreshRelay(ctx context.Context, plan relayPlan) error {
	if s.forwarding && s.proxy == nil {
		if !s.chatgptFound {
			if _, err := s.locateCodex(); err == nil {
				s.chatgptFound, s.chatgptErr = true, ""
			}
		}
		if s.chatgptFound && s.chatgptErr == "" {
			return s.activate(ctx)
		}
	}
	if !s.forwarding || s.proxy == nil {
		return s.probe(ctx, plan)
	}
	files := s.dataFiles(plan)
	err := s.rebuild(ctx, plan)
	if err == nil {
		err = s.enable()
	}
	if err != nil {
		if restoreErr := files.restore(); restoreErr != nil {
			s.proxyErr, s.restoreFailed = errCode(restoreErr), true
		}
		return err
	}
	if s.restoreFailed && s.proxyErr == "internal_error" {
		s.proxyErr = ""
	}
	s.restoreFailed = false
	return nil
}

type dataFile struct {
	path string
	data []byte
	had  bool
}

type dataFiles []dataFile

// 写 Codex 配置失败时要让目录和缓存文件回到调用前，所以先记下它们；读不了的文件不记，也就不还原
func (s *Service) dataFiles(plan relayPlan) dataFiles {
	names := []string{"model-bridge-catalog.json"}
	for id := range plan.mode {
		names = append(names, "relay-models-"+id+".json")
	}
	files := make(dataFiles, 0, len(names))
	for _, name := range names {
		path := filepath.Join(s.opts.DataDir, name)
		data, err := os.ReadFile(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			files = append(files, dataFile{path: path, data: data, had: err == nil})
		}
	}
	return files
}

func (files dataFiles) restore() error {
	var errs []error
	for _, f := range files {
		if !f.had {
			if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			continue
		}
		if err := writeFileAtomic(f.path, f.data); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// 转发没开时不碰 Codex 配置也不写合并目录，找不到 ChatGPT 时 official 为空，按全部可见计数
func (s *Service) probe(ctx context.Context, plan relayPlan) error {
	var official []catalog.Entry
	if codex, err := s.locateCodex(); err == nil {
		official, _ = catalog.LoadOfficial(ctx, codex)
	}
	store := catalog.NewStore(s.opts.DataDir)
	infos := make(map[string]relayInfo, len(s.relays))
	var shown []string
	defaultModel, defaultRelay := "", ""
	for _, r := range s.relays {
		info := relayInfo{}
		var models []catalog.RelayModel
		fromCache := func() {
			cached, at, ok, err := store.LoadRelayModels(r.id)
			if err != nil && info.err == "" {
				info.err = errCode(err)
			}
			if err == nil && ok {
				models, info.fetchedAt = cached, at
			}
		}
		switch plan.mode[r.id] {
		case catalog.UseGiven:
			models, info.fetchedAt = plan.models[r.id], time.Now()
		case catalog.FetchRemote:
			fetched, err := catalog.FetchRelayModels(ctx, r.baseURL, r.apiKey, catalog.RelayTimeout)
			if err != nil {
				info.err = errCode(err)
				fromCache()
			} else {
				models, info.fetchedAt = fetched, time.Now()
			}
		default:
			info.err = s.relayInfos[r.id].err
			fromCache()
		}
		if info.err == "" && !info.fetchedAt.IsZero() && plan.mode[r.id] != catalog.CacheOnly {
			_ = store.SaveRelayModels(r.id, models, info.fetchedAt)
		}
		names := catalog.ShownRelayNames(models, official)
		info.modelCount = len(names)
		info.fetchedAt = trimTime(info.fetchedAt)
		infos[r.id] = info
		for _, n := range names {
			slug := catalog.RelaySlug(r.id, n)
			shown = append(shown, slug)
			if n == "auto" && defaultModel == "" {
				defaultModel, defaultRelay = slug, r.id
			}
		}
	}
	s.relayInfos, s.relayShown, s.shownKnown, s.defaultModel, s.defaultRelay = infos, shown, true, defaultModel, defaultRelay
	return nil
}

func (s *Service) rebuild(ctx context.Context, plan relayPlan) error {
	codex, err := s.locateCodex()
	if err != nil {
		return err
	}
	sources := make([]catalog.RelaySource, 0, len(s.relays))
	for _, r := range s.relays {
		sources = append(sources, catalog.RelaySource{
			ID: r.id, Name: r.name, BaseURL: r.baseURL, Key: r.apiKey,
			Mode: plan.mode[r.id], Models: plan.models[r.id],
		})
	}
	res, err := catalog.Build(ctx, catalog.BuildOptions{
		CodexPath:       codex,
		Relays:          sources,
		RelayTimeout:    catalog.RelayTimeout,
		DataDir:         s.opts.DataDir,
		ModelsCachePath: s.modelsCachePath(),
		RelayOnly:       s.accessMode == modeRelay,
	})
	if err != nil {
		return err
	}

	infos := make(map[string]relayInfo, len(res.Relays))
	for _, r := range res.Relays {
		info := relayInfo{err: errCode(r.Err), fetchedAt: trimTime(r.FetchedAt)}
		if plan.mode[r.ID] == catalog.CacheOnly && info.err == "" {
			info.err = s.relayInfos[r.ID].err
		}
		for _, slug := range res.RelayShown {
			if res.Routes[slug].ID == r.ID {
				info.modelCount++
			}
		}
		infos[r.ID] = info
	}
	s.relayInfos, s.relayShown, s.shownKnown, s.routes, s.levels = infos, res.RelayShown, true, res.Routes, res.ReasoningLevels
	s.defaultModel, s.defaultRelay = res.DefaultModel, res.Routes[res.DefaultModel].ID
	s.pushRelay()
	return nil
}

func (s *Service) pushRelay() {
	if s.proxy == nil {
		return
	}
	routes := make(map[string]proxy.RelayTarget, len(s.routes))
	for slug, r := range s.routes {
		if i := s.relayIndex(r.ID); i >= 0 {
			routes[slug] = proxy.RelayTarget{BaseURL: s.relays[i].baseURL, Key: s.relays[i].apiKey, Model: r.Model}
		}
	}
	configs := make(map[string]proxy.RelayInfo, len(s.relays))
	for _, r := range s.relays {
		configs[r.id] = proxy.RelayInfo{Name: r.name, Fetched: !s.relayInfos[r.id].fetchedAt.IsZero()}
	}
	s.proxy.SetRelay(proxy.RelayConfig{Routes: routes, Configs: configs, RelayOnly: s.accessMode == modeRelay})
}

type relayConfig struct {
	id, name, baseURL, apiKey string
}

type relayInfo struct {
	modelCount int
	fetchedAt  time.Time
	err        string
}

type relayPlan struct {
	mode   map[string]catalog.RelayMode
	models map[string][]catalog.RelayModel
}

func fetchAll(relays []relayConfig) relayPlan {
	p := cacheAll(relays)
	for _, r := range relays {
		p.mode[r.id] = catalog.FetchRemote
	}
	return p
}

// RelayMode 零值是 FetchRemote，每个配置都要显式填
func cacheAll(relays []relayConfig) relayPlan {
	p := relayPlan{mode: make(map[string]catalog.RelayMode, len(relays)), models: map[string][]catalog.RelayModel{}}
	for _, r := range relays {
		p.mode[r.id] = catalog.CacheOnly
	}
	return p
}

type relayState struct {
	relays       []relayConfig
	infos        map[string]relayInfo
	shown        []string
	known        bool
	routes       map[string]catalog.RelayRoute
	defaultModel string
	defaultRelay string
	levels       []string
}

func (s *Service) snapshot() relayState {
	return relayState{s.relays, s.relayInfos, s.relayShown, s.shownKnown, s.routes, s.defaultModel, s.defaultRelay, s.levels}
}

func (s *Service) rollback(c relayState) {
	s.relays, s.relayInfos, s.relayShown, s.shownKnown, s.routes, s.defaultModel, s.defaultRelay, s.levels = c.relays, c.infos, c.shown, c.known, c.routes, c.defaultModel, c.defaultRelay, c.levels
	s.pushRelay()
}

func (s *Service) relayIndex(id string) int {
	return slices.IndexFunc(s.relays, func(r relayConfig) bool { return r.id == id })
}

func (s *Service) validateRelayName(name string, selfIdx int) error {
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return &Error{Code: "name_empty"}
	case n > maxRelayNameRunes:
		return &Error{Code: "name_too_long"}
	case strings.Contains(name, "·"):
		return &Error{Code: "name_has_dot"}
	}
	for i, r := range s.relays {
		if i != selfIdx && strings.EqualFold(r.name, name) {
			return &Error{Code: "name_duplicate", Keys: []string{r.name}}
		}
	}
	return nil
}

func validateRelayID(id string) error {
	if n, err := strconv.Atoi(id); err != nil || n <= 0 || strconv.Itoa(n) != id {
		return &Error{Code: "bad_params"}
	}
	return nil
}

func relaysFromInput(in []RelayInput) ([]relayConfig, error) {
	if len(in) > maxRelays {
		return nil, &Error{Code: "bad_params"}
	}
	relays := make([]relayConfig, 0, len(in))
	seen := map[string]bool{}
	for _, r := range in {
		if err := validateRelayID(r.ID); err != nil || seen[r.ID] {
			return nil, &Error{Code: "bad_params"}
		}
		seen[r.ID] = true
		relays = append(relays, relayConfig{id: r.ID, name: r.Name, baseURL: r.BaseURL, apiKey: r.APIKey})
	}
	return relays, nil
}

func maxRelayNum(relays []relayConfig) int {
	m := 0
	for _, r := range relays {
		n, _ := strconv.Atoi(r.id)
		m = max(m, n)
	}
	return m
}

func trimTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Second)
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

func (s *Service) modelsCachePath() string {
	return filepath.Join(filepath.Dir(s.configPath), "models_cache.json")
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
		Relays:         s.relayStatuses(),
		DefaultRelay:   optional(s.defaultRelayName()),
		TakenOver:      append([]string{}, s.takenOver...),
		RestoreBlocked: append([]string{}, s.restoreBlocked...),
		ConfigPath:     s.configPath,
		Update:         s.update,
		Access:         AccessStatus{Mode: optional(s.accessMode), Target: optional(s.accessTarget), Detect: optional(s.accessDetect)},
	}
	return st
}

func (s *Service) relayStatuses() []RelayEntryStatus {
	out := make([]RelayEntryStatus, 0, len(s.relays))
	for _, r := range s.relays {
		info := s.relayInfos[r.id]
		e := RelayEntryStatus{ID: r.id, Name: r.name, ModelCount: info.modelCount, Stale: info.err != "", Error: optional(info.err)}
		if !info.fetchedAt.IsZero() {
			at := info.fetchedAt
			e.FetchedAt = &at
		}
		out = append(out, e)
	}
	return out
}

func (s *Service) defaultRelayName() string {
	if i := s.relayIndex(s.defaultRelay); i >= 0 {
		return s.relays[i].name
	}
	return ""
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
		return &Error{Code: code, Keys: catalog.ErrorKeys(err)}
	}
	return &Error{Code: "internal_error"}
}
