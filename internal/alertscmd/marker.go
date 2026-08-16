package alertscmd

import (
	"regexp"
	"strings"
)

// --------------------------------------------------------------------------
// 環境マーカーの評価 (python_version 系のみ)
// --------------------------------------------------------------------------

var markerClauseRe = regexp.MustCompile(`^\s*(python_version|python_full_version)\s*(===|==|!=|<=|>=|<|>)\s*["']([^"']+)["']\s*$`)
var orSplitRe = regexp.MustCompile(`\s+or\s+`)
var andSplitRe = regexp.MustCompile(`\s+and\s+`)

// evaluateMarker determines whether the marker holds. Returns nil when it
// cannot be determined (contains anything other than python_version /
// python_full_version).
func evaluateMarker(marker, pythonVersion string) *bool {
	if strings.TrimSpace(marker) == "" {
		return boolPtr(true)
	}
	text := strings.ReplaceAll(marker, "(", " ")
	text = strings.ReplaceAll(text, ")", " ")
	if orSplitRe.MatchString(text) {
		parts := orSplitRe.Split(text, -1)
		results := make([]*bool, len(parts))
		for i, part := range parts {
			results[i] = evaluateMarker(part, pythonVersion)
		}
		for _, r := range results {
			if r != nil && *r {
				return boolPtr(true)
			}
		}
		for _, r := range results {
			if r == nil {
				return nil
			}
		}
		return boolPtr(false)
	}

	var results []*bool
	for _, part := range andSplitRe.Split(text, -1) {
		m := markerClauseRe.FindStringSubmatch(part)
		if m == nil {
			results = append(results, nil)
			continue
		}
		op := m[2]
		cmp, ok := compareVersions(pythonVersion, m[3])
		if !ok {
			results = append(results, nil)
			continue
		}
		var res bool
		switch op {
		case "<":
			res = cmp < 0
		case "<=":
			res = cmp <= 0
		case ">":
			res = cmp > 0
		case ">=":
			res = cmp >= 0
		case "==", "===":
			res = cmp == 0
		case "!=":
			res = cmp != 0
		}
		results = append(results, boolPtr(res))
	}
	for _, r := range results {
		if r != nil && !*r {
			return boolPtr(false)
		}
	}
	for _, r := range results {
		if r == nil {
			return nil
		}
	}
	return boolPtr(true)
}
