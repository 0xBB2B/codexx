package catalog

import (
	"context"
	"sync"
	"time"
)

type RelayMode int

const (
	FetchRemote RelayMode = iota
	CacheOnly
	UseGiven
)

type RelaySource struct {
	ID      string
	Name    string
	BaseURL string
	Key     string
	Mode    RelayMode
	// Models 只在 UseGiven 时使用，空切片也照写缓存
	Models []RelayModel
}

type BuildOptions struct {
	CodexPath    string
	Relays       []RelaySource
	RelayTimeout time.Duration
	DataDir      string
	// ModelsCachePath 是 Codex 的 models_cache.json，用来把官方条目限定在当前账号可用的模型
	ModelsCachePath string
	RelayOnly       bool
}

func Build(ctx context.Context, opts BuildOptions) (*Result, error) {
	official, err := LoadOfficial(ctx, opts.CodexPath)
	if err != nil {
		return nil, err
	}
	store := NewStore(opts.DataDir)

	type fetchOutcome struct {
		models []RelayModel
		err    error
	}
	fetched := make([]fetchOutcome, len(opts.Relays))
	var wg sync.WaitGroup
	for i, src := range opts.Relays {
		if src.Mode != FetchRemote {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetched[i].models, fetched[i].err = FetchRelayModels(ctx, src.BaseURL, src.Key, opts.RelayTimeout)
		}()
	}
	wg.Wait()

	var storeErr error
	var relays []RelayResult
	var groups []RelayGroup
	for i, src := range opts.Relays {
		r := RelayResult{ID: src.ID}
		var models []RelayModel
		save := false
		switch src.Mode {
		case UseGiven:
			models, save = src.Models, true
			r.FetchedAt = time.Now()
		case CacheOnly:
			models = loadCached(store, src.ID, &r)
		default:
			if err := fetched[i].err; err != nil {
				r.Err = err
				models = loadCached(store, src.ID, &r)
				r.Stale = !r.FetchedAt.IsZero()
			} else {
				models, save = fetched[i].models, true
				r.FetchedAt = time.Now()
			}
		}
		if save {
			if err := store.SaveRelayModels(src.ID, models, r.FetchedAt); err != nil && storeErr == nil {
				storeErr = err
			}
		}
		r.ModelCount = len(ShownRelayNames(models, official))
		relays = append(relays, r)
		groups = append(groups, RelayGroup{ID: src.ID, Name: src.Name, Models: models})
	}

	res, err := MergeRelay(official, groups)
	if err != nil {
		return nil, err
	}
	if opts.RelayOnly {
		res.Models = res.Models[len(official):]
	} else {
		res.Models = append(FilterOfficial(official, AccountSlugs(opts.ModelsCachePath)), res.Models[len(official):]...)
	}
	for _, src := range opts.Relays {
		if _, ok := res.Routes[RelaySlug(src.ID, "auto")]; ok {
			res.DefaultModel = RelaySlug(src.ID, "auto")
			break
		}
	}
	res.Relays = relays
	res.StoreErr = storeErr
	if err := store.WriteCatalog(res.Models); err != nil {
		return nil, err
	}
	return res, nil
}

func loadCached(store *Store, id string, r *RelayResult) []RelayModel {
	models, at, ok, err := store.LoadRelayModels(id)
	if err != nil && r.Err == nil {
		r.Err = err
	}
	if err != nil || !ok {
		return nil
	}
	r.FetchedAt = at
	return models
}
