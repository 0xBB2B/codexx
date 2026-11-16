package main

import (
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	exampleKey    = "sk-example-0000"
	trailingPunct = ".,;:!?)]}"
	cjkPunct      = "。，；：！？）】」、"
)

type Violation struct {
	File   string
	Line   int
	Reason string
}

var (
	schemePattern = regexp.MustCompile(`(?i)https?://`)
	keyPattern    = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(sk-[A-Za-z0-9_-]+)`)
)

func Check(dir string) ([]Violation, error) {
	var out []Violation
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(data), "\n") {
			for _, reason := range checkLine(line) {
				out = append(out, Violation{File: rel, Line: i + 1, Reason: reason})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

func checkLine(line string) []string {
	var reasons []string
	for rest := line; ; {
		loc := schemePattern.FindStringIndex(rest)
		if loc == nil {
			break
		}
		raw, end, ok := scanURL(rest[loc[0]:])
		rest = rest[loc[0]+end:]
		if !ok || !allowedURL(strings.TrimRight(raw, trailingPunct)) {
			reasons = append(reasons, "网址不在白名单内")
		}
	}
	for _, m := range keyPattern.FindAllStringSubmatch(line, -1) {
		if m[1] != exampleKey {
			reasons = append(reasons, "Key 不是示例 Key")
		}
	}
	return reasons
}

// 返回网址文本、网址在 s 中的结束位置；ok 为 false 表示网址里混入了非标点的非 ASCII 字符。
func scanURL(s string) (raw string, end int, ok bool) {
	for i, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\r' || r == '"' || r == '\'' || r == '<' || r == '>' || unicode.IsSpace(r):
			return s[:i], i, true
		case r < utf8.RuneSelf:
		case !strings.ContainsRune(cjkPunct, r):
			return s, len(s), false
		default:
			after := s[i+utf8.RuneLen(r):]
			next, _ := utf8.DecodeRuneInString(after)
			if loc := schemePattern.FindStringIndex(after); after == "" || unicode.IsSpace(next) || next >= utf8.RuneSelf || loc != nil && loc[0] == 0 {
				return s[:i], i, true
			}
			return s, len(s), false
		}
	}
	return s, len(s), true
}

func allowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "relay.example.com", "127.0.0.1":
		return true
	case "github.com":
		for _, seg := range strings.Split(u.Path, "/") {
			if seg == "." || seg == ".." {
				return false
			}
		}
		return u.Path == "/0xBB2B/codexx" || strings.HasPrefix(u.Path, "/0xBB2B/codexx/")
	}
	return false
}
