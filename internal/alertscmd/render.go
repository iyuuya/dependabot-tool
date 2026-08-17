package alertscmd

import (
	"fmt"
	"sort"
	"strings"
)

func sortRows(rows []Row, key string) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	if key == "ease" {
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if c := easeOrderOf(a.Ease) - easeOrderOf(b.Ease); c != 0 {
				return c < 0
			}
			if c := -severityOrderOf(a.Severity) - (-severityOrderOf(b.Severity)); c != 0 {
				return c < 0
			}
			if c := gapOrderOf(a.Gap) - gapOrderOf(b.Gap); c != 0 {
				return c < 0
			}
			if a.Ecosystem != b.Ecosystem {
				return a.Ecosystem < b.Ecosystem
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Number < b.Number
		})
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if c := (-severityOrderOf(a.Severity)) - (-severityOrderOf(b.Severity)); c != 0 {
			return c < 0
		}
		if c := gapOrderOf(a.Gap) - gapOrderOf(b.Gap); c != 0 {
			return c < 0
		}
		if c := easeOrderOf(a.Ease) - easeOrderOf(b.Ease); c != 0 {
			return c < 0
		}
		if a.Ecosystem != b.Ecosystem {
			return a.Ecosystem < b.Ecosystem
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Number < b.Number
	})
	return out
}

func truncate(text string, width int) string {
	r := []rune(text)
	if len(r) <= width {
		return text
	}
	return string(r[:width-1]) + "…"
}

type tableHeader struct {
	key   string
	label string
}

func rowField(r Row, key string) string {
	switch key {
	case "severity":
		return r.Severity
	case "ecosystem":
		return r.Ecosystem
	case "name":
		return r.Name
	case "manifest_path":
		return r.ManifestPath
	case "local_version":
		return r.LocalVersion
	case "patched_version":
		return r.PatchedVersion
	case "gap":
		return r.Gap
	case "ease":
		return r.Ease
	case "fix_kind":
		return r.FixKind
	case "affected":
		return r.Affected
	case "vulnerable_range":
		return r.VulnerableRange
	case "fix_hint":
		return r.FixHint
	case "summary":
		return r.Summary
	}
	return ""
}

func renderTable(rows []Row, showSummary, showAction bool) string {
	headers := []tableHeader{
		{"severity", "SEVERITY"},
		{"ecosystem", "ECO"},
		{"name", "PACKAGE"},
		{"manifest_path", "MANIFEST"},
		{"local_version", "LOCAL"},
		{"patched_version", "PATCHED"},
		{"gap", "GAP"},
		{"ease", "EASE"},
		{"fix_kind", "FIX"},
		{"affected", "AFFECTED"},
		{"vulnerable_range", "VULN RANGE"},
	}
	if showAction {
		headers = append(headers, tableHeader{"fix_hint", "ACTION"})
	}
	if showSummary {
		headers = append(headers, tableHeader{"summary", "SUMMARY"})
	}

	table := make([][]string, 0, len(rows)+1)
	headerRow := make([]string, len(headers))
	for i, h := range headers {
		headerRow[i] = h.label
	}
	table = append(table, headerRow)
	for _, row := range rows {
		cells := make([]string, len(headers))
		for i, h := range headers {
			cells[i] = truncate(rowField(row, h.key), 90)
		}
		table = append(table, cells)
	}

	widths := make([]int, len(headers))
	for _, row := range table {
		for i, c := range row {
			if l := len([]rune(c)); l > widths[i] {
				widths[i] = l
			}
		}
	}

	var lines []string
	for i, cells := range table {
		parts := make([]string, len(cells))
		for j, c := range cells {
			parts[j] = padRight(c, widths[j])
		}
		lines = append(lines, strings.TrimRight(strings.Join(parts, "  "), " "))
		if i == 0 {
			seps := make([]string, len(widths))
			for j, w := range widths {
				seps[j] = strings.Repeat("-", w)
			}
			lines = append(lines, strings.Join(seps, "  "))
		}
	}
	return strings.Join(lines, "\n")
}

func padRight(s string, width int) string {
	l := len([]rune(s))
	if l >= width {
		return s
	}
	return s + strings.Repeat(" ", width-l)
}

// --------------------------------------------------------------------------
// パッケージ単位のグルーピング (tree 出力用)
// --------------------------------------------------------------------------

type Group struct {
	Ecosystem string
	Name      string
	Source    string
	Rows      []Row
	Required  string
	Gap       string
	Analysis  Analysis
	Graph     *DependencyGraph
}

type groupKey struct {
	ecosystem string
	source    string
	name      string
}

func buildGroups(rows []Row, graphs *GraphCache) []Group {
	buckets := map[groupKey][]Row{}
	var order []groupKey
	for _, row := range rows {
		key := row.Name
		switch row.Ecosystem {
		case "pip":
			key = normalizePypi(row.Name)
		case "rubygems", "bundler":
			key = normalizeGem(row.Name)
		}
		gk := groupKey{row.Ecosystem, row.LocalSource, key}
		if _, ok := buckets[gk]; !ok {
			order = append(order, gk)
		}
		buckets[gk] = append(buckets[gk], row)
	}

	var groups []Group
	for _, gk := range order {
		members := buckets[gk]
		graph := graphs.get(gk.ecosystem, gk.source)
		name := members[0].Name
		if graph != nil {
			if cands := graph.lookup(gk.name); len(cands) > 0 {
				name = graph.Names[cands[0]]
			}
		}
		var relevant []Row
		for _, r := range members {
			if r.Affected == "yes" {
				relevant = append(relevant, r)
			}
		}
		if len(relevant) == 0 {
			relevant = members
		}
		var patchedVersions []string
		for _, r := range relevant {
			if r.PatchedVersion != "-" {
				patchedVersions = append(patchedVersions, r.PatchedVersion)
			}
		}
		required := "-"
		if mv, ok := maxVersion(patchedVersions); ok {
			required = mv
		}

		var locals []string
		for _, v := range strings.Split(members[0].LocalVersion, " / ") {
			if v != "" && v != "-" {
				locals = append(locals, v)
			}
		}
		var gaps []string
		for _, v := range locals {
			gaps = append(gaps, versionGap(v, required))
		}
		if len(gaps) == 0 {
			gaps = []string{"unknown"}
		}
		gap := gaps[0]
		gapBest := gapOrderOf(gap)
		for _, g := range gaps[1:] {
			if o := gapOrderOf(g); o < gapBest {
				gap = g
				gapBest = o
			}
		}

		nodeIDs := selectNodes(graph, name, locals)
		analysis := analyze(graph, name, nodeIDs, required, gap)
		groups = append(groups, Group{gk.ecosystem, name, gk.source, members, required, gap, analysis, graph})
	}

	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if c := easeOrderOf(a.Analysis.Ease) - easeOrderOf(b.Analysis.Ease); c != 0 {
			return c < 0
		}
		maxSevA, maxSevB := -1, -1
		for _, r := range a.Rows {
			if v := severityOrderOf(r.Severity); v > maxSevA {
				maxSevA = v
			}
		}
		for _, r := range b.Rows {
			if v := severityOrderOf(r.Severity); v > maxSevB {
				maxSevB = v
			}
		}
		if c := (-maxSevA) - (-maxSevB); c != 0 {
			return c < 0
		}
		if c := gapOrderOf(a.Gap) - gapOrderOf(b.Gap); c != 0 {
			return c < 0
		}
		if a.Ecosystem != b.Ecosystem {
			return a.Ecosystem < b.Ecosystem
		}
		return a.Name < b.Name
	})
	return groups
}

func renderTree(groups []Group, depthLimit, maxLines int) string {
	var blocks []string
	for _, group := range groups {
		counts := map[string]int{}
		var sevOrder []string
		for _, row := range group.Rows {
			if _, ok := counts[row.Severity]; !ok {
				sevOrder = append(sevOrder, row.Severity)
			}
			counts[row.Severity]++
		}
		sort.SliceStable(sevOrder, func(i, j int) bool {
			return -severityOrderOf(sevOrder[i]) < -severityOrderOf(sevOrder[j])
		})
		sevParts := make([]string, len(sevOrder))
		for i, s := range sevOrder {
			sevParts[i] = fmt.Sprintf("%s×%d", s, counts[s])
		}
		severities := strings.Join(sevParts, ", ")

		local := group.Rows[0].LocalVersion
		head := fmt.Sprintf("■ %s %s → %s  [%s / ease=%s / %s]  %s",
			group.Name, local, group.Required, group.Gap, group.Analysis.Ease, group.Analysis.Kind, severities)
		body := []string{head, "  出所: " + group.Source, "  対応: " + group.Analysis.Hint}
		if len(group.Analysis.Blockers) > 0 {
			body = append(body, "  ブロッカー:")
			for _, b := range group.Analysis.Blockers {
				body = append(body, "    - "+b.render())
			}
		}
		if group.Graph != nil && len(group.Analysis.Entrypoints) > 0 {
			labels := make([]string, len(group.Analysis.Entrypoints))
			for i, n := range group.Analysis.Entrypoints {
				labels[i] = group.Graph.label(n)
			}
			body = append(body, "  入口(直接依存): "+strings.Join(labels, ", "))
		}
		seen := map[string]bool{}
		var advisories []string
		for _, r := range group.Rows {
			if r.GhsaID != "" && !seen[r.GhsaID] {
				seen[r.GhsaID] = true
				advisories = append(advisories, r.GhsaID)
			}
		}
		if len(advisories) > 0 {
			body = append(body, "  advisory: "+strings.Join(advisories, ", "))
		}
		body = append(body, "  逆依存:")
		if group.Graph != nil && len(group.Analysis.NodeIDs) > 0 {
			tree := renderReverseTree(group.Graph, group.Analysis.NodeIDs, group.Required, depthLimit, maxLines)
			for _, line := range tree {
				body = append(body, "    "+line)
			}
		} else {
			body = append(body, "    (依存グラフを解決できませんでした)")
		}
		blocks = append(blocks, strings.Join(body, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func renderCounts(rows []Row) string {
	bySev := map[string]int{}
	byGap := map[string]int{}
	byEase := map[string]int{}
	byKind := map[string]int{}
	var sevOrder, gapOrderKeys, easeOrderKeys, kindOrderKeys []string
	for _, row := range rows {
		if _, ok := bySev[row.Severity]; !ok {
			sevOrder = append(sevOrder, row.Severity)
		}
		bySev[row.Severity]++
		if _, ok := byGap[row.Gap]; !ok {
			gapOrderKeys = append(gapOrderKeys, row.Gap)
		}
		byGap[row.Gap]++
		if _, ok := byEase[row.Ease]; !ok {
			easeOrderKeys = append(easeOrderKeys, row.Ease)
		}
		byEase[row.Ease]++
		if _, ok := byKind[row.FixKind]; !ok {
			kindOrderKeys = append(kindOrderKeys, row.FixKind)
		}
		byKind[row.FixKind]++
	}
	sort.SliceStable(sevOrder, func(i, j int) bool { return -severityOrderOf(sevOrder[i]) < -severityOrderOf(sevOrder[j]) })
	sort.SliceStable(gapOrderKeys, func(i, j int) bool { return gapOrderOf(gapOrderKeys[i]) < gapOrderOf(gapOrderKeys[j]) })
	sort.SliceStable(easeOrderKeys, func(i, j int) bool { return easeOrderOf(easeOrderKeys[i]) < easeOrderOf(easeOrderKeys[j]) })
	sort.Strings(kindOrderKeys)

	sevParts := make([]string, len(sevOrder))
	for i, k := range sevOrder {
		sevParts[i] = fmt.Sprintf("%s:%d", k, bySev[k])
	}
	gapParts := make([]string, len(gapOrderKeys))
	for i, k := range gapOrderKeys {
		gapParts[i] = fmt.Sprintf("%s:%d", k, byGap[k])
	}
	easeParts := make([]string, len(easeOrderKeys))
	for i, k := range easeOrderKeys {
		easeParts[i] = fmt.Sprintf("%s:%d", k, byEase[k])
	}
	kindParts := make([]string, len(kindOrderKeys))
	for i, k := range kindOrderKeys {
		kindParts[i] = fmt.Sprintf("%s:%d", k, byKind[k])
	}

	affected := 0
	for _, r := range rows {
		if r.Affected == "yes" {
			affected++
		}
	}

	return fmt.Sprintf("\n合計 %d 件 (手元が該当: %d 件)\n  severity: %s\n  gap: %s\n  ease: %s\n  fix: %s",
		len(rows), affected, strings.Join(sevParts, " / "), strings.Join(gapParts, " / "), strings.Join(easeParts, " / "), strings.Join(kindParts, " / "))
}
