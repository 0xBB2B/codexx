package codexconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

const stateFileName = "access-state.json"

type entry struct {
	HadOriginal bool `json:"had_original"`
	Original    any  `json:"original"`
	// OriginalRaw 是原值在文件里的原文（被移除的键为整行），还原时原样放回
	OriginalRaw string `json:"original_raw,omitempty"`
	Written     any    `json:"written"`
	TakenOver   bool   `json:"taken_over"`
	// Line 记录被移除的键原本所在的行，还原时放回原位置
	Line *int `json:"line,omitempty"`
}

type State struct {
	Entries map[string]*entry `json:"entries"`
	// ConfigMissing 为 true 表示开启前配置文件不存在，还原到空内容时直接删除文件
	ConfigMissing bool `json:"config_missing,omitempty"`
}

func stateFilePath(c Config) string { return filepath.Join(c.DataDir, stateFileName) }

func LoadState(c Config) (*State, error) {
	b, err := os.ReadFile(stateFilePath(c))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func SaveState(c Config, st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.DataDir, stateFileName+".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), stateFilePath(c)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func removeState(c Config) error {
	if err := os.Remove(stateFilePath(c)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func TakenOverKeys(c Config) ([]string, error) {
	st, err := LoadState(c)
	if err != nil || st == nil {
		return nil, err
	}
	var keys []string
	for k, e := range st.Entries {
		if e.TakenOver {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys, nil
}
