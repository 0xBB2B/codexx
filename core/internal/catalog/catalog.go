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
	// IDs 只在 UseGiven 时使用，空切片也照写缓存
	IDs []string
}

type BuildOptions struct {
	CodexPath    string
	Relays       []RelaySource
	RelayTimeout time.Duration
	DataDir      string
	// ModelsCachePath 是 Codex 的 models_cache.json，用来把官方条目限定在当前账号可用的模型
	ModelsCachePath string
	Params          []Param
	RelayOnly       bool
}

func Build(ctx context.Context, opts BuildOptions) (*Result, error) {
	official, err := LoadOfficial(ctx, opts.CodexPath)
	if err != nil {
		return nil, err
	}
	store := NewStore(opts.DataDir)

	type fetchOutcome struct {
		ids []string
		err error
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
			fetched[i].ids, fetched[i].err = FetchRelayIDs(ctx, src.BaseURL, src.Key, opts.RelayTimeout)
		}()
	}
	wg.Wait()

	var storeErr error
	var relays []RelayResult
	var groups []RelayGroup
	for i, src := range opts.Relays {
		r := RelayResult{ID: src.ID}
		var ids []string
		save := false
		switch src.Mode {
		case UseGiven:
			ids, save = src.IDs, true
			r.FetchedAt = time.Now()
		case CacheOnly:
			ids = loadCached(store, src.ID, &r)
		default:
			if err := fetched[i].err; err != nil {
				r.Err = err
				ids = loadCached(store, src.ID, &r)
				r.Stale = !r.FetchedAt.IsZero()
			} else {
				ids, save = fetched[i].ids, true
				r.FetchedAt = time.Now()
			}
		}
		if save {
			if err := store.SaveRelayModels(src.ID, ids, r.FetchedAt); err != nil && storeErr == nil {
				storeErr = err
			}
		}
		paramOnly, sameName := RelayNames(ids, opts.Params, official)
		r.ModelCount = len(paramOnly) + len(sameName)
		relays = append(relays, r)
		groups = append(groups, RelayGroup{ID: src.ID, Name: src.Name, ParamOnly: paramOnly, SameName: sameName})
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

func loadCached(store *Store, id string, r *RelayResult) []string {
	ids, at, ok, err := store.LoadRelayModels(id)
	if err != nil && r.Err == nil {
		r.Err = err
	}
	if err != nil || !ok {
		return nil
	}
	r.FetchedAt = at
	return ids
}
