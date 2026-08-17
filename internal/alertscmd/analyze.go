package alertscmd

import "strings"

// --------------------------------------------------------------------------
// 「何を上げればよいか」「どれくらい上げやすいか」の判定
// --------------------------------------------------------------------------

var kindWeight = map[string]int{"lock-only": 0, "direct": 1, "blocked": 2, "unknown": 2, "no-patch": 9}
var gapWeight = map[string]int{"patch": 0, "minor": 1, "other": 1, "up-to-date": 0, "major": 3, "unknown": 3}

// Blocker describes a parent whose constraint blocks upgrading to the target version.
type Blocker struct {
	Label       string
	Constraint  string
	Child       string
	Conditional bool
}

func (b Blocker) render() string {
	mark := ""
	if b.Conditional {
		mark = "?"
	}
	constraint := b.Constraint
	if constraint == "" {
		constraint = "*"
	}
	return b.Label + " [" + b.Child + " " + constraint + "]" + mark
}

// Analysis is the result of analyzing how to fix a vulnerable dependency.
type Analysis struct {
	Kind        string
	Ease        string
	Hint        string
	Blockers    []Blocker
	Dependents  []string
	Entrypoints []string
	NodeIDs     []string
}

func shellPrefix(g *DependencyGraph) string {
	if g.ManifestDir == "." {
		return ""
	}
	return "cd " + g.ManifestDir + " && "
}

func buildHint(graph *DependencyGraph, name, target, kind string, blockers []Blocker, nodeIDs []string) string {
	prefix := shellPrefix(graph)
	if kind == "blocked" {
		seen := map[string]bool{}
		var names []string
		for _, b := range blockers {
			if !seen[b.Label] {
				seen[b.Label] = true
				names = append(names, b.Label)
			}
		}
		return "先に " + strings.Join(names, ", ") + " を更新（" + name + " " + target + " を許容しない）"
	}
	if kind == "direct" {
		manifest := "pyproject.toml"
		follow := "poetry lock"
		switch graph.Ecosystem {
		case "npm":
			manifest = "package.json"
			follow = "bun install"
		case "rubygems", "bundler":
			manifest = "Gemfile"
			follow = "bundle update " + name
		}
		seen := map[string]bool{}
		var currents []string
		for _, n := range nodeIDs {
			if d, ok := graph.Direct[n]; ok {
				if !seen[d.Constraint] {
					seen[d.Constraint] = true
					currents = append(currents, d.Constraint)
				}
			}
		}
		current := strings.Join(currents, ", ")
		return manifest + " の " + name + " (" + current + ") を " + target + " 以上へ書き換え → " + prefix + follow
	}
	switch graph.Ecosystem {
	case "pip":
		return prefix + "poetry update " + name
	case "rubygems", "bundler":
		return prefix + "bundle update " + name
	default:
		return prefix + "bun update " + name
	}
}

func analyze(graph *DependencyGraph, name string, nodeIDs []string, target, gap string) Analysis {
	if graph == nil {
		return Analysis{Kind: "unknown", Ease: "?", Hint: "依存グラフを解決できません"}
	}
	if len(nodeIDs) == 0 {
		return Analysis{Kind: "unknown", Ease: "?", Hint: "ロックファイル内に見つかりません"}
	}
	if target == "" || target == "-" {
		return Analysis{Kind: "no-patch", Ease: "?", Hint: "修正版が未公開", NodeIDs: nodeIDs}
	}

	var blockers []Blocker
	var dependents []string
	dependentsSeen := map[string]bool{}
	for _, nodeID := range nodeIDs {
		for _, edge := range graph.Parents[nodeID] {
			label := graph.label(edge.Parent)
			if !dependentsSeen[label] {
				dependentsSeen[label] = true
				dependents = append(dependents, label)
			}
			allows := constraintAllows(edge.Constraint, target)
			if allows != nil && !*allows {
				childName := graph.Names[nodeID]
				if childName == "" {
					childName = name
				}
				blocker := Blocker{label, edge.Constraint, childName, edge.Conditional}
				rendered := blocker.render()
				dup := false
				for _, b := range blockers {
					if b.render() == rendered {
						dup = true
						break
					}
				}
				if !dup {
					blockers = append(blockers, blocker)
				}
			}
		}
	}

	entrypoints := graph.entrypoints(nodeIDs)
	isDirect := false
	for _, nodeID := range nodeIDs {
		if _, ok := graph.Direct[nodeID]; ok {
			isDirect = true
			break
		}
	}

	var kind string
	switch {
	case len(blockers) > 0:
		kind = "blocked"
	case isDirect:
		allowed := true
		for _, nodeID := range nodeIDs {
			d, ok := graph.Direct[nodeID]
			if !ok {
				continue
			}
			allows := constraintAllows(d.Constraint, target)
			if allows == nil || !*allows {
				allowed = false
				break
			}
		}
		if allowed {
			kind = "lock-only"
		} else {
			kind = "direct"
		}
	default:
		kind = "lock-only"
	}

	score := kindWeight[kind]
	if _, ok := kindWeight[kind]; !ok {
		score = 2
	}
	if w, ok := gapWeight[gap]; ok {
		score += w
	} else {
		score += 3
	}
	if len(blockers) > 1 {
		score++
	}
	var ease string
	switch {
	case score <= 1:
		ease = "easy"
	case score <= 3:
		ease = "medium"
	default:
		ease = "hard"
	}

	hint := buildHint(graph, name, target, kind, blockers, nodeIDs)
	return Analysis{kind, ease, hint, blockers, dependents, entrypoints, nodeIDs}
}
