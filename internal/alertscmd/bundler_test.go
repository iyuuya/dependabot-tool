package alertscmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleGemfileLock = `GIT
  remote: https://github.com/example/arproxy.git
  revision: abc123
  specs:
    arproxy (0.2.9)
      activerecord (>= 4.2.0)

GEM
  remote: https://rubygems.org/
  specs:
    activerecord (7.2.0)
      activemodel (= 7.2.0)
      activesupport (= 7.2.0)
      timeout (>= 0.4.0)
    activemodel (7.2.0)
      activesupport (= 7.2.0)
    activesupport (7.2.0)
      concurrent-ruby (~> 1.0, >= 1.3.1)
      i18n (>= 1.6, < 2)
    concurrent-ruby (1.3.4)
    i18n (1.14.5)
      concurrent-ruby (~> 1.0)
    nokogiri (1.16.7)
    nokogiri (1.16.7-arm64-darwin)
    rack (3.1.8)
    rails (7.2.0)
      actionpack (= 7.2.0)
      activerecord (= 7.2.0)
      activesupport (= 7.2.0)
    actionpack (7.2.0)
      rack (>= 2.2.4)
      activesupport (= 7.2.0)
    timeout (0.4.1)

PLATFORMS
  arm64-darwin
  ruby

DEPENDENCIES
  arproxy!
  nokogiri (~> 1.16)
  rails (~> 7.2.0)
  rack (>= 3.0)

BUNDLED WITH
  2.5.11
`

func TestParseBundlerLock(t *testing.T) {
	lock := parseBundlerLock(sampleGemfileLock)

	byName := map[string]bundlerSpec{}
	for _, s := range lock.specs {
		byName[normalizeGem(s.name)] = s
	}

	ar, ok := byName["activerecord"]
	if !ok {
		t.Fatal("expected activerecord in specs")
	}
	if ar.version != "7.2.0" {
		t.Fatalf("activerecord version: got %q", ar.version)
	}
	if len(ar.deps) < 3 {
		t.Fatalf("activerecord deps: got %d, want >= 3", len(ar.deps))
	}

	// platform 別エントリは畳まれて 1 件
	if _, ok := byName["nokogiri"]; !ok {
		t.Fatal("expected nokogiri")
	}
	nokoCount := 0
	for _, s := range lock.specs {
		if normalizeGem(s.name) == "nokogiri" {
			nokoCount++
		}
	}
	if nokoCount != 1 {
		t.Fatalf("nokogiri merged count: got %d, want 1", nokoCount)
	}
	if byName["nokogiri"].version != "1.16.7" {
		t.Fatalf("nokogiri version should strip platform, got %q", byName["nokogiri"].version)
	}

	if len(lock.direct) != 4 {
		t.Fatalf("direct deps: got %d, want 4: %+v", len(lock.direct), lock.direct)
	}
	direct := map[string]bundlerDirect{}
	for _, d := range lock.direct {
		direct[normalizeGem(d.name)] = d
	}
	if !direct["arproxy"].opaque {
		t.Fatal("arproxy should be opaque (!)")
	}
	if direct["rails"].constraint != "~> 7.2.0" {
		t.Fatalf("rails constraint: got %q", direct["rails"].constraint)
	}
	if direct["nokogiri"].constraint != "~> 1.16" {
		t.Fatalf("nokogiri constraint: got %q", direct["nokogiri"].constraint)
	}
}

func TestBuildBundlerGraph(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "Gemfile.lock")
	if err := os.WriteFile(lockPath, []byte(sampleGemfileLock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte("source 'https://rubygems.org'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	graph, err := buildBundlerGraph(lockPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Ecosystem != "rubygems" {
		t.Fatalf("ecosystem: got %q", graph.Ecosystem)
	}

	nodes := graph.lookup("ActiveRecord")
	if len(nodes) != 1 {
		t.Fatalf("lookup ActiveRecord: got %v", nodes)
	}
	if graph.Versions[nodes[0]] != "7.2.0" {
		t.Fatalf("version: got %q", graph.Versions[nodes[0]])
	}

	parents := graph.Parents[normalizeGem("activemodel")]
	if len(parents) == 0 {
		t.Fatal("expected activemodel to have parents")
	}
	found := false
	for _, e := range parents {
		if e.Parent == normalizeGem("activerecord") && e.Constraint == "= 7.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected activerecord → activemodel edge, got %+v", parents)
	}

	if _, ok := graph.Direct[normalizeGem("rails")]; !ok {
		t.Fatal("rails should be direct")
	}
	if d := graph.Direct[normalizeGem("arproxy")]; !d.Opaque {
		t.Fatal("arproxy should be opaque direct")
	}

	entry := graph.entrypoints([]string{normalizeGem("activesupport")})
	if len(entry) == 0 {
		t.Fatal("expected entrypoints for activesupport")
	}
}

func TestLocalVersionsRubygems(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile.lock"), []byte(sampleGemfileLock), 0o644); err != nil {
		t.Fatal(err)
	}

	local := newLocalVersions(dir)
	versions, source := local.resolve("rubygems", "Gemfile.lock", "rails")
	if source != "Gemfile.lock" {
		t.Fatalf("source: got %q", source)
	}
	if len(versions) != 1 || versions[0] != "7.2.0" {
		t.Fatalf("versions: got %v", versions)
	}

	versions, source = local.resolve("rubygems", "Gemfile", "Nokogiri")
	if len(versions) != 1 || versions[0] != "1.16.7" {
		t.Fatalf("via Gemfile manifest: got %v (%s)", versions, source)
	}

	versions, source = local.resolve("bundler", "Gemfile.lock", "missing-gem")
	if versions != nil || source != "not-found" {
		t.Fatalf("missing: got %v (%s)", versions, source)
	}
}

func TestConstraintAllowsBundlerPessimistic(t *testing.T) {
	cases := []struct {
		spec, version string
		want          bool
	}{
		{"~> 1.16", "1.16.7", true},
		{"~> 1.16", "1.17.0", true},
		{"~> 1.16", "2.0.0", false},
		{"~> 7.2.0", "7.2.1", true},
		{"~> 7.2.0", "7.3.0", false},
		{"~> 1.0, >= 1.3.1", "1.3.4", true},
		{"~> 1.0, >= 1.3.1", "1.2.0", false},
		{">= 1.6, < 2", "1.14.5", true},
		{">= 1.6, < 2", "2.0.0", false},
		{"= 7.2.0", "7.2.0", true},
		{"= 7.2.0", "7.2.1", false},
	}
	for _, tc := range cases {
		got := constraintAllows(tc.spec, tc.version)
		if got == nil {
			t.Fatalf("%s vs %s: undeterminable", tc.spec, tc.version)
		}
		if *got != tc.want {
			t.Fatalf("%s vs %s: got %v, want %v", tc.spec, tc.version, *got, tc.want)
		}
	}
}

func TestBuildHintRubygems(t *testing.T) {
	g := newDependencyGraph("rubygems", "Gemfile.lock", ".")
	g.addNode("rails", "rails", "7.2.0", "rails")
	g.Direct["rails"] = DirectDep{Constraint: "~> 7.2.0", Group: "main", Manifest: "Gemfile"}

	hint := buildHint(g, "rails", "8.0.0", "direct", nil, []string{"rails"})
	if !strings.Contains(hint, "Gemfile") || !strings.Contains(hint, "bundle update rails") {
		t.Fatalf("direct hint: %q", hint)
	}

	hint = buildHint(g, "nokogiri", "1.17.0", "lock-only", nil, []string{"nokogiri"})
	if hint != "bundle update nokogiri" {
		t.Fatalf("lock-only hint: %q", hint)
	}
}
