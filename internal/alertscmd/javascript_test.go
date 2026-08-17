package alertscmd

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLockFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPackageLockGraphAndLocalVersions(t *testing.T) {
	dir := writeLockFixture(t, map[string]string{
		"package.json": `{"dependencies":{"app":"^1.0.0"}}`,
		"package-lock.json": `{
  "lockfileVersion": 3,
  "packages": {
    "": {"dependencies":{"app":"^1.0.0"}},
    "node_modules/app": {"version":"1.0.0","dependencies":{"library":"^2.0.0"}},
    "node_modules/library": {"version":"2.1.0"}
  }
}`,
	})
	lock := filepath.Join(dir, "package-lock.json")
	graph, err := buildPackageLockGraph(lock, dir)
	if err != nil {
		t.Fatal(err)
	}
	app := graph.lookup("app")
	library := graph.lookup("library")
	if len(app) != 1 || len(library) != 1 {
		t.Fatalf("nodes: app=%v library=%v", app, library)
	}
	if graph.Parents[library[0]][0].Parent != app[0] {
		t.Fatalf("library parent: %+v", graph.Parents[library[0]])
	}
	if _, ok := graph.Direct[app[0]]; !ok {
		t.Fatal("app should be a direct dependency")
	}
	versions, source := newLocalVersions(dir).resolve("npm", "package.json", "library")
	if len(versions) != 1 || versions[0] != "2.1.0" || source != "package-lock.json" {
		t.Fatalf("local library: %v from %s", versions, source)
	}
}

func TestPnpmGraphAndLocalVersions(t *testing.T) {
	dir := writeLockFixture(t, map[string]string{
		"package.json": `{"dependencies":{"app":"^1.0.0"}}`,
		"pnpm-lock.yaml": `lockfileVersion: '9.0'
importers:
  .:
    dependencies:
      app:
        specifier: ^1.0.0
        version: 1.0.0
packages:
  app@1.0.0: {}
  library@2.1.0: {}
snapshots:
  app@1.0.0:
    dependencies:
      library: 2.1.0
  library@2.1.0: {}
`,
	})
	lock := filepath.Join(dir, "pnpm-lock.yaml")
	graph, err := buildPnpmGraph(lock, dir)
	if err != nil {
		t.Fatal(err)
	}
	app, library := graph.lookup("app"), graph.lookup("library")
	if len(app) != 1 || len(library) != 1 {
		t.Fatalf("nodes: app=%v library=%v", app, library)
	}
	if len(graph.Parents[library[0]]) != 1 || graph.Parents[library[0]][0].Parent != app[0] {
		t.Fatalf("library parents: %+v", graph.Parents[library[0]])
	}
	if _, ok := graph.Direct[app[0]]; !ok {
		t.Fatal("app should be direct")
	}
	versions, source := newLocalVersions(dir).resolve("npm", "package.json", "library")
	if len(versions) != 1 || versions[0] != "2.1.0" || source != "pnpm-lock.yaml" {
		t.Fatalf("local library: %v from %s", versions, source)
	}
}

func TestYarnClassicAndBerryGraphs(t *testing.T) {
	tests := []struct {
		name, lock string
	}{
		{"classic", `# yarn lockfile v1
"app@^1.0.0":
  version "1.0.0"
  dependencies:
    library "^2.0.0"

"library@^2.0.0":
  version "2.1.0"
`},
		{"berry", `__metadata:
  version: 8
"app@npm:^1.0.0":
  version: 1.0.0
  dependencies:
    library: "npm:^2.0.0"
"library@npm:^2.0.0":
  version: 2.1.0
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeLockFixture(t, map[string]string{
				"package.json": `{"dependencies":{"app":"^1.0.0"}}`,
				"yarn.lock":    tc.lock,
			})
			graph, err := buildYarnClassicGraph(filepath.Join(dir, "yarn.lock"), dir)
			if err != nil {
				t.Fatal(err)
			}
			app, library := graph.lookup("app"), graph.lookup("library")
			if len(app) != 1 || len(library) != 1 {
				t.Fatalf("nodes: app=%v library=%v", app, library)
			}
			if len(graph.Parents[library[0]]) != 1 || graph.Parents[library[0]][0].Parent != app[0] {
				t.Fatalf("library parents: %+v", graph.Parents[library[0]])
			}
			versions, source := newLocalVersions(dir).resolve("npm", "package.json", "library")
			if len(versions) != 1 || versions[0] != "2.1.0" || source != "yarn.lock" {
				t.Fatalf("local library: %v from %s", versions, source)
			}
		})
	}
}
