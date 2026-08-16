package historycmd

import (
	"fmt"
	"os"
	"time"

	"github.com/iyuuya/dependabot-tool/internal/cache"
)

// historyCacheNamespace is the internal/cache namespace used to store
// getAlerts/getDependabotPRs results for the "history" subcommand.
const historyCacheNamespace = "history"

// cachedHistory bundles the two gh api results ("history" fetches) that are
// cached together, keyed by run date + query.
type cachedHistory struct {
	Alerts []Alert       `json:"alerts"`
	PRs    []PullRequest `json:"prs"`
}

// historyCacheKey builds the cache key for a given query: the repo, the run
// date (UTC, YYYY-MM-DD), and the --days/--since flags that determine which
// PRs are fetched. Running the same query again on the same UTC day reuses
// the cache; the date changing (or the query changing) produces a distinct
// entry.
func historyCacheKey(repo, runDate string, days int, since string) string {
	return fmt.Sprintf("repo=%s|date=%s|days=%d|since=%s", repo, runDate, days, since)
}

// loadHistoryCache looks up a previously cached (alerts, PRs) pair for the
// given query. found is false whenever there is no usable cache entry,
// including when reading/decoding the cache file fails; callers should just
// fall back to fetching fresh data in that case.
func loadHistoryCache(repo, runDate string, days int, since string) (data cachedHistory, found bool) {
	key := historyCacheKey(repo, runDate, days, since)

	found, err := cache.Load(historyCacheNamespace, key, &data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to read history cache: %v\n", err)
		return cachedHistory{}, false
	}

	return data, found
}

// saveHistoryCache stores (alerts, PRs) for the given query. Failures are
// reported but non-fatal: the cache is a pure optimization, so the command
// should still succeed using the freshly fetched data.
func saveHistoryCache(repo, runDate string, days int, since string, data cachedHistory) {
	key := historyCacheKey(repo, runDate, days, since)

	if err := cache.Save(historyCacheNamespace, key, data); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write history cache: %v\n", err)
	}
}

// fetchHistoryData returns the Dependabot alerts and merged PRs for repo,
// using the on-disk cache for the current UTC day/query unless refresh is
// true. On a cache miss (or when refresh is set) it calls the gh api via
// getAlerts/getDependabotPRs and stores the result for later reuse.
func fetchHistoryData(
	repo string,
	now time.Time,
	sinceTime time.Time,
	days int,
	sinceFlag string,
	refresh bool,
) ([]Alert, []PullRequest, error) {
	runDate := now.Format("2006-01-02")

	if !refresh {
		if data, found := loadHistoryCache(repo, runDate, days, sinceFlag); found {
			return data.Alerts, data.PRs, nil
		}
	}

	alerts, err := getAlerts(repo)
	if err != nil {
		return nil, nil, err
	}

	prs, err := getDependabotPRs(repo, sinceTime)
	if err != nil {
		return nil, nil, err
	}

	saveHistoryCache(repo, runDate, days, sinceFlag, cachedHistory{Alerts: alerts, PRs: prs})

	return alerts, prs, nil
}
