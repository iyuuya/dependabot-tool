package alertscmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/iyuuya/dependabot-tool/internal/cache"
)

// alertsCacheNamespace is the internal/cache namespace used for cached
// `gh api .../dependabot/alerts` responses.
const alertsCacheNamespace = "alerts"

// commitHash returns the current commit hash of the git repository rooted at
// root (via `git -C root rev-parse HEAD`). It returns an error if root is not
// inside a git repository (or git is unavailable), so callers can fall back
// to skipping the cache instead of failing outright.
func commitHash(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("commit hash を取得できません: %s", msg)
	}
	hash := strings.TrimSpace(stdout.String())
	if hash == "" {
		return "", fmt.Errorf("commit hash を取得できません: 空の出力")
	}
	return hash, nil
}

// alertsCacheKey builds a stable cache key for fetchAlerts(repo, params) at
// the given commit. The params map is serialized with sorted keys so that
// the resulting key does not depend on map iteration order, while different
// filter combinations still produce distinct keys.
func alertsCacheKey(repo, commit string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "repo=%s\ncommit=%s\n", repo, commit)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, params[k])
	}
	return b.String()
}

// fetchAlertsCached wraps fetchAlerts with an on-disk cache keyed by repo,
// current commit hash, and the filter params passed to `gh api`. If the
// commit hash cannot be determined (e.g. root is not a git repository), the
// cache is bypassed entirely and gh api is always called.
//
// When refresh is true, any existing cache entry is ignored and gh api is
// always called, with the fresh result overwriting the cache.
func fetchAlertsCached(root, repo string, params map[string]string, refresh bool) ([]map[string]interface{}, error) {
	commit, err := commitHash(root)
	if err != nil {
		return fetchAlerts(repo, params)
	}

	key := alertsCacheKey(repo, commit, params)

	if !refresh {
		var cached []map[string]interface{}
		found, err := cache.Load(alertsCacheNamespace, key, &cached)
		if err == nil && found {
			return cached, nil
		}
		// On cache read errors, fall through and fetch fresh rather than
		// failing the whole command.
	}

	alerts, err := fetchAlerts(repo, params)
	if err != nil {
		return nil, err
	}
	// Best-effort: a cache write failure shouldn't fail the command.
	_ = cache.Save(alertsCacheNamespace, key, alerts)
	return alerts, nil
}
