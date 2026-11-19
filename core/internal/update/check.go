package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	StartupDelay = time.Minute
	Interval     = 24 * time.Hour
	pollStep     = time.Minute

	requestTimeout = 10 * time.Second
	maxBodyBytes   = 1 << 20
	releaseURLBase = "https://github.com/"
)

var repository string

func Repository() string { return repository }

type Config struct {
	APIBase        string
	Repository     string
	CurrentVersion string
	AssetSuffix    string
}

type Result struct {
	IsNewer       bool
	LatestVersion string
	URL           string
	Installer     bool
}

type Clock interface {
	After(d time.Duration) <-chan time.Time
	Now() time.Time
}

type RealClock struct{}

func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (RealClock) Now() time.Time { return time.Now() }

func Check(ctx context.Context, cfg Config) (Result, error) {
	if cfg.Repository == "" {
		return Result{}, nil
	}
	endpoint := strings.TrimRight(cfg.APIBase, "/") + "/repos/" + cfg.Repository + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "model-bridge/"+cfg.CurrentVersion)

	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("release 接口返回 %d", resp.StatusCode)
	}
	var rel struct {
		TagName string  `json:"tag_name"`
		HTMLURL string  `json:"html_url"`
		Assets  []asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&rel); err != nil {
		return Result{}, err
	}

	latest, ok := ParseVersion(rel.TagName)
	if !ok {
		return Result{}, nil
	}
	current, preRelease, ok := parseCurrent(cfg.CurrentVersion)
	if !ok || !(latest.Newer(current) || preRelease && latest == current) || !strings.HasPrefix(rel.HTMLURL, releaseURLBase) {
		return Result{}, nil
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	res := Result{IsNewer: true, LatestVersion: version, URL: rel.HTMLURL}
	if u, ok := installerURL(rel.Assets, version, cfg.AssetSuffix); ok {
		res.URL, res.Installer = u, true
	}
	return res, nil
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func installerURL(assets []asset, version, suffix string) (string, bool) {
	if suffix == "" {
		return "", false
	}
	want := "model-bridge-" + version + suffix
	for _, a := range assets {
		if a.Name == want && strings.HasPrefix(a.URL, releaseURLBase) {
			return a.URL, true
		}
	}
	return "", false
}

func Loop(ctx context.Context, clock Clock, check func(context.Context)) {
	delay := StartupDelay
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock.After(delay):
		}
		if ctx.Err() != nil {
			return
		}
		if !last.IsZero() {
			now := clock.Now().Round(0)
			elapsed := now.Sub(last)
			if elapsed < 0 {
				last = now
			}
			if elapsed < Interval {
				delay = pollStep
				continue
			}
		}
		check(ctx)
		last = clock.Now().Round(0)
		delay = pollStep
	}
}
