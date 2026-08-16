package alertscmd

import (
	"regexp"
	"strconv"
	"strings"
)

// --------------------------------------------------------------------------
// バージョン比較 (PEP 440 / semver 両対応の緩い実装)
// --------------------------------------------------------------------------

var versionRe = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)(.*)$`)
var prereleaseRe = regexp.MustCompile(`(?i)^[-_.]?(a|b|c|rc|alpha|beta|pre|preview|dev)`)

// parsedVersion is the comparison key for a version string.
type parsedVersion struct {
	release   []int
	isRelease int // 0 = prerelease, 1 = release
	rest      string
	ok        bool
}

// parseVersion parses '1.2.3-rc1' into a comparable key. Returns ok=false if it cannot be parsed.
func parseVersion(raw string) parsedVersion {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedVersion{}
	}
	m := versionRe.FindStringSubmatch(raw)
	if m == nil {
		return parsedVersion{}
	}
	parts := strings.Split(m[1], ".")
	release := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return parsedVersion{}
		}
		release[i] = n
	}
	rest := m[2]
	isRelease := 1
	if prereleaseRe.MatchString(rest) {
		isRelease = 0
	}
	return parsedVersion{release: release, isRelease: isRelease, rest: rest, ok: true}
}

func padRelease(release []int, width int) []int {
	if len(release) >= width {
		return release
	}
	out := make([]int, width)
	copy(out, release)
	return out
}

func cmpIntSlices(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// compareVersions returns -1/0/1 comparing a to b, and ok=false if either cannot be parsed.
func compareVersions(a, b string) (int, bool) {
	pa, pb := parseVersion(a), parseVersion(b)
	if !pa.ok || !pb.ok {
		return 0, false
	}
	width := len(pa.release)
	if len(pb.release) > width {
		width = len(pb.release)
	}
	ra, rb := padRelease(pa.release, width), padRelease(pb.release, width)
	if c := cmpIntSlices(ra, rb); c != 0 {
		return c, true
	}
	if pa.isRelease != pb.isRelease {
		if pa.isRelease < pb.isRelease {
			return -1, true
		}
		return 1, true
	}
	if pa.rest < pb.rest {
		return -1, true
	}
	if pa.rest > pb.rest {
		return 1, true
	}
	return 0, true
}

// maxVersion returns the highest parsable version, or "" with ok=false if none.
func maxVersion(versions []string) (string, bool) {
	best := ""
	found := false
	for _, v := range versions {
		if !parseVersion(v).ok {
			continue
		}
		if !found {
			best = v
			found = true
			continue
		}
		if c, ok := compareVersions(v, best); ok && c > 0 {
			best = v
		}
	}
	return best, found
}

// compareIntSlicesPrefix compares two int slices using Python tuple
// semantics: element-wise comparison, with the shorter slice considered
// smaller when it is a strict prefix of the longer one (no zero-padding).
func compareIntSlicesPrefix(a, b []int) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// bunVersionSortLess replicates the Python sort key
// `parse_version(v) or ((), 0, v)` used when sorting bun.lock versions:
// unparsable versions sort by their raw string (release treated as the
// empty tuple, which always sorts first against any non-empty release),
// while parsable versions compare release tuples with the *prefix* rule
// (no zero-padding), then release/prerelease flag, then the raw suffix.
func bunVersionSortLess(a, b string) bool {
	pa, pb := parseVersion(a), parseVersion(b)
	switch {
	case !pa.ok && !pb.ok:
		return a < b
	case !pa.ok:
		return true // empty release always sorts before a non-empty one
	case !pb.ok:
		return false
	}
	if c := compareIntSlicesPrefix(pa.release, pb.release); c != 0 {
		return c < 0
	}
	if pa.isRelease != pb.isRelease {
		return pa.isRelease < pb.isRelease
	}
	return pa.rest < pb.rest
}

// versionGap returns the level difference from current to target.
func versionGap(current, target string) string {
	pc, pt := parseVersion(current), parseVersion(target)
	if !pc.ok || !pt.ok {
		return "unknown"
	}
	if cmp, ok := compareVersions(current, target); ok && cmp >= 0 {
		return "up-to-date"
	}
	width := 3
	if len(pc.release) > width {
		width = len(pc.release)
	}
	if len(pt.release) > width {
		width = len(pt.release)
	}
	rc := padRelease(pc.release, width)
	rt := padRelease(pt.release, width)
	if rc[0] != rt[0] {
		return "major"
	}
	if rc[1] != rt[1] {
		return "minor"
	}
	if rc[2] != rt[2] {
		return "patch"
	}
	return "other"
}
