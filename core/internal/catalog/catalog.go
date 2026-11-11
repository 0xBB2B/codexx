package catalog

import (
	"context"
	"time"
)

type BuildOptions struct {
	CodexPath    string
	RelayBaseURL string
	RelayKey     string
	RelayTimeout time.Duration
	DataDir      string
	Params       []Param
}

func Build(ctx context.Context, opts BuildOptions) (*Result, error) {
	official, err := LoadOfficial(ctx, opts.CodexPath)
	if err != nil {
		return nil, err
	}
	store := NewStore(opts.DataDir)

	var storeErr error
	var fetchedAt time.Time
	ids, relayErr := FetchRelayIDs(ctx, opts.RelayBaseURL, opts.RelayKey, opts.RelayTimeout)
	if relayErr == nil {
		fetchedAt = time.Now()
		storeErr = store.SaveRelayModels(ids, fetchedAt)
	} else if recorded, at, ok, err := store.LoadRelayModels(); err == nil && ok {
		ids, fetchedAt = recorded, at
	} else {
		ids = nil
	}

	res, err := Merge(official, SelectRelay(ids, opts.Params))
	if err != nil {
		return nil, err
	}
	res.RelayErr = relayErr
	res.StoreErr = storeErr
	res.RelayFetchedAt = fetchedAt
	if err := store.WriteCatalog(res.Models); err != nil {
		return nil, err
	}
	return res, nil
}
