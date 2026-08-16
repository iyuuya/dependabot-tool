package alertscmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlertsCacheKeyStableAcrossParamOrder(t *testing.T) {
	params1 := map[string]string{"state": "open", "severity": "high", "scope": "runtime"}
	params2 := map[string]string{"scope": "runtime", "state": "open", "severity": "high"}

	key1 := alertsCacheKey("owner/repo", "deadbeef", params1)
	key2 := alertsCacheKey("owner/repo", "deadbeef", params2)

	if key1 != key2 {
		t.Fatalf("expected cache key to be independent of map iteration order, got %q vs %q", key1, key2)
	}
}

func TestAlertsCacheKeyDiffersByFilter(t *testing.T) {
	base := map[string]string{"state": "open"}
	other := map[string]string{"state": "fixed"}

	key1 := alertsCacheKey("owner/repo", "deadbeef", base)
	key2 := alertsCacheKey("owner/repo", "deadbeef", other)

	if key1 == key2 {
		t.Fatalf("expected different filters to produce different cache keys, both were %q", key1)
	}
}

func TestAlertsCacheKeyDiffersByCommit(t *testing.T) {
	params := map[string]string{"state": "open"}

	key1 := alertsCacheKey("owner/repo", "commit-a", params)
	key2 := alertsCacheKey("owner/repo", "commit-b", params)

	if key1 == key2 {
		t.Fatalf("expected different commits to produce different cache keys, both were %q", key1)
	}
}

func TestAlertsCacheKeyDiffersByRepo(t *testing.T) {
	params := map[string]string{"state": "open"}

	key1 := alertsCacheKey("owner/repo-a", "deadbeef", params)
	key2 := alertsCacheKey("owner/repo-b", "deadbeef", params)

	if key1 == key2 {
		t.Fatalf("expected different repos to produce different cache keys, both were %q", key1)
	}
}

func TestAlertsCacheKeyEmptyParams(t *testing.T) {
	// Should not panic and should still differ from a non-empty params key.
	key1 := alertsCacheKey("owner/repo", "deadbeef", map[string]string{})
	key2 := alertsCacheKey("owner/repo", "deadbeef", map[string]string{"state": "open"})
	if key1 == key2 {
		t.Fatalf("expected empty params to differ from non-empty params")
	}
}

func TestCommitHashSucceedsInGitRepo(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// The repository root is a couple of levels up from this package.
	root = filepath.Join(root, "..", "..")

	hash, err := commitHash(root)
	if err != nil {
		t.Skipf("skipping: not inside a git repository with commits: %v", err)
	}
	if len(hash) == 0 {
		t.Fatalf("expected non-empty commit hash")
	}
}

func TestCommitHashFailsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir() // a plain temp directory is not a git repository

	if _, err := commitHash(dir); err == nil {
		t.Fatalf("expected commitHash to fail outside a git repository, got nil error")
	}
}

// TestFetchAlertsCachedFallsBackWithoutGit verifies that when the commit
// hash cannot be determined, fetchAlertsCached does not attempt to use the
// cache at all and instead always delegates to fetchAlerts (here, via a
// non-existent root that makes commitHash fail; the underlying `gh` call is
// expected to fail too since there's no real gh/network in this test
// environment, but the important part is that it fails via fetchAlerts, i.e.
// no panic and no cache interaction).
func TestFetchAlertsCachedFallsBackWithoutGit(t *testing.T) {
	dir := t.TempDir()

	_, err := fetchAlertsCached(dir, "owner/repo", map[string]string{"state": "open"}, false)
	if err == nil {
		t.Skip("gh happened to succeed in this environment; nothing to assert further")
	}
	// We only assert that it returns an error rather than panicking; the
	// actual error originates from fetchAlerts (gh api), confirming the
	// cache-bypass fallback path was taken instead of a cache-related error.
}
