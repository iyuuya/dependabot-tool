package alertscmd

import (
	"regexp"
	"strconv"
	"strings"
)

// --------------------------------------------------------------------------
// 脆弱バージョン範囲の判定
// --------------------------------------------------------------------------

var comparatorRe = regexp.MustCompile(`^(<=|>=|<|>|=)?\s*(.+)$`)

// inVulnerableRange checks whether version falls within a Dependabot-style
// range spec such as '>= 4.0.0, < 4.2.6'. Returns nil if it cannot be
// determined.
func inVulnerableRange(version, spec string) *bool {
	if version == "" || spec == "" {
		return nil
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		m := comparatorRe.FindStringSubmatch(part)
		if m == nil {
			return nil
		}
		op := m[1]
		if op == "" {
			op = "="
		}
		bound := strings.TrimSpace(m[2])
		cmp, ok := compareVersions(version, bound)
		if !ok {
			return nil
		}
		okResult := false
		switch op {
		case "<":
			okResult = cmp < 0
		case "<=":
			okResult = cmp <= 0
		case ">":
			okResult = cmp > 0
		case ">=":
			okResult = cmp >= 0
		case "=":
			okResult = cmp == 0
		}
		if !okResult {
			return boolPtr(false)
		}
	}
	return boolPtr(true)
}

func boolPtr(b bool) *bool { return &b }

// --------------------------------------------------------------------------
// 依存制約 (poetry / npm / bundler) が特定バージョンを許容するかの判定
// --------------------------------------------------------------------------

var constraintTokenRe = regexp.MustCompile(`(===|==|!=|<=|>=|~=|~>|\^|~|<|>|=)?\s*(v?\d[^\s,|]*|\*|[xX])`)
var wildcardRe = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)\.(?:\*|[xX])$`)
var opaqueRe = regexp.MustCompile(`(git|https?|file|link|workspace|npm|catalog|portal|patch)[:+]`)
var hyphenRe = regexp.MustCompile(`^(v?[\w.+-]+)\s+-\s+(v?[\w.+-]+)$`)

type bound struct {
	op      string
	version string
}

// nextVersion bumps release[index] by 1, truncating to index+1 elements.
func nextVersion(release []int, index int) string {
	bumped := make([]int, index+1)
	copy(bumped, release[:index+1])
	bumped[index]++
	parts := make([]string, len(bumped))
	for i, v := range bumped {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ".")
}

// expand converts an operator + raw version into a list of (op, version) bounds.
// Returns (nil, false) when unrecognized/unparseable, ([], true) for "no constraint".
func expand(op, raw string) ([]bound, bool) {
	if raw == "*" || raw == "x" || raw == "X" {
		return []bound{}, true
	}
	if wm := wildcardRe.FindStringSubmatch(raw); wm != nil {
		prefix := wm[1]
		parts := strings.Split(prefix, ".")
		release := make([]int, len(parts))
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, false
			}
			release[i] = n
		}
		switch op {
		case "", "=", "==", "===", "~=", "~", "^":
			return []bound{{">=", prefix}, {"<", nextVersion(release, len(release)-1)}}, true
		case ">=", ">", "<":
			return []bound{{op, prefix}}, true
		case "<=":
			return []bound{{"<", nextVersion(release, len(release)-1)}}, true
		default:
			return nil, false
		}
	}
	parsed := parseVersion(raw)
	if !parsed.ok {
		return nil, false
	}
	release := parsed.release
	switch op {
	case "", "=", "==", "===":
		return []bound{{"==", raw}}, true
	case "<", "<=", ">", ">=", "!=":
		return []bound{{op, raw}}, true
	case "^":
		index := len(release) - 1
		for i, x := range release {
			if x != 0 {
				index = i
				break
			}
		}
		return []bound{{">=", raw}, {"<", nextVersion(release, index)}}, true
	case "~":
		index := 0
		if len(release) >= 2 {
			index = 1
		}
		return []bound{{">=", raw}, {"<", nextVersion(release, index)}}, true
	case "~=", "~>":
		// poetry ~= / bundler ~> : bump the second-to-last component
		// ~> 1.2   => >= 1.2,   < 2.0
		// ~> 1.2.3 => >= 1.2.3, < 1.3.0
		index := 0
		if len(release) >= 2 {
			index = len(release) - 2
		}
		return []bound{{">=", raw}, {"<", nextVersion(release, index)}}, true
	default:
		return nil, false
	}
}

// clauseAllows evaluates a single comma-free constraint clause against version.
func clauseAllows(clause, version string) *bool {
	clause = strings.TrimSpace(clause)
	if clause == "" || clause == "*" || clause == "x" || clause == "X" {
		return boolPtr(true)
	}
	var bounds []bound
	if hm := hyphenRe.FindStringSubmatch(clause); hm != nil {
		bounds = []bound{{">=", hm[1]}, {"<=", hm[2]}}
	} else {
		pos := 0
		matches := constraintTokenRe.FindAllStringSubmatchIndex(clause, -1)
		if len(matches) == 0 {
			return nil
		}
		for _, mi := range matches {
			start, end := mi[0], mi[1]
			if strings.Trim(clause[pos:start], " ,") != "" {
				return nil
			}
			var op string
			if mi[2] != -1 {
				op = clause[mi[2]:mi[3]]
			}
			raw := clause[mi[4]:mi[5]]
			expanded, ok := expand(op, raw)
			if !ok {
				return nil
			}
			bounds = append(bounds, expanded...)
			pos = end
		}
		if pos == 0 || strings.Trim(clause[pos:], " ,") != "" {
			return nil
		}
	}
	for _, b := range bounds {
		cmp, ok := compareVersions(version, b.version)
		if !ok {
			return nil
		}
		okResult := false
		switch b.op {
		case "<":
			okResult = cmp < 0
		case "<=":
			okResult = cmp <= 0
		case ">":
			okResult = cmp > 0
		case ">=":
			okResult = cmp >= 0
		case "==":
			okResult = cmp == 0
		case "!=":
			okResult = cmp != 0
		}
		if !okResult {
			return boolPtr(false)
		}
	}
	return boolPtr(true)
}

// constraintAllows determines whether the dependency constraint spec allows
// version. Returns nil when undeterminable.
func constraintAllows(spec, version string) *bool {
	if version == "" {
		return nil
	}
	// Note: Python distinguishes spec is None vs spec == "". In Go we treat
	// empty string as "no spec provided" analogous to None, matching callers
	// that pass "" for missing constraints.
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "*" || spec == "x" || spec == "X" || strings.EqualFold(spec, "latest") || strings.EqualFold(spec, "any") {
		return boolPtr(true)
	}
	if opaqueRe.MatchString(spec) {
		return nil
	}
	unknown := false
	for _, clause := range strings.Split(spec, "||") {
		result := clauseAllows(clause, version)
		if result != nil && *result {
			return boolPtr(true)
		}
		if result == nil {
			unknown = true
		}
	}
	if unknown {
		return nil
	}
	return boolPtr(false)
}
