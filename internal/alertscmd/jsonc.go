package alertscmd

import "encoding/json"

// loadsJSONC parses text as JSON, falling back to a lenient JSONC parse
// (strips // and /* */ comments and trailing commas, while leaving string
// literals untouched) for bun.lock, which is JSON with comments and
// trailing commas allowed.
func loadsJSONC(text string) (map[string]interface{}, error) {
	var direct map[string]interface{}
	if err := json.Unmarshal([]byte(text), &direct); err == nil {
		return direct, nil
	}

	runes := []rune(text)
	n := len(runes)
	var out []rune
	i := 0
	for i < n {
		ch := runes[i]
		if ch == '"' {
			j := i + 1
			for j < n {
				if runes[j] == '\\' {
					j += 2
					continue
				}
				if runes[j] == '"' {
					break
				}
				j++
			}
			end := j + 1
			if end > n {
				end = n
			}
			out = append(out, runes[i:end]...)
			i = end
			continue
		}
		if ch == '/' && i+1 < n && runes[i+1] == '/' {
			for i < n && runes[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < n && runes[i+1] == '*' {
			end := indexOfRunes(runes, "*/", i+2)
			if end < 0 {
				i = n
			} else {
				i = end + 2
			}
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < n && (runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\r' || runes[j] == '\n') {
				j++
			}
			if j < n && (runes[j] == '}' || runes[j] == ']') {
				i++
				continue
			}
		}
		out = append(out, ch)
		i++
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(string(out)), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func indexOfRunes(haystack []rune, needle string, from int) int {
	needleRunes := []rune(needle)
	n, m := len(haystack), len(needleRunes)
	for i := from; i+m <= n; i++ {
		match := true
		for j := 0; j < m; j++ {
			if haystack[i+j] != needleRunes[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
