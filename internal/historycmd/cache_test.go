package historycmd

import "testing"

func TestParseRefreshFlag_Empty(t *testing.T) {
	got, err := parseRefreshFlag("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != (refreshTargets{}) {
		t.Fatalf("expected no targets for empty value, got %+v", got)
	}
}

func TestParseRefreshFlag_SingleTargets(t *testing.T) {
	cases := map[string]refreshTargets{
		"alerts": {Alerts: true},
		"open":   {Open: true},
		"closed": {Closed: true},
		"all":    {Alerts: true, Open: true, Closed: true},
	}

	for value, want := range cases {
		got, err := parseRefreshFlag(value)
		if err != nil {
			t.Fatalf("parseRefreshFlag(%q): unexpected error: %v", value, err)
		}
		if got != want {
			t.Fatalf("parseRefreshFlag(%q) = %+v, want %+v", value, got, want)
		}
	}
}

func TestParseRefreshFlag_CommaSeparatedCombinesTargets(t *testing.T) {
	got, err := parseRefreshFlag("alerts,closed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := refreshTargets{Alerts: true, Closed: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseRefreshFlag_TrimsWhitespace(t *testing.T) {
	got, err := parseRefreshFlag("alerts, open")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := refreshTargets{Alerts: true, Open: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseRefreshFlag_InvalidValue(t *testing.T) {
	if _, err := parseRefreshFlag("bogus"); err == nil {
		t.Fatalf("expected an error for an invalid --refresh value")
	}
}

func TestAlertsCacheKey_SameInputsSameKey(t *testing.T) {
	k1 := alertsCacheKey("owner/repo")
	k2 := alertsCacheKey("owner/repo")

	if k1 != k2 {
		t.Fatalf("expected identical keys for identical inputs, got %q and %q", k1, k2)
	}
}

func TestAlertsCacheKey_NotDateScoped(t *testing.T) {
	// The alerts cache is keyed by repo only, not by run date: date-scoping
	// the key would leave one abandoned cache file behind per day instead
	// of reusing/overwriting a single file. Freshness is tracked in the
	// cached value's RunDate field instead.
	k1 := alertsCacheKey("owner/repo")
	k2 := alertsCacheKey("owner/repo")

	if k1 != k2 {
		t.Fatalf("expected identical keys across calls, got %q and %q", k1, k2)
	}
}

func TestAlertsCacheKey_DifferentRepoDifferentKey(t *testing.T) {
	k1 := alertsCacheKey("owner/repo-a")
	k2 := alertsCacheKey("owner/repo-b")

	if k1 == k2 {
		t.Fatalf("expected different keys for different repos, got %q for both", k1)
	}
}

func TestOpenPRsCacheKey_NotDateScoped(t *testing.T) {
	k1 := openPRsCacheKey("owner/repo")
	k2 := openPRsCacheKey("owner/repo")

	if k1 != k2 {
		t.Fatalf("expected identical keys across calls, got %q and %q", k1, k2)
	}
}

func TestClosedPRsCacheKey_DifferentRepoDifferentKey(t *testing.T) {
	k1 := closedPRsCacheKey("owner/repo-a")
	k2 := closedPRsCacheKey("owner/repo-b")

	if k1 == k2 {
		t.Fatalf("expected different keys for different repos, got %q for both", k1)
	}
}

func TestClosedPRsCacheKey_NotDateScoped(t *testing.T) {
	// The closed-PR cache is keyed by repo only: it's updated incrementally
	// across runs rather than refetched fresh every day like alerts/open PRs.
	k1 := closedPRsCacheKey("owner/repo")
	k2 := closedPRsCacheKey("owner/repo")

	if k1 != k2 {
		t.Fatalf("expected identical keys across calls, got %q and %q", k1, k2)
	}
}

func TestMergePRsByNumber_DedupesAndPrefersFresh(t *testing.T) {
	cached := []PullRequest{
		{Number: 1, Title: "old title", UpdatedAt: "2026-08-01T00:00:00Z"},
		{Number: 2, Title: "unrelated", UpdatedAt: "2026-08-01T00:00:00Z"},
	}
	fresh := []PullRequest{
		{Number: 1, Title: "new title", UpdatedAt: "2026-08-10T00:00:00Z"},
		{Number: 3, Title: "brand new", UpdatedAt: "2026-08-10T00:00:00Z"},
	}

	merged := mergePRsByNumber(cached, fresh)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged PRs, got %d", len(merged))
	}

	byNumber := make(map[int]PullRequest, len(merged))
	for _, pr := range merged {
		byNumber[pr.Number] = pr
	}

	if got := byNumber[1].Title; got != "new title" {
		t.Fatalf("expected fresh entry to win for #1, got title %q", got)
	}
	if _, ok := byNumber[2]; !ok {
		t.Fatalf("expected cached-only PR #2 to survive the merge")
	}
	if _, ok := byNumber[3]; !ok {
		t.Fatalf("expected fresh-only PR #3 to survive the merge")
	}
}
