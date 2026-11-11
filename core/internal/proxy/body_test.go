package proxy_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func zstdEncode(t *testing.T, raw []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	return enc.EncodeAll(raw, nil)
}

func TestBody_ZstdRelay_DecompressedAndEncodingDropped(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"` + relayModel + `","input":"hello"}`)
	e := newEnv(t, nil)
	hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
	resp, _ := e.do(t, http.MethodPost, "/v1/responses", hdr, zstdEncode(t, raw))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	hit := e.relay.onlyHit(t)
	if !bytes.Equal(hit.Body, raw) {
		t.Fatalf("中台收到的请求体 = %q，期望解压后 %q", hit.Body, raw)
	}
	if v := hit.Header.Get("Content-Encoding"); v != "" {
		t.Fatalf("中台不应收到 Content-Encoding，实际 %q", v)
	}
	if n := len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
		t.Fatalf("官方不应收到请求，实际 %d 次", n)
	}
}

func TestBody_PlainRelay_BodyUnchanged(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"` + relayModel + `","input":"hello"}`)
	e := newEnv(t, nil)
	e.do(t, http.MethodPost, "/v1/responses", map[string]string{"Content-Type": "application/json"}, raw)
	if hit := e.relay.onlyHit(t); !bytes.Equal(hit.Body, raw) {
		t.Fatalf("中台收到的请求体 = %q", hit.Body)
	}
}

func TestBody_ZstdOfficial_ForwardedByteForByte(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"official-model","input":"hello"}`)
	compressed := zstdEncode(t, raw)
	tests := []struct {
		name        string
		withAccount bool
	}{
		{"chatgpt", true},
		{"api", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
			if tt.withAccount {
				hdr["ChatGPT-Account-ID"] = accountID
			}
			e.do(t, http.MethodPost, "/v1/responses", hdr, compressed)
			target := e.api
			if tt.withAccount {
				target = e.chatgpt
			}
			hit := target.onlyHit(t)
			if !bytes.Equal(hit.Body, compressed) {
				t.Fatalf("官方收到的请求体与原压缩体不一致")
			}
			if hit.Header.Get("Content-Encoding") != "zstd" {
				t.Fatalf("官方收到的 Content-Encoding = %q", hit.Header.Get("Content-Encoding"))
			}
		})
	}
}

func TestBody_ZstdOverLimit_Rejected(t *testing.T) {
	t.Parallel()
	const limit = 64 << 20
	head := []byte(`{"model":"` + relayModel + `","pad":"`)
	tail := []byte(`"}`)
	raw := append(append(append([]byte{}, head...), bytes.Repeat([]byte("a"), limit)...), tail...)

	e := newEnv(t, nil)
	hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
	resp, _ := e.do(t, http.MethodPost, "/v1/responses", hdr, zstdEncode(t, raw))
	if resp.StatusCode < 400 {
		t.Fatalf("解压后超过 64MB 应被拒绝，实际状态码 %d", resp.StatusCode)
	}
	if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
		t.Fatalf("超限请求不应到达任何上游，实际 %d 次", n)
	}
}

func TestBody_OverLimit_413(t *testing.T) {
	t.Parallel()
	const limit = 64 << 20
	head := []byte(`{"model":"` + relayModel + `","pad":"`)
	raw := append(append(append([]byte{}, head...), bytes.Repeat([]byte("a"), limit)...), '"', '}')
	tests := []struct {
		name string
		hdr  map[string]string
		body []byte
	}{
		{"未压缩", map[string]string{"Content-Type": "application/json"}, raw},
		{"zstd", map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}, zstdEncode(t, raw)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, nil)
			resp, _ := e.do(t, http.MethodPost, "/v1/responses", tt.hdr, tt.body)
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("状态码 = %d，期望 413", resp.StatusCode)
			}
			if n := len(e.relay.Hits()) + len(e.api.Hits()) + len(e.chatgpt.Hits()); n != 0 {
				t.Fatalf("超限请求不应到达任何上游，实际 %d 次", n)
			}
		})
	}
}

func TestBody_BadZstd_ForwardedOfficialAsIs(t *testing.T) {
	t.Parallel()
	bad := []byte("this-is-not-zstd")
	e := newEnv(t, nil)
	hdr := map[string]string{"Content-Type": "application/json", "Content-Encoding": "zstd"}
	resp, _ := e.do(t, http.MethodPost, "/v1/responses", hdr, bad)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	hit := e.api.onlyHit(t)
	if !bytes.Equal(hit.Body, bad) {
		t.Fatalf("官方收到的请求体 = %q，期望原始字节", hit.Body)
	}
	if n := len(e.relay.Hits()); n != 0 {
		t.Fatalf("中台不应收到请求，实际 %d 次", n)
	}
}
