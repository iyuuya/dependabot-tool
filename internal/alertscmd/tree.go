package alertscmd

import (
	"fmt"
	"sort"
)

// --------------------------------------------------------------------------
// 逆依存ツリーの描画
// --------------------------------------------------------------------------

func verdictFor(constraint string, conditional bool, target string) string {
	var allows *bool
	if target != "" && target != "-" {
		allows = constraintAllows(constraint, target)
	}
	var mark string
	switch {
	case allows == nil:
		mark = "?"
	case *allows:
		mark = "OK"
	default:
		mark = "NG"
	}
	if conditional && mark == "NG" {
		return "NG?"
	}
	if conditional {
		return "?"
	}
	return mark
}

type edgeKey struct {
	parent     string
	constraint string
}

func renderReverseTree(graph *DependencyGraph, nodeIDs []string, target string, depthLimit, maxLines int) []string {
	var lines []string
	expanded := map[string]bool{}
	targets := map[string]bool{}
	for _, id := range nodeIDs {
		targets[id] = true
	}

	var walk func(nodeID, prefix string, depth int, path map[string]bool)
	walk = func(nodeID, prefix string, depth int, path map[string]bool) {
		if len(lines) >= maxLines {
			return
		}
		edges := graph.Parents[nodeID]
		uniqueOrder := []edgeKey{}
		unique := map[edgeKey]Edge{}
		for _, edge := range edges {
			key := edgeKey{edge.Parent, edge.Constraint}
			if _, ok := unique[key]; !ok {
				unique[key] = edge
				uniqueOrder = append(uniqueOrder, key)
			}
		}
		judged := targets[nodeID]

		ordered := make([]Edge, len(uniqueOrder))
		for i, k := range uniqueOrder {
			ordered[i] = unique[k]
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			ei, ej := ordered[i], ordered[j]
			okI := judged && verdictFor(ei.Constraint, ei.Conditional, target) == "OK"
			okJ := judged && verdictFor(ej.Constraint, ej.Conditional, target) == "OK"
			if okI != okJ {
				// Python sorts False before True (ascending bool), so
				// "OK" (true) sorts after non-OK (false).
				return !okI && okJ
			}
			return graph.label(ei.Parent) < graph.label(ej.Parent)
		})

		for i, edge := range ordered {
			if len(lines) >= maxLines {
				lines = append(lines, prefix+"… (表示上限)")
				return
			}
			last := i == len(ordered)-1
			branch := "├─ "
			childPrefix := prefix + "│  "
			if last {
				branch = "└─ "
				childPrefix = prefix + "   "
			}
			childName := graph.Names[nodeID]
			if childName == "" {
				childName = nodeID
			}
			constraint := edge.Constraint
			if constraint == "" {
				constraint = "*"
			}
			note := "[" + childName + " " + constraint
			if judged {
				note += " → " + verdictFor(edge.Constraint, edge.Conditional, target) + "]"
			} else {
				note += "]"
			}
			marker := ""
			if edge.Marker != "" {
				marker = " {" + edge.Marker + "}"
			}
			suffix := ""
			if direct, ok := graph.Direct[edge.Parent]; ok {
				value := direct.Constraint
				if direct.Opaque {
					value = "<" + direct.Constraint + ">"
				}
				suffix = fmt.Sprintf("  ★direct %s [%s] %s", direct.Manifest, direct.Group, value)
			}
			if edge.Kind != "runtime" {
				suffix += fmt.Sprintf("  (%s)", edge.Kind)
			}
			lines = append(lines, fmt.Sprintf("%s%s%s  %s%s%s", prefix, branch, graph.label(edge.Parent), note, marker, suffix))

			if path[edge.Parent] {
				lines = append(lines, childPrefix+"… (循環)")
				continue
			}
			if expanded[edge.Parent] {
				if len(graph.Parents[edge.Parent]) > 0 {
					lines = append(lines, childPrefix+"… (既出)")
				}
				continue
			}
			if depth+1 >= depthLimit {
				if len(graph.Parents[edge.Parent]) > 0 {
					lines = append(lines, childPrefix+"… (深さ上限)")
				}
				continue
			}
			expanded[edge.Parent] = true
			newPath := make(map[string]bool, len(path)+1)
			for k := range path {
				newPath[k] = true
			}
			newPath[edge.Parent] = true
			walk(edge.Parent, childPrefix, depth+1, newPath)
		}
	}

	for _, nodeID := range nodeIDs {
		direct, hasDirect := graph.Direct[nodeID]
		head := graph.label(nodeID)
		if hasDirect {
			value := direct.Constraint
			if direct.Opaque {
				value = "<" + direct.Constraint + ">"
			}
			head += fmt.Sprintf("  ★direct %s [%s] %s", direct.Manifest, direct.Group, value)
		}
		lines = append(lines, head)
		expanded[nodeID] = true
		walk(nodeID, "", 0, map[string]bool{nodeID: true})
		if len(graph.Parents[nodeID]) == 0 && !hasDirect {
			lines = append(lines, "  (逆依存なし: 孤立したロックエントリ)")
		}
	}
	return lines
}
