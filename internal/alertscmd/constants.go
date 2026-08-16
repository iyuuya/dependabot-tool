package alertscmd

var severityOrder = map[string]int{"low": 0, "medium": 1, "high": 2, "critical": 3}
var gapOrder = map[string]int{"major": 0, "minor": 1, "patch": 2, "other": 3, "up-to-date": 4, "unknown": 5}
var easeOrder = map[string]int{"easy": 0, "medium": 1, "hard": 2, "?": 3}

var validStates = []string{"open", "fixed", "dismissed", "auto_dismissed"}

const defaultPythonVersion = "3.11" // mise.toml の python に合わせる

func severityOrderOf(s string) int {
	if v, ok := severityOrder[s]; ok {
		return v
	}
	return -1
}

func gapOrderOf(g string) int {
	if v, ok := gapOrder[g]; ok {
		return v
	}
	return 9
}

func easeOrderOf(e string) int {
	if v, ok := easeOrder[e]; ok {
		return v
	}
	return 9
}
