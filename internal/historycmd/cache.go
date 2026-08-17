package historycmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/iyuuya/dependabot-tool/internal/cache"
)

// Cache namespaces for the "history" subcommand. Each cache gets its own
// namespace (internal/cache stores each namespace under its own
// subdirectory of ~/.cache/dependabot-tool/) so the three caches don't end
// up as indistinguishable hashed filenames in one shared directory.
const (
	alertsCacheNamespace    = "history/alerts"
	openPRsCacheNamespace   = "history/open-prs"
	closedPRsCacheNamespace = "history/closed-prs"
)

// refreshTargets selects which of the three history caches (alerts, open
// PRs, closed PRs) a run should bypass and re-fetch from gh api.
type refreshTargets struct {
	Alerts bool
	Open   bool
	Closed bool
}

// parseRefreshFlag parses the --refresh flag value into the set of caches
// to bypass: a comma-separated list of "alerts", "open", "closed", and
// "all". An empty value refreshes nothing (use every cache as-is).
func parseRefreshFlag(value string) (refreshTargets, error) {
	var targets refreshTargets

	if value == "" {
		return targets, nil
	}

	for part := range strings.SplitSeq(value, ",") {
		switch strings.TrimSpace(part) {
		case "alerts":
			targets.Alerts = true
		case "open":
			targets.Open = true
		case "closed":
			targets.Closed = true
		case "all":
			targets = refreshTargets{Alerts: true, Open: true, Closed: true}
		default:
			return refreshTargets{}, fmt.Errorf(
				"invalid --refresh value %q: expected a comma-separated list of alerts, open, closed, all",
				part,
			)
		}
	}

	return targets, nil
}

// ------------------------------------------------------------
// Alerts cache: one entry per repo, refetched once per UTC run date. The
// key is repo-only (not date-scoped) so a given repo always maps to the
// same cache file instead of accumulating one abandoned file per day; the
// cached run date lives in the value and is checked to decide freshness.
// ------------------------------------------------------------

// alertsCache stores the alerts fetched for a repo along with the UTC date
// they were fetched on, so a stale (previous-day) entry can be detected and
// refreshed instead of reused.
type alertsCache struct {
	Alerts  []Alert `json:"alerts"`
	RunDate string  `json:"run_date"`
}

func alertsCacheKey(repo string) string {
	return fmt.Sprintf("repo=%s", repo)
}

// fetchAlerts returns the Dependabot alerts for repo, using the on-disk
// cache for the current UTC day unless refresh is true.
func fetchAlerts(repo, runDate string, refresh bool) ([]Alert, error) {
	key := alertsCacheKey(repo)

	if !refresh {
		var cached alertsCache
		found, err := cache.Load(alertsCacheNamespace, key, &cached)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to read alerts cache: %v\n", err)
		} else if found && cached.RunDate == runDate {
			return cached.Alerts, nil
		}
	}

	alerts, err := getAlerts(repo)
	if err != nil {
		return nil, err
	}

	if err := cache.Save(alertsCacheNamespace, key, alertsCache{Alerts: alerts, RunDate: runDate}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write alerts cache: %v\n", err)
	}

	return alerts, nil
}

// ------------------------------------------------------------
// Open PR cache: one entry per repo, refetched once per UTC run date. Open
// PR counts are small, so refetching the full set once a day is cheap. Like
// the alerts cache, the key is repo-only and freshness is tracked in the
// value to avoid accumulating one file per day.
// ------------------------------------------------------------

// openPRsCache stores the open PRs fetched for a repo along with the UTC
// date they were fetched on.
type openPRsCache struct {
	PRs     []PullRequest `json:"prs"`
	RunDate string        `json:"run_date"`
}

func openPRsCacheKey(repo string) string {
	return fmt.Sprintf("repo=%s", repo)
}

// fetchOpenPRs returns the currently open pull requests for repo, using the
// on-disk cache for the current UTC day unless refresh is true.
func fetchOpenPRs(repo, runDate string, refresh bool) ([]PullRequest, error) {
	key := openPRsCacheKey(repo)

	if !refresh {
		var cached openPRsCache
		found, err := cache.Load(openPRsCacheNamespace, key, &cached)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to read open PR cache: %v\n", err)
		} else if found && cached.RunDate == runDate {
			return cached.PRs, nil
		}
	}

	prs, err := getOpenDependabotPRs(repo)
	if err != nil {
		return nil, err
	}

	if err := cache.Save(openPRsCacheNamespace, key, openPRsCache{PRs: prs, RunDate: runDate}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write open PR cache: %v\n", err)
	}

	return prs, nil
}

// ------------------------------------------------------------
// Closed PR cache: one entry per repo, updated incrementally. Instead of
// re-fetching the repo's entire closed-PR history on every run, we keep the
// full set found so far plus the newest updated_at seen, and only fetch
// pages newer than that on subsequent runs.
// ------------------------------------------------------------

// closedPRCache stores every closed pull request fetched so far for a repo.
type closedPRCache struct {
	PRs           []PullRequest `json:"prs"`
	LastUpdatedAt string        `json:"last_updated_at"`
}

func closedPRsCacheKey(repo string) string {
	return fmt.Sprintf("repo=%s", repo)
}

// fetchClosedPRs returns the full known set of closed pull requests for
// repo. Normally it fetches only what changed since the last run (by
// updated_at) and merges that into the cached set; passing refresh=true
// discards the cache and rebuilds it from a full walk of the repo's
// closed-PR history, which is expensive and should be opt-in
// (--refresh=closed or --refresh=all) rather than the default, since
// closed PRs are effectively immutable.
func fetchClosedPRs(repo string, refresh bool) ([]PullRequest, error) {
	key := closedPRsCacheKey(repo)

	var cached closedPRCache

	if !refresh {
		found, err := cache.Load(closedPRsCacheNamespace, key, &cached)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to read closed PR cache: %v\n", err)
			cached = closedPRCache{}
		} else if !found {
			cached = closedPRCache{}
		}
	}

	var since time.Time
	if cached.LastUpdatedAt != "" {
		since, _ = parseDate(cached.LastUpdatedAt)
	}

	fetched, err := getClosedDependabotPRsSince(repo, since)
	if err != nil {
		return nil, err
	}

	merged := mergePRsByNumber(cached.PRs, fetched)

	newest := cached.LastUpdatedAt
	for _, pr := range fetched {
		if pr.UpdatedAt > newest {
			newest = pr.UpdatedAt
		}
	}

	if err := cache.Save(closedPRsCacheNamespace, key, closedPRCache{PRs: merged, LastUpdatedAt: newest}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write closed PR cache: %v\n", err)
	}

	return merged, nil
}

// mergePRsByNumber combines cached and freshly fetched pull requests,
// de-duplicating by PR number. Entries in fresh take precedence over
// matching entries in cached, since they reflect the more recent state.
func mergePRsByNumber(cached, fresh []PullRequest) []PullRequest {
	byNumber := make(map[int]PullRequest, len(cached)+len(fresh))

	for _, pr := range cached {
		byNumber[pr.Number] = pr
	}

	for _, pr := range fresh {
		byNumber[pr.Number] = pr
	}

	result := make([]PullRequest, 0, len(byNumber))
	for _, pr := range byNumber {
		result = append(result, pr)
	}

	return result
}

// ------------------------------------------------------------

// fetchHistoryData returns the Dependabot alerts and merged PRs for repo,
// bypassing whichever caches refresh selects.
func fetchHistoryData(
	repo string,
	now time.Time,
	sinceTime time.Time,
	refresh refreshTargets,
) ([]Alert, []PullRequest, error) {
	runDate := now.Format("2006-01-02")

	alerts, err := fetchAlerts(repo, runDate, refresh.Alerts)
	if err != nil {
		return nil, nil, err
	}

	openPRs, err := fetchOpenPRs(repo, runDate, refresh.Open)
	if err != nil {
		return nil, nil, err
	}

	closedPRs, err := fetchClosedPRs(repo, refresh.Closed)
	if err != nil {
		return nil, nil, err
	}

	allPRs := make([]PullRequest, 0, len(openPRs)+len(closedPRs))
	allPRs = append(allPRs, openPRs...)
	allPRs = append(allPRs, closedPRs...)

	prs := filterMergedSince(allPRs, sinceTime)

	return alerts, prs, nil
}
