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
}

type Result struct {
	IsNewer       bool
	LatestVersion string
	URL           string
}

type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type RealClock struct{}

func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

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
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&rel); err != nil {
		return Result{}, err
	}

	latest, ok := ParseVersion(rel.TagName)
	if !ok {
		return Result{}, nil
	}
	current, ok := ParseVersion(cfg.CurrentVersion)
	if !ok || !latest.Newer(current) || !strings.HasPrefix(rel.HTMLURL, releaseURLBase) {
		return Result{}, nil
	}
	return Result{IsNewer: true, LatestVersion: strings.TrimPrefix(rel.TagName, "v"), URL: rel.HTMLURL}, nil
}

func Loop(ctx context.Context, clock Clock, check func(context.Context)) {
	delay := StartupDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock.After(delay):
		}
		if ctx.Err() != nil {
			return
		}
		check(ctx)
		delay = Interval
	}
}
