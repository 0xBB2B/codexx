package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	legacyRelayModelsFile = "relay-models.json"
	catalogFile           = "model-bridge-catalog.json"
)

type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) SaveRelayModels(id string, ids []string, at time.Time) error {
	return s.writeJSON(relayModelsFileName(id), map[string]any{
		"fetched_at": at.UTC().Format(time.RFC3339),
		"ids":        ids,
	})
}

// 编号 1 的新缓存不存在时退回旧版单文件缓存
func (s *Store) LoadRelayModels(id string) ([]string, time.Time, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, relayModelsFileName(id)))
	if errors.Is(err, os.ErrNotExist) && id == "1" {
		raw, err = os.ReadFile(filepath.Join(s.dir, legacyRelayModelsFile))
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	var file struct {
		FetchedAt time.Time `json:"fetched_at"`
		IDs       []string  `json:"ids"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, time.Time{}, false, err
	}
	return file.IDs, file.FetchedAt, true, nil
}

func relayModelsFileName(id string) string { return "relay-models-" + id + ".json" }

func (s *Store) WriteCatalog(models []Entry) error {
	return s.writeJSON(catalogFile, map[string]any{"models": models})
}

func (s *Store) writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, name+".*.tmp")
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
	return os.Rename(tmp.Name(), filepath.Join(s.dir, name))
}
