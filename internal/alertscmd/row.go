package alertscmd

import "strings"

// --------------------------------------------------------------------------
// 集計と出力
// --------------------------------------------------------------------------

// Row mirrors the Python Row dataclass; JSON/CSV field names must match
// exactly (snake_case).
type Row struct {
	Number          int    `json:"number"`
	State           string `json:"state"`
	Severity        string `json:"severity"`
	Ecosystem       string `json:"ecosystem"`
	Name            string `json:"name"`
	ManifestPath    string `json:"manifest_path"`
	Scope           string `json:"scope"`
	VulnerableRange string `json:"vulnerable_range"`
	PatchedVersion  string `json:"patched_version"`
	LocalVersion    string `json:"local_version"`
	LocalSource     string `json:"local_source"`
	Gap             string `json:"gap"`
	Affected        string `json:"affected"`
	Ease            string `json:"ease"`
	FixKind         string `json:"fix_kind"`
	FixHint         string `json:"fix_hint"`
	Blockers        string `json:"blockers"`
	Dependents      string `json:"dependents"`
	Entrypoints     string `json:"entrypoints"`
	GhsaID          string `json:"ghsa_id"`
	Summary         string `json:"summary"`
	HTMLURL         string `json:"html_url"`
}

// rowFieldOrder lists Row's field names (JSON/CSV key names) in declaration order.
var rowFieldOrder = []string{
	"number", "state", "severity", "ecosystem", "name", "manifest_path",
	"scope", "vulnerable_range", "patched_version", "local_version",
	"local_source", "gap", "affected", "ease", "fix_kind", "fix_hint",
	"blockers", "dependents", "entrypoints", "ghsa_id", "summary", "html_url",
}

func mGet(m map[string]interface{}, key string) interface{} {
	if m == nil {
		return nil
	}
	return m[key]
}

func mGetMap(m map[string]interface{}, key string) map[string]interface{} {
	v, _ := mGet(m, key).(map[string]interface{})
	return v
}

func mGetString(m map[string]interface{}, key string) string {
	v := mGet(m, key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func mGetInt(m map[string]interface{}, key string) int {
	v := mGet(m, key)
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func buildRows(alerts []map[string]interface{}, local *LocalVersions, graphs *GraphCache) []Row {
	rows := make([]Row, 0, len(alerts))
	for _, alert := range alerts {
		dep := mGetMap(alert, "dependency")
		pkg := mGetMap(dep, "package")
		vuln := mGetMap(alert, "security_vulnerability")
		adv := mGetMap(alert, "security_advisory")

		ecosystem := mGetString(pkg, "ecosystem")
		if ecosystem == "" {
			ecosystem = "?"
		}
		name := mGetString(pkg, "name")
		if name == "" {
			name = "?"
		}
		manifestPath := mGetString(dep, "manifest_path")
		firstPatched := mGetMap(vuln, "first_patched_version")
		patched := mGetString(firstPatched, "identifier")
		vulnRange := mGetString(vuln, "vulnerable_version_range")

		versions, source := local.resolve(ecosystem, manifestPath, name)

		hits := make([]*bool, len(versions))
		for i, v := range versions {
			hits[i] = inVulnerableRange(v, vulnRange)
		}
		var hit *bool
		if len(versions) == 0 {
			hit = nil
		} else {
			anyTrue, anyNil := false, false
			for _, h := range hits {
				if h == nil {
					anyNil = true
				} else if *h {
					anyTrue = true
				}
			}
			switch {
			case anyTrue:
				hit = boolPtr(true)
			case anyNil:
				hit = nil
			default:
				hit = boolPtr(false)
			}
		}

		var targets []string
		for i, v := range versions {
			if hits[i] != nil && *hits[i] {
				targets = append(targets, v)
			}
		}
		if len(targets) == 0 {
			targets = versions
		}

		gap := "unknown"
		if len(targets) > 0 {
			best := ""
			bestOrder := 1 << 30
			for _, v := range targets {
				g := versionGap(v, patched)
				order, ok := gapOrder[g]
				if !ok {
					order = 9
				}
				if best == "" || order < bestOrder {
					best = g
					bestOrder = order
				}
			}
			gap = best
		}

		graph := graphs.get(ecosystem, source)
		nodeIDs := selectNodes(graph, name, targets)
		result := analyze(graph, name, nodeIDs, patched, gap)

		patchedDisplay := patched
		if patchedDisplay == "" {
			patchedDisplay = "-"
		}
		localDisplay := strings.Join(versions, " / ")
		if localDisplay == "" {
			localDisplay = "-"
		}
		var affected string
		switch {
		case hit == nil:
			affected = "?"
		case *hit:
			affected = "yes"
		default:
			affected = "no"
		}

		entrypointLabels := ""
		if graph != nil {
			labels := make([]string, len(result.Entrypoints))
			for i, n := range result.Entrypoints {
				labels[i] = graph.label(n)
			}
			entrypointLabels = strings.Join(labels, " / ")
		}

		blockerStrs := make([]string, len(result.Blockers))
		for i, b := range result.Blockers {
			blockerStrs[i] = b.render()
		}

		ghsaID := mGetString(adv, "ghsa_id")
		summary := strings.ReplaceAll(mGetString(adv, "summary"), "\n", " ")

		rows = append(rows, Row{
			Number:          mGetInt(alert, "number"),
			State:           orDefault(mGetString(alert, "state"), "?"),
			Severity:        orDefault(orDefault(mGetString(vuln, "severity"), mGetString(adv, "severity")), "?"),
			Ecosystem:       ecosystem,
			Name:            name,
			ManifestPath:    manifestPath,
			Scope:           mGetString(dep, "scope"),
			VulnerableRange: vulnRange,
			PatchedVersion:  patchedDisplay,
			LocalVersion:    localDisplay,
			LocalSource:     source,
			Gap:             gap,
			Affected:        affected,
			Ease:            result.Ease,
			FixKind:         result.Kind,
			FixHint:         result.Hint,
			Blockers:        strings.Join(blockerStrs, " / "),
			Dependents:      strings.Join(result.Dependents, " / "),
			Entrypoints:     entrypointLabels,
			GhsaID:          ghsaID,
			Summary:         summary,
			HTMLURL:         mGetString(alert, "html_url"),
		})
	}
	return rows
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func selectNodes(graph *DependencyGraph, name string, versions []string) []string {
	if graph == nil {
		return nil
	}
	candidates := graph.lookup(name)
	if len(candidates) == 0 || len(versions) == 0 {
		return candidates
	}
	versionSet := map[string]bool{}
	for _, v := range versions {
		versionSet[v] = true
	}
	var matched []string
	for _, n := range candidates {
		if versionSet[graph.Versions[n]] {
			matched = append(matched, n)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return candidates
}
