package alertscmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// rowValues returns the values of r in rowFieldOrder order (for CSV output).
func rowValues(r Row) []string {
	return []string{
		strconv.Itoa(r.Number),
		r.State,
		r.Severity,
		r.Ecosystem,
		r.Name,
		r.ManifestPath,
		r.Scope,
		r.VulnerableRange,
		r.PatchedVersion,
		r.LocalVersion,
		r.LocalSource,
		r.Gap,
		r.Affected,
		r.Ease,
		r.FixKind,
		r.FixHint,
		r.Blockers,
		r.Dependents,
		r.Entrypoints,
		r.GhsaID,
		r.Summary,
		r.HTMLURL,
	}
}

func splitCSVSet(value string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			set[part] = true
		}
	}
	return set
}

// Run is the entry point for the `dependabot-tool alerts` subcommand.
func Run(args []string) error {
	fs := flag.NewFlagSet("alerts", flag.ContinueOnError)

	repo := fs.String("repo", "", "owner/repo (省略時は gh repo view で自動判定)")
	state := fs.String("state", "open", fmt.Sprintf("取得する state のカンマ区切り (%s)、'all' で全件。デフォルト: open", strings.Join(validStates, "/")))
	severity := fs.String("severity", "", "severity のカンマ区切り (low/medium/high/critical)")
	ecosystem := fs.String("ecosystem", "", "ecosystem のカンマ区切り (pip/npm/composer/...)")
	pkg := fs.String("package", "", "パッケージ名のカンマ区切り")
	scope := fs.String("scope", "", "依存スコープで絞る (runtime/development)")
	minSeverity := fs.String("min-severity", "", "この severity 以上だけ表示 (low/medium/high/critical)")
	gapFlag := fs.String("gap", "", "レベル差で絞る (major/minor/patch/other/up-to-date/unknown のカンマ区切り)")
	easeFlag := fs.String("ease", "", "上げやすさで絞る (easy/medium/hard/? のカンマ区切り)")
	fixKind := fs.String("fix-kind", "", "対応の種類で絞る (lock-only/direct/blocked/no-patch/unknown のカンマ区切り)")
	affectedOnly := fs.Bool("affected-only", false, "手元のバージョンが脆弱範囲に該当するものだけ表示")
	format := fs.String("format", "table", "出力形式 (table/tree/json/csv)")
	summary := fs.Bool("summary", false, "table 出力に advisory の概要列を追加")
	action := fs.Bool("action", false, "table 出力に対応方法 (ACTION) の列を追加")
	sortFlag := fs.String("sort", "", "並び順 (severity/ease)。デフォルト: table/json/csv は severity、tree は ease")
	treeDepth := fs.Int("tree-depth", 8, "逆依存ツリーの深さ上限 (デフォルト: 8)")
	treeLines := fs.Int("tree-lines", 120, "1 パッケージあたりのツリー行数上限")
	pythonVersion := fs.String("python-version", defaultPythonVersion, fmt.Sprintf("環境マーカー評価に使う Python バージョン (デフォルト: %s)", defaultPythonVersion))
	rootFlag := fs.String("root", os.Getenv("REPO_ROOT"), "ロックファイル探索のルート (省略時は git のトップレベル)")
	alertsFile := fs.String("alerts-file", "", "gh を呼ばずにこのファイルの JSON を使う (デバッグ用)")
	refresh := fs.Bool("refresh", false, "キャッシュを無視して gh api を実行し直す")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	switch *scope {
	case "", "runtime", "development":
	default:
		return fmt.Errorf("invalid --scope: %s (有効: runtime, development)", *scope)
	}
	if *minSeverity != "" {
		if _, ok := severityOrder[*minSeverity]; !ok {
			return fmt.Errorf("invalid --min-severity: %s (有効: low, medium, high, critical)", *minSeverity)
		}
	}
	switch *format {
	case "table", "tree", "json", "csv":
	default:
		return fmt.Errorf("invalid --format: %s (有効: table, tree, json, csv)", *format)
	}
	switch *sortFlag {
	case "", "severity", "ease":
	default:
		return fmt.Errorf("invalid --sort: %s (有効: severity, ease)", *sortFlag)
	}

	root, err := resolveRoot(*rootFlag)
	if err != nil {
		return err
	}

	var alerts []map[string]interface{}
	if *alertsFile != "" {
		data, err := os.ReadFile(*alertsFile)
		if err != nil {
			return err
		}
		alerts, err = decodeAlerts(string(data))
		if err != nil {
			return err
		}
	} else {
		repoName := *repo
		if repoName == "" {
			repoName, err = detectRepo()
			if err != nil {
				return err
			}
		}
		params := map[string]string{}
		if strings.ToLower(*state) != "all" {
			var states []string
			var invalid []string
			for _, s := range strings.Split(*state, ",") {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				states = append(states, s)
				if !contains(validStates, s) {
					invalid = append(invalid, s)
				}
			}
			if len(invalid) > 0 {
				return fmt.Errorf("不正な state: %s (有効: %s, all)", strings.Join(invalid, ", "), strings.Join(validStates, ", "))
			}
			params["state"] = strings.Join(states, ",")
		}
		if *severity != "" {
			params["severity"] = *severity
		}
		if *ecosystem != "" {
			params["ecosystem"] = *ecosystem
		}
		if *pkg != "" {
			params["package"] = *pkg
		}
		if *scope != "" {
			params["scope"] = *scope
		}
		alerts, err = fetchAlertsCached(root, repoName, params, *refresh)
		if err != nil {
			return err
		}
	}

	graphs := newGraphCache(root, *pythonVersion)
	rows := buildRows(alerts, newLocalVersions(root), graphs)

	if *severity != "" {
		wanted := map[string]bool{}
		for k := range splitCSVSet(*severity) {
			wanted[strings.ToLower(k)] = true
		}
		rows = filterRows(rows, func(r Row) bool { return wanted[strings.ToLower(r.Severity)] })
	}
	if *ecosystem != "" {
		wanted := map[string]bool{}
		for k := range splitCSVSet(*ecosystem) {
			wanted[strings.ToLower(k)] = true
		}
		rows = filterRows(rows, func(r Row) bool { return wanted[strings.ToLower(r.Ecosystem)] })
	}
	if *pkg != "" {
		wanted := map[string]bool{}
		for k := range splitCSVSet(*pkg) {
			wanted[strings.ToLower(k)] = true
		}
		rows = filterRows(rows, func(r Row) bool { return wanted[strings.ToLower(r.Name)] })
	}
	if *scope != "" {
		rows = filterRows(rows, func(r Row) bool { return r.Scope == *scope })
	}
	if *minSeverity != "" {
		floor := severityOrder[*minSeverity]
		rows = filterRows(rows, func(r Row) bool { return severityOrderOf(r.Severity) >= floor })
	}
	if *gapFlag != "" {
		wanted := splitCSVSet(*gapFlag)
		rows = filterRows(rows, func(r Row) bool { return wanted[r.Gap] })
	}
	if *easeFlag != "" {
		wanted := splitCSVSet(*easeFlag)
		rows = filterRows(rows, func(r Row) bool { return wanted[r.Ease] })
	}
	if *fixKind != "" {
		wanted := splitCSVSet(*fixKind)
		rows = filterRows(rows, func(r Row) bool { return wanted[r.FixKind] })
	}
	if *affectedOnly {
		rows = filterRows(rows, func(r Row) bool { return r.Affected == "yes" })
	}

	effectiveSort := *sortFlag
	if effectiveSort == "" {
		if *format == "tree" {
			effectiveSort = "ease"
		} else {
			effectiveSort = "severity"
		}
	}
	rows = sortRows(rows, effectiveSort)

	switch *format {
	case "json":
		return printJSON(rows)
	case "csv":
		return printCSV(rows)
	}

	if len(rows) == 0 {
		fmt.Println("該当するアラートはありません。")
		return nil
	}
	if *format == "tree" {
		fmt.Println(renderTree(buildGroups(rows, graphs), *treeDepth, *treeLines))
	} else {
		fmt.Println(renderTable(rows, *summary, *action))
	}
	fmt.Println(renderCounts(rows))
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func filterRows(rows []Row, keep func(Row) bool) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func printJSON(rows []Row) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func printCSV(rows []Row) error {
	w := csv.NewWriter(os.Stdout)
	// Python's csv module defaults to CRLF line endings; match that exactly.
	w.UseCRLF = true
	if len(rows) == 0 {
		if err := w.Write([]string{"name"}); err != nil {
			return err
		}
		w.Flush()
		return w.Error()
	}
	if err := w.Write(rowFieldOrder); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(rowValues(r)); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func resolveRoot(rootFlag string) (string, error) {
	if rootFlag != "" {
		return filepath.Abs(rootFlag)
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err == nil {
		if abs, err := filepath.Abs(strings.TrimSpace(stdout.String())); err == nil {
			return abs, nil
		}
	}
	return os.Getwd()
}
