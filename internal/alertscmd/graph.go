package alertscmd

import (
	"container/list"
	"os"
	"path/filepath"
)

// --------------------------------------------------------------------------
// 依存グラフ (逆依存を辿って「実際に上げるべき対象」を出すため)
// --------------------------------------------------------------------------

// Edge is a parent -> child dependency edge (used in reverse: looked up by child).
type Edge struct {
	Parent      string // ノードID
	Constraint  string
	Marker      string
	Kind        string // runtime / dev / peer / optional
	Conditional bool   // マーカーを評価しきれなかった
}

// DirectDep represents a direct (manifest-declared) dependency.
type DirectDep struct {
	Constraint string
	Group      string // main / dev / test / dependencies / devDependencies ...
	Manifest   string // 表示用の相対パス
	Opaque     bool   // git 依存など、バージョン指定でないもの
}

// DependencyGraph is a reverse dependency graph built from a lock file.
type DependencyGraph struct {
	Ecosystem   string
	LockPath    string
	ManifestDir string
	Names       map[string]string
	Versions    map[string]string
	Parents     map[string][]Edge
	Direct      map[string]DirectDep
	ByName      map[string][]string
}

func newDependencyGraph(ecosystem, lockPath, manifestDir string) *DependencyGraph {
	return &DependencyGraph{
		Ecosystem:   ecosystem,
		LockPath:    lockPath,
		ManifestDir: manifestDir,
		Names:       map[string]string{},
		Versions:    map[string]string{},
		Parents:     map[string][]Edge{},
		Direct:      map[string]DirectDep{},
		ByName:      map[string][]string{},
	}
}

func (g *DependencyGraph) addNode(nodeID, name, version, indexKey string) {
	g.Names[nodeID] = name
	g.Versions[nodeID] = version
	g.ByName[indexKey] = append(g.ByName[indexKey], nodeID)
}

func (g *DependencyGraph) addEdge(child string, edge Edge) {
	g.Parents[child] = append(g.Parents[child], edge)
}

func (g *DependencyGraph) label(nodeID string) string {
	version := g.Versions[nodeID]
	name, ok := g.Names[nodeID]
	if !ok {
		name = nodeID
	}
	if version != "" {
		return name + " " + version
	}
	return name
}

func (g *DependencyGraph) lookup(name string) []string {
	key := name
	switch g.Ecosystem {
	case "pip":
		key = normalizePypi(name)
	case "rubygems", "bundler":
		key = normalizeGem(name)
	}
	return g.ByName[key]
}

// entrypoints walks up the graph to find reachable direct dependency node IDs.
func (g *DependencyGraph) entrypoints(nodeIDs []string) []string {
	var found []string
	foundSet := map[string]bool{}
	seen := map[string]bool{}
	queue := list.New()
	for _, id := range nodeIDs {
		seen[id] = true
		queue.PushBack(id)
	}
	for queue.Len() > 0 {
		front := queue.Front()
		queue.Remove(front)
		current := front.Value.(string)
		if _, ok := g.Direct[current]; ok && !foundSet[current] {
			found = append(found, current)
			foundSet[current] = true
		}
		for _, edge := range g.Parents[current] {
			if !seen[edge.Parent] {
				seen[edge.Parent] = true
				queue.PushBack(edge.Parent)
			}
		}
	}
	return found
}

// depSpec is a (constraint, marker) pair.
type depSpec struct {
	constraint string
	marker     string
}

// pipDepSpecs normalizes the value of a poetry.lock dependencies entry into
// a list of (constraint, marker) pairs.
func pipDepSpecs(value interface{}) []depSpec {
	switch v := value.(type) {
	case string:
		return []depSpec{{v, ""}}
	case map[string]interface{}:
		version := tomlGetString(v, "version")
		if version == "" {
			version = "*"
		}
		return []depSpec{{version, tomlGetString(v, "markers")}}
	case []interface{}:
		var specs []depSpec
		for _, item := range v {
			switch iv := item.(type) {
			case map[string]interface{}:
				version := tomlGetString(iv, "version")
				if version == "" {
					version = "*"
				}
				specs = append(specs, depSpec{version, tomlGetString(iv, "markers")})
			case string:
				specs = append(specs, depSpec{iv, ""})
			}
		}
		return specs
	}
	return nil
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func buildPipGraph(lock, root, pythonVersion string) (*DependencyGraph, error) {
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("pip", relPath(root, lock), manifestDir)

	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	doc, err := parseTOML(string(data))
	if err != nil {
		return nil, err
	}

	packages := tomlGetArray(doc, "package")
	for _, pkgRaw := range packages {
		pkg, ok := pkgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		name := tomlGetString(pkg, "name")
		if name == "" {
			continue
		}
		nodeID := normalizePypi(name)
		graph.addNode(nodeID, name, tomlGetString(pkg, "version"), nodeID)
	}

	for _, pkgRaw := range packages {
		pkg, ok := pkgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		parent := normalizePypi(tomlGetString(pkg, "name"))
		if parent == "" {
			continue
		}
		deps := tomlGetMap(pkg, "dependencies")
		for depName, value := range deps {
			child := normalizePypi(depName)
			if _, ok := graph.Names[child]; !ok {
				continue
			}
			for _, spec := range pipDepSpecs(value) {
				applies := evaluateMarker(spec.marker, pythonVersion)
				if applies != nil && !*applies {
					continue
				}
				graph.addEdge(child, Edge{parent, spec.constraint, spec.marker, "runtime", applies == nil})
			}
		}
	}

	pyproject := filepath.Join(filepath.Dir(lock), "pyproject.toml")
	if info, err := os.Stat(pyproject); err == nil && !info.IsDir() {
		manifest := relPath(root, pyproject)
		data, err := os.ReadFile(pyproject)
		if err == nil {
			config, err := parseTOML(string(data))
			if err == nil {
				tool := tomlGetMap(config, "tool")
				poetry := tomlGetMap(tool, "poetry")

				type namedGroup struct {
					name string
					deps map[string]interface{}
				}
				var groups []namedGroup
				groups = append(groups, namedGroup{"main", tomlGetMap(poetry, "dependencies")})
				groups = append(groups, namedGroup{"dev", tomlGetMap(poetry, "dev-dependencies")})
				groupTable := tomlGetMap(poetry, "group")
				for groupName, groupRaw := range groupTable {
					groupMap, ok := groupRaw.(map[string]interface{})
					if !ok {
						continue
					}
					groups = append(groups, namedGroup{groupName, tomlGetMap(groupMap, "dependencies")})
				}
				for _, group := range groups {
					for depName, value := range group.deps {
						if normalizePypi(depName) == "python" {
							continue
						}
						nodeID := normalizePypi(depName)
						if _, ok := graph.Names[nodeID]; !ok {
							continue
						}
						switch v := value.(type) {
						case string:
							graph.Direct[nodeID] = DirectDep{Constraint: v, Group: group.name, Manifest: manifest}
						case map[string]interface{}:
							if version := tomlGetString(v, "version"); version != "" {
								graph.Direct[nodeID] = DirectDep{Constraint: version, Group: group.name, Manifest: manifest}
							} else {
								source := "path"
								if git, ok := v["git"]; ok && git != nil && git != "" {
									source = "git"
								}
								graph.Direct[nodeID] = DirectDep{Constraint: source, Group: group.name, Manifest: manifest, Opaque: true}
							}
						}
					}
				}
			}
		}
	}
	return graph, nil
}

func buildNpmGraph(lock, root string) (*DependencyGraph, error) {
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("npm", relPath(root, lock), manifestDir)

	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	doc, err := loadsJSONC(string(data))
	if err != nil {
		return nil, err
	}
	packages, _ := doc["packages"].(map[string]interface{})
	if packages == nil {
		packages = map[string]interface{}{}
	}

	for key, entryRaw := range packages {
		entry, ok := entryRaw.([]interface{})
		if !ok || len(entry) == 0 {
			continue
		}
		descriptor, ok := entry[0].(string)
		if !ok {
			continue
		}
		name, version := splitNPMDescriptor(descriptor)
		graph.addNode(key, name, version, name)
	}

	depFields := []struct {
		field string
		kind  string
	}{
		{"dependencies", "runtime"},
		{"optionalDependencies", "optional"},
		{"peerDependencies", "peer"},
		{"devDependencies", "dev"},
	}

	for key, entryRaw := range packages {
		if _, ok := graph.Names[key]; !ok {
			continue
		}
		entry := entryRaw.([]interface{})
		var meta map[string]interface{}
		if len(entry) > 2 {
			meta, _ = entry[2].(map[string]interface{})
		}
		for _, df := range depFields {
			depsRaw, _ := meta[df.field].(map[string]interface{})
			for depName, constraint := range depsRaw {
				child, ok := resolveNPMKey(packages, key, depName)
				if !ok {
					continue
				}
				graph.addEdge(child, Edge{key, toStringValue(constraint), "", df.kind, false})
			}
		}
	}

	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	workspaces, _ := doc["workspaces"].(map[string]interface{})
	for wsPath, wsRaw := range workspaces {
		ws, ok := wsRaw.(map[string]interface{})
		if !ok {
			continue
		}
		for _, df := range depFields {
			depsRaw, _ := ws[df.field].(map[string]interface{})
			for depName, constraint := range depsRaw {
				child, ok := resolveNPMKey(packages, wsPath, depName)
				if !ok {
					continue
				}
				group := df.kind
				if wsPath != "" {
					group = df.kind + "@" + wsPath
				}
				graph.Direct[child] = DirectDep{Constraint: toStringValue(constraint), Group: group, Manifest: manifest}
			}
		}
	}
	return graph, nil
}

func toStringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GraphCache builds a DependencyGraph for each lock file only once.
type GraphCache struct {
	root          string
	pythonVersion string
	cache         map[string]*DependencyGraph
}

func newGraphCache(root, pythonVersion string) *GraphCache {
	return &GraphCache{root: root, pythonVersion: pythonVersion, cache: map[string]*DependencyGraph{}}
}

func (c *GraphCache) get(ecosystem, source string) *DependencyGraph {
	if g, ok := c.cache[source]; ok {
		return g
	}
	var graph *DependencyGraph
	path := filepath.Join(c.root, source)
	base := filepath.Base(path)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		if ecosystem == "pip" && base == "poetry.lock" {
			if g, err := buildPipGraph(path, c.root, c.pythonVersion); err == nil {
				graph = g
			}
		} else if ecosystem == "npm" && base == "bun.lock" {
			if g, err := buildNpmGraph(path, c.root); err == nil {
				graph = g
			}
		} else if ecosystem == "npm" && (base == "package-lock.json" || base == "npm-shrinkwrap.json") {
			if g, err := buildPackageLockGraph(path, c.root); err == nil {
				graph = g
			}
		} else if ecosystem == "npm" && base == "pnpm-lock.yaml" {
			if g, err := buildPnpmGraph(path, c.root); err == nil {
				graph = g
			}
		} else if ecosystem == "npm" && base == "yarn.lock" {
			if g, err := buildYarnClassicGraph(path, c.root); err == nil {
				graph = g
			}
		} else if (ecosystem == "rubygems" || ecosystem == "bundler") && (base == "Gemfile.lock" || base == "gems.locked") {
			if g, err := buildBundlerGraph(path, c.root); err == nil {
				graph = g
			}
		}
	}
	c.cache[source] = graph
	return graph
}
