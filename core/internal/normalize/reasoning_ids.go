package normalize

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	MaxReasoningIDs = 5000

	reasoningIDsFile = "relay-reasoning-ids.json"
)

type ReasoningIDs struct {
	mu    sync.Mutex
	dir   string
	order []string
	set   map[string]struct{}
}

func OpenReasoningIDs(dir string) (*ReasoningIDs, error) {
	s := &ReasoningIDs{dir: dir, set: map[string]struct{}{}}
	if dir == "" {
		return s, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, reasoningIDsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if json.Unmarshal(b, &ids) != nil {
		return s, nil
	}
	for _, id := range ids {
		if _, ok := s.set[id]; !ok {
			s.set[id] = struct{}{}
			s.order = append(s.order, id)
		}
	}
	s.evict()
	return s, nil
}

func (s *ReasoningIDs) evict() {
	for len(s.order) > MaxReasoningIDs {
		delete(s.set, s.order[0])
		s.order = s.order[1:]
	}
}

func (s *ReasoningIDs) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[id]
	return ok
}

func (s *ReasoningIDs) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.order)
}

func (s *ReasoningIDs) Add(ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	added := false
	for _, id := range ids {
		if _, ok := s.set[id]; ok {
			continue
		}
		s.set[id] = struct{}{}
		s.order = append(s.order, id)
		added = true
	}
	if !added {
		return nil
	}
	s.evict()
	return s.save()
}

func (s *ReasoningIDs) save() error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s.order)
	tmp, err := os.CreateTemp(s.dir, reasoningIDsFile+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, reasoningIDsFile)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

type reasoningTap struct {
	src io.Reader
	ids *ReasoningIDs
	buf []byte
}

func TapReasoningIDs(r io.Reader, ids *ReasoningIDs) io.Reader {
	return &reasoningTap{src: r, ids: ids}
}

func (t *reasoningTap) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 {
		t.buf = append(t.buf, p[:n]...)
		t.scan()
	}
	return n, err
}

func (t *reasoningTap) scan() {
	var found []string
	for {
		i, sepLen := nextEventEnd(t.buf)
		if i < 0 {
			break
		}
		found = append(found, reasoningIDOf(string(t.buf[:i]))...)
		t.buf = t.buf[i+sepLen:]
	}
	if len(found) > 0 {
		_ = t.ids.Add(found...)
	}
}

func nextEventEnd(b []byte) (idx, sepLen int) {
	lf := bytes.Index(b, []byte("\n\n"))
	crlf := bytes.Index(b, []byte("\r\n\r\n"))
	switch {
	case lf < 0 && crlf < 0:
		return -1, 0
	case crlf < 0 || (lf >= 0 && lf < crlf):
		return lf, 2
	default:
		return crlf, 4
	}
}

func reasoningIDOf(block string) []string {
	var name string
	var data []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	var v struct {
		Type string   `json:"type"`
		Item itemHead `json:"item"`
	}
	if json.Unmarshal([]byte(strings.Join(data, "\n")), &v) != nil {
		return nil
	}
	if name == "" {
		name = v.Type
	}
	if name != "response.output_item.done" || v.Item.Type != "reasoning" || v.Item.ID == "" {
		return nil
	}
	return []string{v.Item.ID}
}
