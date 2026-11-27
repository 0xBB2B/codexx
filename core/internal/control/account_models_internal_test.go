package control

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return nil, errors.New("blocked in test")
}

func TestAccountModels_EmptyOfficialURLQueriesDefaultAddress(t *testing.T) {
	rec := &recordingTransport{}
	orig := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = orig })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cache := `{"client_version":"0.130.0-alpha.2","models":[]}`
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewService(Options{DataDir: filepath.Join(dir, "data"), ConfigPath: configPath})

	h := http.Header{}
	h.Set("Authorization", "Bearer tok-example-aaa")
	h.Set("ChatGPT-Account-ID", "acct-example-aaa")
	s.syncAccountModels(h)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := "https://chatgpt.com/backend-api/codex/models?client_version=0.130.0"
	if len(rec.urls) != 1 || rec.urls[0] != want {
		t.Errorf("查询地址 = %v, want [%s]", rec.urls, want)
	}
}
