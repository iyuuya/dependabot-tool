package historycmd

import (
	"fmt"
	"os"
	"sort"
	"time"
)

// Alert models a single Dependabot alert as returned by the GitHub API.
type Alert struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	CreatedAt  string `json:"created_at"`
	FixedAt    string `json:"fixed_at"`
	Dependency struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
	} `json:"dependency"`
	SecurityAdvisory struct {
		Summary string `json:"summary"`
	} `json:"security_advisory"`
}

// PullRequest models the subset of a GitHub pull request we care about.
type PullRequest struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	MergedAt  string `json:"merged_at"`
	UpdatedAt string `json:"updated_at"`
}

// parseDate parses a GitHub timestamp (RFC3339, optionally with fractional
// seconds and a trailing "Z"), mirroring the Python parse_date() helper.
// It returns ok=false for empty or unparsable values.
func parseDate(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}

	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}

	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, true
	}

	return time.Time{}, false
}

// getAlerts fetches all Dependabot alerts for repo.
func getAlerts(repo string) ([]Alert, error) {
	fmt.Fprintln(os.Stderr, "Fetching Dependabot alerts...")

	return ghAPI[Alert](fmt.Sprintf("repos/%s/dependabot/alerts?per_page=100", repo))
}

// getOpenDependabotPRs fetches all currently open pull requests for repo.
// Open PR counts are small enough that fetching the full set every time is
// cheap, unlike walking the repo's entire closed-PR history.
func getOpenDependabotPRs(repo string) ([]PullRequest, error) {
	fmt.Fprintln(os.Stderr, "Fetching open pull requests...")

	endpoint := fmt.Sprintf("repos/%s/pulls?state=open&per_page=100", repo)

	return ghAPI[PullRequest](endpoint)
}

// getClosedDependabotPRsSince fetches closed pull requests for repo, paging
// newest-updated-first (mirroring the API's sort=updated&direction=desc
// order) and stopping as soon as it reaches a PR whose updated_at is not
// after sinceUpdatedAt. A zero sinceUpdatedAt fetches the full closed-PR
// history. Callers combine the result with a previously cached set of
// closed PRs rather than re-fetching everything on every run.
func getClosedDependabotPRsSince(repo string, sinceUpdatedAt time.Time) ([]PullRequest, error) {
	fmt.Fprintln(os.Stderr, "Fetching closed pull requests...")

	endpoint := fmt.Sprintf(
		"repos/%s/pulls?state=closed&sort=updated&direction=desc&per_page=100",
		repo,
	)

	const perPage = 100

	var result []PullRequest

	for page := 1; ; page++ {
		items, err := ghAPIPage[PullRequest](endpoint, page)
		if err != nil {
			return nil, err
		}

		if len(items) == 0 {
			break
		}

		kept, stop := filterPageSince(items, sinceUpdatedAt)
		result = append(result, kept...)

		if stop || len(items) < perPage {
			break
		}
	}

	return result, nil
}

// filterPageSince processes a single page of pull requests sorted
// newest-updated-first, keeping entries newer than sinceUpdatedAt. It
// returns stop=true as soon as it reaches an entry that is not newer than
// sinceUpdatedAt, signalling that the remaining (older) pages are already
// covered by the cache and pagination can stop. A zero sinceUpdatedAt never
// stops early.
func filterPageSince(items []PullRequest, sinceUpdatedAt time.Time) (kept []PullRequest, stop bool) {
	for _, pr := range items {
		updatedAt, ok := parseDate(pr.UpdatedAt)
		if ok && !sinceUpdatedAt.IsZero() && !updatedAt.After(sinceUpdatedAt) {
			return kept, true
		}

		kept = append(kept, pr)
	}

	return kept, false
}

// filterMergedSince keeps only pull requests merged on/after since, sorted
// by merge time ascending. Pull requests without a merged_at (e.g. still
// open, or closed without merging) are dropped. Mirrors the filtering
// previously done inline in getDependabotPRs.
func filterMergedSince(prs []PullRequest, since time.Time) []PullRequest {
	var result []PullRequest

	for _, pr := range prs {
		mergedAt, ok := parseDate(pr.MergedAt)
		if !ok {
			continue
		}

		if mergedAt.Before(since) {
			continue
		}

		result = append(result, pr)
	}

	sort.SliceStable(result, func(i, j int) bool {
		ti, _ := parseDate(result[i].MergedAt)
		tj, _ := parseDate(result[j].MergedAt)
		return ti.Before(tj)
	})

	return result
}

// countActiveAlerts counts the alerts that were active (open) at the given
// point in time: created_at <= timestamp and (fixed_at is absent or
// fixed_at > timestamp). Mirrors count_active_alerts().
func countActiveAlerts(alerts []Alert, timestamp time.Time) int {
	count := 0

	for _, alert := range alerts {
		createdAt, ok := parseDate(alert.CreatedAt)
		if !ok {
			continue
		}

		if createdAt.After(timestamp) {
			continue
		}

		if fixedAt, ok := parseDate(alert.FixedAt); ok && !fixedAt.After(timestamp) {
			continue
		}

		count++
	}

	return count
}
