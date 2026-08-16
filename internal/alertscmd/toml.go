package alertscmd

import (
	"fmt"
	"strconv"
	"strings"
)

// A minimal TOML parser tailored to the subset used by poetry.lock and
// pyproject.toml: nested tables ([a.b.c]), array-of-tables ([[a.b]]),
// bare/quoted keys, strings (basic/literal/multiline), arrays, inline
// tables, numbers, booleans and dates (captured as raw strings).
//
// It is not a fully spec-compliant TOML implementation, but it is
// sufficient for the fields we actually read (name, version, dependencies,
// markers, and [tool.poetry] dependency tables).

type tomlParser struct {
	src []rune
	pos int
}

func parseTOML(text string) (map[string]interface{}, error) {
	p := &tomlParser{src: []rune(text)}
	root := map[string]interface{}{}
	current := root

	for {
		p.skipWSAndComments(true)
		if p.eof() {
			break
		}
		ch := p.peek()
		if ch == '[' {
			isArray := false
			p.pos++ // consume '['
			if p.peek() == '[' {
				isArray = true
				p.pos++
			}
			segments, err := p.parseDottedKey()
			if err != nil {
				return nil, err
			}
			p.skipWS()
			if isArray {
				if p.peek() != ']' || p.peekAt(1) != ']' {
					return nil, fmt.Errorf("toml: expected ]] at pos %d", p.pos)
				}
				p.pos += 2
			} else {
				if p.peek() != ']' {
					return nil, fmt.Errorf("toml: expected ] at pos %d", p.pos)
				}
				p.pos++
			}
			p.skipWSAndComments(false)
			tbl, err := navigateHeader(root, segments, isArray)
			if err != nil {
				return nil, err
			}
			current = tbl
			continue
		}
		// key = value
		segments, err := p.parseDottedKey()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != '=' {
			return nil, fmt.Errorf("toml: expected '=' at pos %d", p.pos)
		}
		p.pos++
		p.skipWS()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if err := assignDotted(current, segments, value); err != nil {
			return nil, err
		}
		p.skipWSAndComments(false)
	}
	return root, nil
}

func (p *tomlParser) eof() bool { return p.pos >= len(p.src) }

func (p *tomlParser) peek() rune {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *tomlParser) peekAt(offset int) rune {
	if p.pos+offset >= len(p.src) {
		return 0
	}
	return p.src[p.pos+offset]
}

func (p *tomlParser) skipWS() {
	for !p.eof() && (p.peek() == ' ' || p.peek() == '\t') {
		p.pos++
	}
}

// skipWSAndComments skips whitespace, comments, and optionally newlines.
func (p *tomlParser) skipWSAndComments(includeNewlines bool) {
	for !p.eof() {
		ch := p.peek()
		if ch == ' ' || ch == '\t' || ch == '\r' {
			p.pos++
			continue
		}
		if ch == '\n' {
			if !includeNewlines {
				return
			}
			p.pos++
			continue
		}
		if ch == '#' {
			for !p.eof() && p.peek() != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}

func isBareKeyChar(ch rune) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-'
}

func (p *tomlParser) parseKeySegment() (string, error) {
	p.skipWS()
	ch := p.peek()
	if ch == '"' || ch == '\'' {
		s, err := p.parseString()
		if err != nil {
			return "", err
		}
		return s.(string), nil
	}
	start := p.pos
	for !p.eof() && isBareKeyChar(p.peek()) {
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("toml: expected key at pos %d", p.pos)
	}
	return string(p.src[start:p.pos]), nil
}

func (p *tomlParser) parseDottedKey() ([]string, error) {
	var segments []string
	seg, err := p.parseKeySegment()
	if err != nil {
		return nil, err
	}
	segments = append(segments, seg)
	for {
		p.skipWS()
		if p.peek() != '.' {
			break
		}
		p.pos++
		seg, err := p.parseKeySegment()
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	return segments, nil
}

func (p *tomlParser) parseValue() (interface{}, error) {
	p.skipWS()
	if p.eof() {
		return nil, fmt.Errorf("toml: unexpected EOF while parsing value")
	}
	ch := p.peek()
	switch {
	case ch == '"' || ch == '\'':
		return p.parseString()
	case ch == '[':
		return p.parseArray()
	case ch == '{':
		return p.parseInlineTable()
	default:
		return p.parseScalar()
	}
}

func (p *tomlParser) parseString() (interface{}, error) {
	quote := p.peek()
	multiline := p.peekAt(1) == quote && p.peekAt(2) == quote
	if multiline {
		p.pos += 3
		// A newline immediately following the opening delimiter is trimmed.
		if p.peek() == '\n' {
			p.pos++
		} else if p.peek() == '\r' && p.peekAt(1) == '\n' {
			p.pos += 2
		}
		var sb strings.Builder
		for {
			if p.eof() {
				return nil, fmt.Errorf("toml: unterminated multiline string")
			}
			if p.peek() == quote && p.peekAt(1) == quote && p.peekAt(2) == quote {
				p.pos += 3
				break
			}
			if quote == '"' && p.peek() == '\\' {
				r, err := p.readEscape()
				if err != nil {
					return nil, err
				}
				sb.WriteString(r)
				continue
			}
			sb.WriteRune(p.peek())
			p.pos++
		}
		return sb.String(), nil
	}
	p.pos++
	var sb strings.Builder
	for {
		if p.eof() {
			return nil, fmt.Errorf("toml: unterminated string")
		}
		if p.peek() == quote {
			p.pos++
			break
		}
		if quote == '"' && p.peek() == '\\' {
			r, err := p.readEscape()
			if err != nil {
				return nil, err
			}
			sb.WriteString(r)
			continue
		}
		sb.WriteRune(p.peek())
		p.pos++
	}
	return sb.String(), nil
}

func (p *tomlParser) readEscape() (string, error) {
	p.pos++ // consume backslash
	if p.eof() {
		return "", fmt.Errorf("toml: unterminated escape")
	}
	ch := p.peek()
	p.pos++
	switch ch {
	case 'n':
		return "\n", nil
	case 't':
		return "\t", nil
	case 'r':
		return "\r", nil
	case 'b':
		return "\b", nil
	case 'f':
		return "\f", nil
	case '"':
		return "\"", nil
	case '\\':
		return "\\", nil
	case 'u':
		if p.pos+4 > len(p.src) {
			return "", fmt.Errorf("toml: invalid unicode escape")
		}
		hex := string(p.src[p.pos : p.pos+4])
		p.pos += 4
		n, err := strconv.ParseInt(hex, 16, 32)
		if err != nil {
			return "", err
		}
		return string(rune(n)), nil
	case 'U':
		if p.pos+8 > len(p.src) {
			return "", fmt.Errorf("toml: invalid unicode escape")
		}
		hex := string(p.src[p.pos : p.pos+8])
		p.pos += 8
		n, err := strconv.ParseInt(hex, 16, 32)
		if err != nil {
			return "", err
		}
		return string(rune(n)), nil
	default:
		return string(ch), nil
	}
}

func (p *tomlParser) parseArray() (interface{}, error) {
	p.pos++ // consume '['
	var out []interface{}
	for {
		p.skipWSAndComments(true)
		if p.eof() {
			return nil, fmt.Errorf("toml: unterminated array")
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipWSAndComments(true)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		return nil, fmt.Errorf("toml: expected ',' or ']' at pos %d", p.pos)
	}
	return out, nil
}

func (p *tomlParser) parseInlineTable() (interface{}, error) {
	p.pos++ // consume '{'
	out := map[string]interface{}{}
	p.skipWS()
	if p.peek() == '}' {
		p.pos++
		return out, nil
	}
	for {
		p.skipWS()
		segments, err := p.parseDottedKey()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != '=' {
			return nil, fmt.Errorf("toml: expected '=' in inline table at pos %d", p.pos)
		}
		p.pos++
		p.skipWS()
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if err := assignDotted(out, segments, v); err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() == ',' {
			p.pos++
			continue
		}
		if p.peek() == '}' {
			p.pos++
			break
		}
		return nil, fmt.Errorf("toml: expected ',' or '}' at pos %d", p.pos)
	}
	return out, nil
}

var scalarStop = map[rune]bool{',': true, ']': true, '}': true, '#': true, '\n': true, '\r': true}

func (p *tomlParser) parseScalar() (interface{}, error) {
	start := p.pos
	for !p.eof() && !scalarStop[p.peek()] {
		p.pos++
	}
	raw := strings.TrimSpace(string(p.src[start:p.pos]))
	if raw == "" {
		return nil, fmt.Errorf("toml: expected value at pos %d", start)
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	clean := strings.ReplaceAll(raw, "_", "")
	if n, err := strconv.ParseInt(clean, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return f, nil
	}
	// Dates and anything else unrecognized are kept as their raw string form.
	return raw, nil
}

// navigateHeader resolves (creating as needed) the table addressed by a
// [a.b.c] / [[a.b.c]] header, honoring array-of-tables semantics for
// intermediate segments.
func navigateHeader(root map[string]interface{}, segments []string, isArray bool) (map[string]interface{}, error) {
	current := root
	for i, seg := range segments {
		last := i == len(segments)-1
		if last && isArray {
			existing, ok := current[seg]
			var arr []interface{}
			if ok {
				arr, ok = existing.([]interface{})
				if !ok {
					return nil, fmt.Errorf("toml: %q is not an array of tables", seg)
				}
			}
			newTable := map[string]interface{}{}
			arr = append(arr, newTable)
			current[seg] = arr
			return newTable, nil
		}
		existing, ok := current[seg]
		if !ok {
			newTable := map[string]interface{}{}
			current[seg] = newTable
			current = newTable
			continue
		}
		switch v := existing.(type) {
		case map[string]interface{}:
			current = v
		case []interface{}:
			if len(v) == 0 {
				return nil, fmt.Errorf("toml: %q array of tables is empty", seg)
			}
			m, ok := v[len(v)-1].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("toml: %q last element is not a table", seg)
			}
			current = m
		default:
			return nil, fmt.Errorf("toml: %q already defined as non-table", seg)
		}
	}
	return current, nil
}

// assignDotted assigns value under current following (creating as needed)
// nested tables for all but the final key segment.
func assignDotted(current map[string]interface{}, segments []string, value interface{}) error {
	for _, seg := range segments[:len(segments)-1] {
		existing, ok := current[seg]
		if !ok {
			newTable := map[string]interface{}{}
			current[seg] = newTable
			current = newTable
			continue
		}
		m, ok := existing.(map[string]interface{})
		if !ok {
			return fmt.Errorf("toml: %q already defined as non-table", seg)
		}
		current = m
	}
	current[segments[len(segments)-1]] = value
	return nil
}

// -- helpers for consuming the generic map[string]interface{} tree --------

func tomlGetMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	tm, _ := v.(map[string]interface{})
	return tm
}

func tomlGetArray(m map[string]interface{}, key string) []interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, _ := v.([]interface{})
	return arr
}

func tomlGetString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
