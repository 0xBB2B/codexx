package codexconfig

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

const (
	CodeRestoreBlocked = "restore_blocked"
	CodeConfigInvalid  = "config_invalid"
	CodeConfigChanged  = "config_changed"

	keyBaseURL  = "openai_base_url"
	keyCatalog  = "model_catalog_json"
	keyModel    = "model"
	keyProvider = "model_provider"
	keyEfforts  = "desktop.enabled-reasoning-efforts"

	proxyBaseURL = "http://127.0.0.1:47311/v1"
	autoModel    = "auto"
)

type Config struct {
	ConfigPath string
	DataDir    string
	Replace    func(tmp, dst string) error

	beforeReplace func()
}

type EnableInput struct {
	CatalogPath  string
	HasAutoModel bool
	Efforts      []string
}

type Error struct {
	Code  string
	Keys  []string
	cause error
}

func (e *Error) Error() string {
	s := e.Code
	if len(e.Keys) > 0 {
		s += ": " + strings.Join(e.Keys, ", ")
	}
	if e.cause != nil {
		s += ": " + e.cause.Error()
	}
	return s
}

func (e *Error) Unwrap() error { return e.cause }

func readConfig(path string) (string, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	return string(b), err == nil, err
}

func atomicWrite(c Config, text, expected string) error {
	if err := Validate(text); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(c.ConfigPath); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.ConfigPath), "config.toml.tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(text)
	merr := tmp.Chmod(mode)
	cerr := tmp.Close()
	err = firstErr(werr, merr, cerr)
	if err == nil {
		err = checkUnchanged(c, expected)
	}
	if err == nil {
		replace := c.Replace
		if replace == nil {
			replace = os.Rename
		}
		err = replace(tmp.Name(), c.ConfigPath)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func checkUnchanged(c Config, expected string) error {
	if c.beforeReplace != nil {
		c.beforeReplace()
	}
	now, _, err := readConfig(c.ConfigPath)
	if err == nil && now != expected {
		err = &Error{Code: CodeConfigChanged}
	}
	return err
}

func typeOK(key string, it item) bool {
	if key == keyEfforts {
		return it.isArr
	}
	return it.isStr
}

func Enable(c Config, in EnableInput) ([]string, error) {
	link := c
	c.ConfigPath = resolvePath(c.ConfigPath)
	orig, existed, err := readConfig(c.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := Validate(orig); err != nil {
		return nil, err
	}
	managed := []string{keyBaseURL, keyCatalog, keyProvider}
	if in.HasAutoModel {
		managed = append(managed, keyModel)
	}
	if len(in.Efforts) > 0 {
		managed = append(managed, keyEfforts)
	}
	if len(in.Efforts) > 0 {
		if _, plain, err := get(orig, "desktop"); err != nil {
			return nil, err
		} else if plain {
			return nil, &Error{Code: CodeConfigInvalid, Keys: []string{keyEfforts}}
		}
	}
	for _, key := range managed {
		it, had, err := get(orig, key)
		if err != nil {
			return nil, err
		}
		if had && !typeOK(key, it) {
			return nil, &Error{Code: CodeConfigInvalid, Keys: []string{key}}
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return nil, err
	}
	if err := EnsureInitialBackup(link); err != nil {
		return nil, err
	}
	st, err := LoadState(c)
	if err != nil {
		return nil, err
	}
	prev, err := LoadState(c)
	if err != nil {
		return nil, err
	}
	badSet := map[string]bool{}
	var modelEntry *entry
	if st == nil {
		st = &State{Entries: map[string]*entry{}, ConfigMissing: !existed}
	} else {
		modelEntry = st.Entries[keyModel]
		bad, err := tampered(orig, st)
		if err != nil {
			return nil, err
		}
		for _, k := range bad {
			badSet[k] = true
			delete(st.Entries, k)
		}
	}

	text, eol := orig, detectEOL(orig)
	var taken []string
	if modelEntry != nil && !in.HasAutoModel {
		switch {
		case badSet[keyModel]:
			taken = append(taken, keyModel)
		case modelEntry.HadOriginal:
			text, err = setValue(text, keyModel, modelEntry.OriginalRaw, eol)
		default:
			text, _, _, err = removeKey(text, keyModel)
		}
		if err != nil {
			return nil, err
		}
		delete(st.Entries, keyModel)
	}
	record := func(key string, original any, raw string, had bool, written any, takenOver bool, line *int) {
		if e, ok := st.Entries[key]; ok {
			e.Written = written
			return
		}
		takenOver = takenOver || (had && badSet[key])
		st.Entries[key] = &entry{HadOriginal: had, Original: original, OriginalRaw: raw, Written: written, TakenOver: takenOver, Line: line}
		if takenOver {
			taken = append(taken, key)
		}
	}
	origStr := func(it item, had bool) any {
		if had {
			return it.str
		}
		return nil
	}
	rawOf := func(it item, had bool) string {
		if had {
			return text[it.valStart:it.valEnd]
		}
		return ""
	}

	it, had, err := get(text, keyProvider)
	if err != nil {
		return nil, err
	}
	var line *int
	var removed string
	if had {
		var l int
		if text, l, removed, err = removeKey(text, keyProvider); err != nil {
			return nil, err
		}
		line = &l
	}
	record(keyProvider, origStr(it, had), removed, had, nil, had, line)

	for _, kv := range []struct{ key, val string }{{keyBaseURL, proxyBaseURL}, {keyCatalog, in.CatalogPath}} {
		it, had, err := get(text, kv.key)
		if err != nil {
			return nil, err
		}
		raw := rawOf(it, had)
		if text, err = setString(text, kv.key, kv.val, eol); err != nil {
			return nil, err
		}
		record(kv.key, origStr(it, had), raw, had, kv.val, had, nil)
	}

	if in.HasAutoModel {
		it, had, err := get(text, keyModel)
		if err != nil {
			return nil, err
		}
		raw := rawOf(it, had)
		if text, err = setString(text, keyModel, autoModel, eol); err != nil {
			return nil, err
		}
		record(keyModel, origStr(it, had), raw, had, autoModel, false, nil)
	}

	if len(in.Efforts) > 0 {
		it, had, err := get(text, keyEfforts)
		if err != nil {
			return nil, err
		}
		merged := slices.Clone(it.strs)
		for _, e := range in.Efforts {
			if !slices.Contains(merged, e) {
				merged = append(merged, e)
			}
		}
		changed := !had || len(merged) != len(it.strs)
		raw := rawOf(it, had)
		if changed {
			if text, err = setStrings(text, keyEfforts, merged, eol); err != nil {
				return nil, err
			}
		}
		if _, prior := st.Entries[keyEfforts]; changed || prior || badSet[keyEfforts] {
			var original any
			if had {
				original = it.strs
			}
			record(keyEfforts, original, raw, had, merged, false, nil)
		}
	}

	if err := SaveState(c, st); err != nil {
		return nil, err
	}
	if err := atomicWrite(c, text, orig); err != nil {
		var rerr error
		if prev != nil {
			rerr = SaveState(c, prev)
		} else {
			rerr = removeState(c)
		}
		return nil, errors.Join(err, rerr)
	}
	return taken, nil
}

func tampered(text string, st *State) ([]string, error) {
	var bad []string
	for key, e := range st.Entries {
		it, present, err := get(text, key)
		if err != nil {
			return nil, err
		}
		var ok bool
		switch w := e.Written.(type) {
		case nil:
			ok = !present
		case string:
			ok = present && it.isStr && it.str == w
		default:
			ok = present && it.isArr && slices.Equal(it.strs, strsOf(w))
		}
		if !ok {
			bad = append(bad, key)
		}
	}
	slices.Sort(bad)
	return bad, nil
}

func strsOf(v any) []string {
	switch a := v.(type) {
	case []string:
		return a
	case []any:
		out := make([]string, len(a))
		for i, x := range a {
			out[i], _ = x.(string)
		}
		return out
	}
	return nil
}

func CheckTampered(c Config) ([]string, error) {
	c.ConfigPath = resolvePath(c.ConfigPath)
	st, err := LoadState(c)
	if err != nil || st == nil {
		return nil, err
	}
	text, _, err := readConfig(c.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := Validate(text); err != nil {
		return nil, err
	}
	return tampered(text, st)
}

func Restore(c Config) error { return restore(c, false) }

func RestoreAll(c Config) error { return restore(c, true) }

func restore(c Config, all bool) error {
	c.ConfigPath = resolvePath(c.ConfigPath)
	st, err := LoadState(c)
	if err != nil || st == nil {
		return err
	}
	orig, _, err := readConfig(c.ConfigPath)
	if err != nil {
		return err
	}
	if err := Validate(orig); err != nil {
		return err
	}
	bad, err := tampered(orig, st)
	if err != nil {
		return err
	}
	if all && len(bad) > 0 {
		return &Error{Code: CodeRestoreBlocked, Keys: bad}
	}

	text, eol := orig, detectEOL(orig)
	var reinsert []string
	for _, key := range slices.Sorted(maps.Keys(st.Entries)) {
		if slices.Contains(bad, key) {
			continue
		}
		e := st.Entries[key]
		switch {
		case !e.HadOriginal:
			text, _, _, err = removeKey(text, key)
		case e.Written == nil:
			reinsert = append(reinsert, key)
		default:
			text, err = setValue(text, key, e.OriginalRaw, eol)
		}
		if err != nil {
			return err
		}
	}
	for _, key := range reinsert {
		e := st.Entries[key]
		at, err := rootOffset(text)
		if err != nil {
			return err
		}
		if e.Line != nil {
			at = min(at, lineOff(text, *e.Line))
		}
		text = splice(text, at, e.OriginalRaw, eol)
	}

	if text != orig {
		if st.ConfigMissing && text == "" {
			if err = checkUnchanged(c, orig); err == nil {
				err = os.Remove(c.ConfigPath)
			}
		} else {
			err = atomicWrite(c, text, orig)
		}
		if err != nil {
			return err
		}
	}

	if len(bad) == 0 {
		return removeState(c)
	}
	remaining := map[string]*entry{}
	for _, k := range bad {
		remaining[k] = st.Entries[k]
	}
	if !reflect.DeepEqual(remaining, st.Entries) {
		st.Entries = remaining
		if err := SaveState(c, st); err != nil {
			return err
		}
	}
	return &Error{Code: CodeRestoreBlocked, Keys: bad}
}
