package codexconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
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
	// RelayOnly 记录写入时用的方式；缺省视为官方加中转
	RelayOnly bool `json:"relay_only,omitempty"`
	// PriorSum 是本次 Enable 写配置之前配置文本的摘要，Previous 是那时生效的状态。
	// 先存状态后写配置，被打断时配置还是旧文本，摘要对得上就按 Previous 还原
	PriorSum string `json:"prior_sum,omitempty"`
	Previous *State `json:"previous,omitempty"`
}

func sumOf(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func effective(text string, st *State) *State {
	if st != nil && st.PriorSum != "" && st.PriorSum == sumOf(text) {
		return st.Previous
	}
	return st
}

func cloneState(st *State) *State {
	if st == nil {
		return nil
	}
	b, _ := json.Marshal(st)
	var out State
	_ = json.Unmarshal(b, &out)
	out.PriorSum, out.Previous = "", nil
	return &out
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
	if st.Entries == nil {
		st.Entries = map[string]*entry{}
	}
	maps.DeleteFunc(st.Entries, func(_ string, e *entry) bool { return e == nil })
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
