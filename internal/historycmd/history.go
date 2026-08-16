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
	Number   int    `json:"number"`
	Title    string `json:"title"`
	MergedAt string `json:"merged_at"`
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

// getDependabotPRs fetches closed pull requests for repo and returns those
// merged on/after since, sorted by merge time ascending. It mirrors
// get_dependabot_prs() from dependabot_history.py: the GitHub API paginates
// PRs by updated_at rather than created_at, so a broad fetch is filtered
// down by merged_at locally.
func getDependabotPRs(repo string, since time.Time) ([]PullRequest, error) {
	fmt.Fprintln(os.Stderr, "Fetching pull requests...")

	endpoint := fmt.Sprintf(
		"repos/%s/pulls?state=closed&sort=updated&direction=desc&per_page=100",
		repo,
	)

	prs, err := ghAPI[PullRequest](endpoint)
	if err != nil {
		return nil, err
	}

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

	return result, nil
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
