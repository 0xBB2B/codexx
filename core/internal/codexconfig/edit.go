package codexconfig

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type item struct {
	header           bool
	path             string
	line, end        int
	valStart, valEnd int
	isStr, isArr     bool
	str              string
	strs             []string
}

func scan(text string) ([]item, error) {
	var p unstable.Parser
	p.Reset([]byte(text))
	lineOf := func(off int) int { return strings.Count(text[:off], "\n") }

	var items []item
	var table []string
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			parts, first, _ := keyOf(n)
			table = parts
			items = append(items, item{header: true, path: strings.Join(parts, "."), line: lineOf(int(first.Offset))})
		case unstable.KeyValue:
			parts, _, last := keyOf(n)
			it := item{path: strings.Join(append(append([]string{}, table...), parts...), ".")}
			start, end := int(n.Raw.Offset), int(n.Raw.Offset+n.Raw.Length)
			for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
				end--
			}
			it.line, it.end, it.valEnd = lineOf(start), lineOf(end-1), end
			vs := int(last.Offset + last.Length)
			for text[vs] == ' ' || text[vs] == '\t' {
				vs++
			}
			vs++
			for text[vs] == ' ' || text[vs] == '\t' {
				vs++
			}
			it.valStart = vs
			v := n.Value()
			switch v.Kind {
			case unstable.String:
				it.isStr, it.str = true, string(v.Data)
			case unstable.Array:
				it.isArr, it.strs = true, []string{}
				for c := v.Children(); c.Next(); {
					if c.Node().Kind != unstable.String {
						it.isArr = false
						break
					}
					it.strs = append(it.strs, string(c.Node().Data))
				}
			}
			items = append(items, it)
		}
	}
	return items, p.Error()
}

func keyOf(n *unstable.Node) (parts []string, first, last unstable.Range) {
	for it := n.Key(); it.Next(); {
		k := it.Node()
		if len(parts) == 0 {
			first = k.Raw
		}
		parts = append(parts, string(k.Data))
		last = k.Raw
	}
	return parts, first, last
}

func get(text, path string) (item, bool, error) {
	items, err := scan(text)
	if err != nil {
		return item{}, false, err
	}
	for _, it := range items {
		if !it.header && it.path == path {
			return it, true, nil
		}
	}
	return item{}, false, nil
}

func Validate(text string) error {
	var m map[string]any
	if err := toml.Unmarshal([]byte(text), &m); err != nil {
		return &Error{Code: CodeConfigInvalid, cause: err}
	}
	return nil
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func quoteList(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = quote(v)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func detectEOL(text string) string {
	if i := strings.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func lineOff(text string, idx int) int {
	off := 0
	for ; idx > 0; idx-- {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return off
}

func rootOffset(text string) (int, error) {
	items, err := scan(text)
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if it.header {
			return lineOff(text, it.line), nil
		}
	}
	return len(text), nil
}

// 末尾没有换行的文本在文件尾追加时，把换行加在新内容前面，这样删掉新内容后能回到原样。
func splice(text string, off int, raw, eol string) string {
	if off == len(text) && text != "" && !strings.HasSuffix(text, "\n") {
		return text + eol + strings.TrimSuffix(raw, eol)
	}
	return text[:off] + raw + text[off:]
}

func setValue(text, path, raw, eol string) (string, error) {
	items, err := scan(text)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if !it.header && it.path == path {
			return text[:it.valStart] + raw + text[it.valEnd:], nil
		}
	}
	if dot := strings.LastIndex(path, "."); dot >= 0 {
		for i, h := range items {
			if h.header && h.path == path[:dot] {
				last := h.line
				for _, it := range items[i+1:] {
					if it.header {
						break
					}
					last = it.end
				}
				return splice(text, lineOff(text, last+1), path[dot+1:]+" = "+raw+eol, eol), nil
			}
		}
	}
	at, err := rootOffset(text)
	if err != nil {
		return "", err
	}
	return splice(text, at, path+" = "+raw+eol, eol), nil
}

func setString(text, path, val, eol string) (string, error) {
	return setValue(text, path, quote(val), eol)
}

func setStrings(text, path string, vals []string, eol string) (string, error) {
	return setValue(text, path, quoteList(vals), eol)
}

func removeKey(text, path string) (newText string, line int, raw string, err error) {
	it, ok, err := get(text, path)
	if err != nil || !ok {
		return text, 0, "", err
	}
	start, end := lineOff(text, it.line), lineOff(text, it.end+1)
	raw = text[start:end]
	if end == len(text) && !strings.HasSuffix(raw, "\n") && start > 0 {
		start--
		if start > 0 && text[start-1] == '\r' {
			start--
		}
	}
	return text[:start] + text[end:], it.line, raw, nil
}
