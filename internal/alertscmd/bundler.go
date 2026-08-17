package alertscmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --------------------------------------------------------------------------
// Bundler (Gemfile / Gemfile.lock) — ecosystem: rubygems
// --------------------------------------------------------------------------

// bundlerNameVersionRe mirrors Bundler's LockfileParser NAME_VERSION
// (without Perl lookahead; indent is exactly 2 / 4 / 6 spaces).
var bundlerNameVersionRe = regexp.MustCompile(
	`^(  |    |      )([^ ].*?)(?: \(([^-]*)(?:-(.*))?\))?(!)?(?: ([^ ]+))?$`,
)

type bundlerDep struct {
	name       string
	constraint string
}

type bundlerSpec struct {
	name    string
	version string
	deps    []bundlerDep
}

type bundlerDirect struct {
	name       string
	constraint string
	opaque     bool // path/git など (DEPENDENCIES の !)
}

type bundlerLock struct {
	specs  []bundlerSpec
	direct []bundlerDirect
}

func normalizeGem(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func parseBundlerLock(content string) bundlerLock {
	var lock bundlerLock
	section := ""
	var current *bundlerSpec
	flush := func() {
		if current != nil {
			lock.specs = append(lock.specs, *current)
			current = nil
		}
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}

		switch line {
		case "GIT", "PATH", "GEM", "PLUGIN SOURCE":
			flush()
			section = "source"
			continue
		case "PLATFORMS":
			flush()
			section = "platforms"
			continue
		case "DEPENDENCIES":
			flush()
			section = "dependencies"
			continue
		case "CHECKSUMS":
			flush()
			section = "checksums"
			continue
		case "BUNDLED WITH", "RUBY VERSION":
			flush()
			section = "meta"
			continue
		}

		switch section {
		case "source":
			if line == "  specs:" {
				continue
			}
			m := bundlerNameVersionRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			indent, name, version := m[1], m[2], m[3]
			switch len(indent) {
			case 4:
				flush()
				if name == "" || version == "" {
					continue
				}
				current = &bundlerSpec{name: name, version: version}
			case 6:
				if current == nil || name == "" {
					continue
				}
				current.deps = append(current.deps, bundlerDep{
					name:       name,
					constraint: strings.TrimSpace(version),
				})
			}
		case "dependencies":
			m := bundlerNameVersionRe.FindStringSubmatch(line)
			if m == nil || len(m[1]) != 2 {
				continue
			}
			name := m[2]
			if name == "" {
				continue
			}
			constraint := strings.TrimSpace(m[3])
			opaque := m[5] == "!"
			lock.direct = append(lock.direct, bundlerDirect{
				name:       name,
				constraint: constraint,
				opaque:     opaque,
			})
		}
	}
	flush()

	// 同一 gem のプラットフォーム別エントリを名前で畳む (バージョンは同一想定)
	merged := map[string]*bundlerSpec{}
	var order []string
	for i := range lock.specs {
		s := &lock.specs[i]
		key := normalizeGem(s.name)
		if existing, ok := merged[key]; ok {
			if len(existing.deps) == 0 && len(s.deps) > 0 {
				existing.deps = s.deps
			}
			continue
		}
		cp := *s
		merged[key] = &cp
		order = append(order, key)
	}
	specs := make([]bundlerSpec, 0, len(order))
	for _, key := range order {
		specs = append(specs, *merged[key])
	}
	lock.specs = specs
	return lock
}

func buildBundlerGraph(lockPath, root string) (*DependencyGraph, error) {
	manifestDir := relPath(root, filepath.Dir(lockPath))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("rubygems", relPath(root, lockPath), manifestDir)

	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	lock := parseBundlerLock(string(data))

	for _, spec := range lock.specs {
		nodeID := normalizeGem(spec.name)
		graph.addNode(nodeID, spec.name, spec.version, nodeID)
	}

	for _, spec := range lock.specs {
		parent := normalizeGem(spec.name)
		for _, dep := range spec.deps {
			child := normalizeGem(dep.name)
			if _, ok := graph.Names[child]; !ok {
				continue
			}
			graph.addEdge(child, Edge{parent, dep.constraint, "", "runtime", false})
		}
	}

	manifest := relPath(root, filepath.Join(filepath.Dir(lockPath), "Gemfile"))
	if info, err := os.Stat(filepath.Join(filepath.Dir(lockPath), "Gemfile")); err != nil || info.IsDir() {
		if _, err := os.Stat(filepath.Join(filepath.Dir(lockPath), "gems.rb")); err == nil {
			manifest = relPath(root, filepath.Join(filepath.Dir(lockPath), "gems.rb"))
		}
	}
	for _, d := range lock.direct {
		nodeID := normalizeGem(d.name)
		if _, ok := graph.Names[nodeID]; !ok {
			continue
		}
		constraint := d.constraint
		opaque := d.opaque
		if opaque && constraint == "" {
			constraint = "git"
			opaque = true
		}
		graph.Direct[nodeID] = DirectDep{
			Constraint: constraint,
			Group:      "main",
			Manifest:   manifest,
			Opaque:     opaque,
		}
	}
	return graph, nil
}
