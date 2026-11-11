package control

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const settingsFile = "settings.json"

type Settings struct {
	Forwarding bool `json:"forwarding"`
}

func LoadSettings(dataDir string) (Settings, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, settingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, nil
	}
	return s, nil
}

func SaveSettings(dataDir string, s Settings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, settingsFile+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
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
	return os.Rename(tmp.Name(), filepath.Join(dataDir, settingsFile))
}
