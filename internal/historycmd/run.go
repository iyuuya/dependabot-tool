package historycmd

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

// Run is the entry point for the "history" subcommand. It shows the
// Dependabot alert Before/After counts around recently merged Dependabot
// pull requests, mirroring dependabot_history.py's main().
func Run(args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)

	days := fs.Int("days", 14, "Show PRs merged in the last N days. Default: 14")
	since := fs.String("since", "", "Show PRs merged on/after YYYY-MM-DD. Overrides --days.")
	refresh := fs.Bool("refresh", false, "Bypass the local cache and re-fetch from gh api, overwriting the cached result.")

	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: dependabot-tool history [repo] [flags]")
		fmt.Fprintln(fs.Output(), "Show Dependabot Alert Before/After changes for recently merged Dependabot PRs.")
		fs.PrintDefaults()
	}

	// flag.Parse stops treating tokens as flags as soon as it sees the first
	// non-flag argument, so `history <repo> --days 7` would silently ignore
	// --days. Reorder so flags and the positional repo argument can appear
	// in any order.
	flagArgs, positional := reorderArgs(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	var repo string
	if len(positional) > 0 {
		repo = positional[0]
	} else {
		r, err := getRepo()
		if err != nil {
			return err
		}
		repo = r
	}

	// ------------------------------------------------------------
	// 期間
	// ------------------------------------------------------------

	now := time.Now().UTC()

	var sinceTime time.Time
	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: expected YYYY-MM-DD", *since)
		}
		sinceTime = t
	} else {
		sinceTime = now.Add(-time.Duration(*days) * 24 * time.Hour)
	}

	// ------------------------------------------------------------
	// データ取得
	// ------------------------------------------------------------

	alerts, prs, err := fetchHistoryData(repo, now, sinceTime, *days, *since, *refresh)
	if err != nil {
		return err
	}

	// ------------------------------------------------------------
	// 出力
	// ------------------------------------------------------------

	fmt.Println()
	fmt.Printf("Repository : %s\n", repo)
	fmt.Printf("Period     : %s → %s\n", sinceTime.Format("2006-01-02"), now.Format("2006-01-02"))
	fmt.Printf("Dependabot PRs : %d\n", len(prs))
	fmt.Printf("Total alerts   : %d\n", len(alerts))
	fmt.Println()

	if len(prs) == 0 {
		fmt.Println("No merged Dependabot PRs found.")
		return nil
	}

	// ------------------------------------------------------------
	// Before / After
	//
	// Before:
	//   PR merge直前のAlert数
	//
	// After:
	//   PR merge直後に、そのPRによって解消された
	//   Alertを考慮したAlert数
	//
	// ただしGitHub APIではPRとAlertの直接的な対応関係を
	// 完全には取得できないため、fixed_atを利用する。
	// ------------------------------------------------------------

	fmt.Printf("%-10s %-7s %-7s %-7s %-6s TITLE\n", "DATE", "PR", "BEFORE", "AFTER", "Δ")
	fmt.Println(strings.Repeat("-", 120))

	for _, pr := range prs {
		mergedAt, _ := parseDate(pr.MergedAt)

		// --------------------------------------------------------
		// Before
		//
		// 今回のPRがmergeされる直前の状態。
		// --------------------------------------------------------

		before := countActiveAlerts(alerts, mergedAt)

		// --------------------------------------------------------
		// After
		//
		// fixed_at が今回のPRのmerge時刻以前になったAlertを
		// 除外した状態。
		//
		// ※ 実際のDependabot処理にはタイムラグがあるため、
		//    完全なリアルタイム値ではなく、GitHubのAlert履歴
		//    を元にした推定値。
		// --------------------------------------------------------

		after := countActiveAlerts(alerts, mergedAt)

		delta := after - before

		fmt.Printf(
			"%-10s #%-6d %7d %7d %6d %s\n",
			mergedAt.Format("2006-01-02"),
			pr.Number,
			before,
			after,
			delta,
			pr.Title,
		)
	}

	fmt.Println(strings.Repeat("-", 120))

	// ------------------------------------------------------------
	// 現在
	// ------------------------------------------------------------

	current := countActiveAlerts(alerts, now)

	fmt.Printf("%-10s %-7s %-7s %7d\n", "CURRENT", "", "", current)
	fmt.Println()

	// ------------------------------------------------------------
	// 現在のOpen Alert
	// ------------------------------------------------------------

	var openAlerts []Alert
	for _, alert := range alerts {
		if alert.State == "open" {
			openAlerts = append(openAlerts, alert)
		}
	}

	fmt.Printf("Current open alerts: %d\n", len(openAlerts))

	for _, alert := range openAlerts {
		pkg := alert.Dependency.Package.Name
		if pkg == "" {
			pkg = "?"
		}

		summary := alert.SecurityAdvisory.Summary
		if summary == "" {
			summary = "Unknown vulnerability"
		}

		fmt.Printf("  #%d %s - %s\n", alert.Number, pkg, summary)
	}

	return nil
}

// reorderArgs splits args into flag tokens and positional arguments so that
// flags may appear before or after the positional `repo` argument. The
// standard library's flag.Parse stops parsing flags at the first token that
// doesn't start with "-", which would silently ignore flags placed after
// repo (e.g. `history owner/repo --days 7`).
func reorderArgs(fs *flag.FlagSet, args []string) (flagArgs, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}

		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue // -flag=value already carries its value
		}
		if fl := fs.Lookup(name); fl != nil {
			if bv, ok := fl.Value.(interface{ IsBoolFlag() bool }); !ok || !bv.IsBoolFlag() {
				if i+1 < len(args) {
					i++
					flagArgs = append(flagArgs, args[i])
				}
			}
		}
	}
	return flagArgs, positional
}
