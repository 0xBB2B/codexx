package codexconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type stateEntry struct {
	HadOriginal bool `json:"had_original"`
	Original    any  `json:"original"`
	Written     any  `json:"written"`
	TakenOver   bool `json:"taken_over"`
}

type stateFile struct {
	Entries map[string]stateEntry `json:"entries"`
}

func statePath(c Config) string { return filepath.Join(c.DataDir, "access-state.json") }

func readState(t *testing.T, c Config) stateFile {
	t.Helper()
	var sf stateFile
	if err := json.Unmarshal([]byte(readFile(t, statePath(c))), &sf); err != nil {
		t.Fatalf("access-state.json 不是合法 JSON: %v", err)
	}
	return sf
}

func entryKeys(sf stateFile) []string {
	var ks []string
	for k := range sf.Entries {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestEnable_StateRecordsOriginalAndWritten(t *testing.T) {
	orig := "openai_base_url = \"" + relayURL + "\"\nmodel = \"gpt-x\"\n"
	c, _ := newFixture(t, sample{content: orig})
	cat := catalogPath(c)
	if _, err := Enable(c, EnableInput{CatalogPath: cat, HasAutoModel: true, Efforts: []string{"none"}}); err != nil {
		t.Fatal(err)
	}
	sf := readState(t, c)

	wantKeys := []string{"desktop.enabled-reasoning-efforts", "model", "model_catalog_json", "model_provider", "openai_base_url"}
	if got := entryKeys(sf); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("entries 键 = %v, 期望 %v", got, wantKeys)
	}

	e := sf.Entries["openai_base_url"]
	if !e.HadOriginal || !e.TakenOver || e.Written != proxyURL || !strings.Contains(toString(e.Original), "relay.example.com") {
		t.Fatalf("openai_base_url 记录 = %+v", e)
	}
	e = sf.Entries["model_catalog_json"]
	if e.HadOriginal || e.Original != nil || e.Written != cat || e.TakenOver {
		t.Fatalf("model_catalog_json 记录 = %+v", e)
	}
	e = sf.Entries["model"]
	if !e.HadOriginal || e.Written != "auto" || e.TakenOver {
		t.Fatalf("model 记录 = %+v", e)
	}
	e = sf.Entries["model_provider"]
	if e.HadOriginal || e.Original != nil || e.Written != nil || e.TakenOver {
		t.Fatalf("model_provider 记录 = %+v", e)
	}
	if e = sf.Entries["desktop.enabled-reasoning-efforts"]; e.HadOriginal || e.Written == nil {
		t.Fatalf("efforts 记录 = %+v", e)
	}
}

func toString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEnable_StateOmitsUnusedKeys(t *testing.T) {
	c, _ := newFixture(t, sample{content: "a = 1\n"})
	if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
		t.Fatal(err)
	}
	want := []string{"model_catalog_json", "model_provider", "openai_base_url"}
	if got := entryKeys(readState(t, c)); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries 键 = %v, 期望 %v", got, want)
	}
}

func TestEnable_ProviderRemovedRecordedAsTakenOver(t *testing.T) {
	c, _ := newFixture(t, sample{content: "model_provider = \"relay\"\n"})
	if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
		t.Fatal(err)
	}
	e := readState(t, c).Entries["model_provider"]
	if !e.HadOriginal || !e.TakenOver || e.Written != nil || !strings.Contains(toString(e.Original), "relay") {
		t.Fatalf("model_provider 记录 = %+v", e)
	}
}

func TestEnable_Twice_KeepsOriginalAndNoTakeover(t *testing.T) {
	orig := "a = 1\n"
	c, cfg := newFixture(t, sample{content: orig})
	in := EnableInput{CatalogPath: catalogPath(c), HasAutoModel: true}
	if _, err := Enable(c, in); err != nil {
		t.Fatal(err)
	}
	got, err := Enable(c, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("重复开启不应再报接管: %v", got)
	}
	if e := readState(t, c).Entries["openai_base_url"]; e.HadOriginal || e.TakenOver {
		t.Fatalf("重复开启后原值记录被覆盖: %+v", e)
	}
	if err := Restore(c); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); got != orig {
		t.Fatalf("还原后 = %q, 期望 %q", got, orig)
	}
}

func TestEnable_Backup(t *testing.T) {
	t.Run("首次开启复制整份配置", func(t *testing.T) {
		orig := "# c\na = 1\n[t]\nb = 2\n"
		c, cfg := newFixture(t, sample{content: orig})
		if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, cfg+".before-model-bridge"); got != orig {
			t.Fatalf("备份 = %q", got)
		}
	})
	t.Run("备份已存在时跳过", func(t *testing.T) {
		c, cfg := newFixture(t, sample{content: "a = 1\n"})
		if err := os.WriteFile(cfg+".before-model-bridge", []byte("旧备份"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, cfg+".before-model-bridge"); got != "旧备份" {
			t.Fatalf("备份被覆盖: %q", got)
		}
	})
	t.Run("配置文件不存在时写空文件", func(t *testing.T) {
		c, cfg := newFixture(t, sample{missing: true})
		if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, cfg+".before-model-bridge"); got != "" {
			t.Fatalf("备份应为空文件: %q", got)
		}
	})
}

func TestTakenOverKeys(t *testing.T) {
	c, _ := newFixture(t, sample{content: "model_provider = \"relay\"\nopenai_base_url = \"https://old.example.com/v1\"\n"})
	keys, err := TakenOverKeys(c)
	if err != nil || len(keys) != 0 {
		t.Fatalf("无状态文件时 = %v, %v", keys, err)
	}
	if _, err := Enable(c, EnableInput{CatalogPath: catalogPath(c)}); err != nil {
		t.Fatal(err)
	}
	keys, err = TakenOverKeys(c)
	if err != nil || !reflect.DeepEqual(keys, []string{"model_provider", "openai_base_url"}) {
		t.Fatalf("TakenOverKeys = %v, %v", keys, err)
	}
}
