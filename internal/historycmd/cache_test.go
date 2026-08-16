package historycmd

import "testing"

func TestHistoryCacheKey_SameInputsSameKey(t *testing.T) {
	k1 := historyCacheKey("owner/repo", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo", "2026-08-16", 14, "")

	if k1 != k2 {
		t.Fatalf("expected identical keys for identical inputs, got %q and %q", k1, k2)
	}
}

func TestHistoryCacheKey_DifferentDateDifferentKey(t *testing.T) {
	k1 := historyCacheKey("owner/repo", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo", "2026-08-17", 14, "")

	if k1 == k2 {
		t.Fatalf("expected different keys for different run dates, got %q for both", k1)
	}
}

func TestHistoryCacheKey_DifferentRepoDifferentKey(t *testing.T) {
	k1 := historyCacheKey("owner/repo-a", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo-b", "2026-08-16", 14, "")

	if k1 == k2 {
		t.Fatalf("expected different keys for different repos, got %q for both", k1)
	}
}

func TestHistoryCacheKey_DifferentDaysDifferentKey(t *testing.T) {
	k1 := historyCacheKey("owner/repo", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo", "2026-08-16", 30, "")

	if k1 == k2 {
		t.Fatalf("expected different keys for different --days values, got %q for both", k1)
	}
}

func TestHistoryCacheKey_DifferentSinceDifferentKey(t *testing.T) {
	k1 := historyCacheKey("owner/repo", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo", "2026-08-16", 14, "2026-01-01")

	if k1 == k2 {
		t.Fatalf("expected different keys for different --since values, got %q for both", k1)
	}
}

// TestHistoryCacheKey_NoDelimiterCollision guards against two distinct
// queries accidentally producing the same key string because field values
// happen to contain characters that look like the key's separators.
func TestHistoryCacheKey_NoDelimiterCollision(t *testing.T) {
	k1 := historyCacheKey("owner/repo|date=2026-08-17", "2026-08-16", 14, "")
	k2 := historyCacheKey("owner/repo", "2026-08-17", 14, "")

	if k1 == k2 {
		t.Fatalf("expected no collision between crafted repo value and a different query, got %q for both", k1)
	}
}
