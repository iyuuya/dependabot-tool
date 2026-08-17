package alertscmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// jsDepFields are the package.json fields whose edges are recorded in lockfiles.
var jsDepFields = []struct{ field, kind string }{
	{"dependencies", "runtime"},
	{"optionalDependencies", "optional"},
	{"peerDependencies", "peer"},
	{"devDependencies", "dev"},
}

func jsMap(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	return m
}

func addUniqueVersion(result map[string][]string, name, version string) {
	if name == "" || version == "" {
		return
	}
	for _, existing := range result[name] {
		if existing == version {
			return
		}
	}
	result[name] = append(result[name], version)
}

func sortedJSVersions(versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool { return bunVersionSortLess(sorted[i], sorted[j]) })
	return sorted
}

// npmPackageName derives the package name from a package-lock package path.
func npmPackageName(key string) string {
	if key == "" {
		return ""
	}
	const marker = "node_modules/"
	i := strings.LastIndex(key, marker)
	if i < 0 {
		return ""
	}
	return key[i+len(marker):]
}

func resolveNpmPackagePath(packages map[string]interface{}, fromPath, dep string) (string, bool) {
	path := fromPath
	for {
		candidate := filepath.ToSlash(filepath.Join(path, "node_modules", dep))
		if _, ok := packages[candidate]; ok {
			return candidate, true
		}
		if path == "" {
			break
		}
		next := filepath.ToSlash(filepath.Dir(path))
		if next == "." {
			next = ""
		}
		// Package paths always move up one node_modules level.
		if i := strings.LastIndex(next, "/node_modules/"); i >= 0 {
			path = next[:i]
		} else {
			path = ""
		}
	}
	return "", false
}

func buildPackageLockGraph(lock, root string) (*DependencyGraph, error) {
	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("npm", relPath(root, lock), manifestDir)
	packages := jsMap(doc["packages"])
	if len(packages) == 0 {
		return buildPackageLockV1Graph(doc, graph, root, lock)
	}

	for key, raw := range packages {
		entry := jsMap(raw)
		name, version := npmPackageName(key), toStringValue(entry["version"])
		if name != "" && version != "" {
			graph.addNode(key, name, version, name)
		}
	}
	for key, raw := range packages {
		if _, ok := graph.Names[key]; !ok {
			continue
		}
		entry := jsMap(raw)
		for _, df := range jsDepFields {
			for name, rawConstraint := range jsMap(entry[df.field]) {
				if child, ok := resolveNpmPackagePath(packages, key, name); ok {
					graph.addEdge(child, Edge{key, toStringValue(rawConstraint), "", df.kind, false})
				}
			}
		}
	}
	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	for path, raw := range packages {
		entry := jsMap(raw)
		if path != "" && strings.Contains(path, "node_modules/") {
			continue
		}
		for _, df := range jsDepFields {
			for name, rawConstraint := range jsMap(entry[df.field]) {
				if child, ok := resolveNpmPackagePath(packages, path, name); ok {
					group := df.kind
					if path != "" {
						group += "@" + path
					}
					graph.Direct[child] = DirectDep{Constraint: toStringValue(rawConstraint), Group: group, Manifest: manifest}
				}
			}
		}
	}
	return graph, nil
}

// npm lockfile v1 has a recursive dependencies object instead of packages.
func buildPackageLockV1Graph(doc map[string]interface{}, graph *DependencyGraph, root, lock string) (*DependencyGraph, error) {
	var add func(map[string]interface{}, string)
	add = func(deps map[string]interface{}, parentPath string) {
		for name, raw := range deps {
			entry := jsMap(raw)
			version := toStringValue(entry["version"])
			key := parentPath + "node_modules/" + name
			if version != "" {
				graph.addNode(key, name, version, name)
			}
			add(jsMap(entry["dependencies"]), key+"/")
			for _, df := range jsDepFields {
				for childName, constraint := range jsMap(entry[df.field]) {
					child := key + "/node_modules/" + childName
					if _, ok := graph.Names[child]; !ok {
						child = "node_modules/" + childName
					}
					if _, ok := graph.Names[child]; ok {
						graph.addEdge(child, Edge{key, toStringValue(constraint), "", df.kind, false})
					}
				}
			}
		}
	}
	deps := jsMap(doc["dependencies"])
	add(deps, "")
	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	for name, raw := range deps {
		entry := jsMap(raw)
		key := "node_modules/" + name
		if _, ok := graph.Names[key]; ok {
			kind := "runtime"
			if dev, _ := entry["dev"].(bool); dev {
				kind = "dev"
			}
			graph.Direct[key] = DirectDep{Constraint: toStringValue(entry["version"]), Group: kind, Manifest: manifest}
		}
	}
	return graph, nil
}

func loadPackageLock(lock string) map[string][]string {
	result := map[string][]string{}
	data, err := os.ReadFile(lock)
	if err != nil {
		return result
	}
	var doc map[string]interface{}
	if json.Unmarshal(data, &doc) != nil {
		return result
	}
	for key, raw := range jsMap(doc["packages"]) {
		entry := jsMap(raw)
		addUniqueVersion(result, npmPackageName(key), toStringValue(entry["version"]))
	}
	if len(result) == 0 {
		var walk func(map[string]interface{})
		walk = func(deps map[string]interface{}) {
			for name, raw := range deps {
				entry := jsMap(raw)
				addUniqueVersion(result, name, toStringValue(entry["version"]))
				walk(jsMap(entry["dependencies"]))
			}
		}
		walk(jsMap(doc["dependencies"]))
	}
	return result
}

func pnpmPackageNameAndVersion(key string) (string, string) {
	key = strings.TrimPrefix(key, "/")
	scoped := strings.HasPrefix(key, "@")
	key = strings.TrimPrefix(key, "@")
	key = strings.Split(key, "(")[0]
	if i := strings.LastIndex(key, "@"); i > 0 {
		name := key[:i]
		if scoped {
			name = "@" + name
		}
		return name, key[i+1:]
	}
	// pnpm lockfile v5/v6 uses /name/version instead of name@version.
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if len(parts) >= 2 {
		version := parts[len(parts)-1]
		nameParts := parts[:len(parts)-1]
		name := strings.Join(nameParts, "/")
		if scoped {
			name = "@" + name
		}
		return name, version
	}
	return "", ""
}

func pnpmResolvedVersion(value interface{}) string {
	v := toStringValue(value)
	v = strings.TrimPrefix(v, "link:")
	v = strings.Split(v, "(")[0]
	return v
}

func buildPnpmGraph(lock, root string) (*DependencyGraph, error) {
	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("npm", relPath(root, lock), manifestDir)
	packages := jsMap(doc["packages"])
	snapshots := jsMap(doc["snapshots"])
	nodes := map[string]string{}
	for key, raw := range packages {
		name, version := pnpmPackageNameAndVersion(key)
		if name == "" || version == "" {
			continue
		}
		nodeID := key
		graph.addNode(nodeID, name, version, name)
		nodes[name+"@"+version] = nodeID
		_ = raw
	}
	lookup := func(name, version string) (string, bool) {
		version = pnpmResolvedVersion(version)
		if id, ok := nodes[name+"@"+version]; ok {
			return id, true
		}
		for id, candidate := range graph.Names {
			if candidate == name && graph.Versions[id] == version {
				return id, true
			}
		}
		return "", false
	}
	edgesFrom := func(parent string, raw map[string]interface{}) {
		for _, df := range jsDepFields {
			for name, version := range jsMap(raw[df.field]) {
				if child, ok := lookup(name, toStringValue(version)); ok {
					graph.addEdge(child, Edge{parent, toStringValue(version), "", df.kind, false})
				}
			}
		}
	}
	for key, raw := range snapshots {
		// snapshot keys match package keys in v9 (possibly with peer suffix).
		parent := key
		if _, ok := graph.Names[parent]; !ok {
			name, version := pnpmPackageNameAndVersion(key)
			parent, _ = lookup(name, version)
		}
		if parent != "" {
			edgesFrom(parent, jsMap(raw))
		}
	}
	if len(snapshots) == 0 {
		for key, raw := range packages {
			edgesFrom(key, jsMap(raw))
		}
	}
	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	for importer, raw := range jsMap(doc["importers"]) {
		for _, df := range jsDepFields {
			for name, value := range jsMap(jsMap(raw)[df.field]) {
				version := toStringValue(value)
				if meta := jsMap(value); meta != nil {
					version = toStringValue(meta["version"])
				}
				if child, ok := lookup(name, version); ok {
					group := df.kind
					if importer != "." {
						group += "@" + importer
					}
					graph.Direct[child] = DirectDep{Constraint: version, Group: group, Manifest: manifest}
				}
			}
		}
	}
	return graph, nil
}

func loadPnpmLock(lock string) map[string][]string {
	result := map[string][]string{}
	data, err := os.ReadFile(lock)
	if err != nil {
		return result
	}
	var doc map[string]interface{}
	if yaml.Unmarshal(data, &doc) != nil {
		return result
	}
	for key := range jsMap(doc["packages"]) {
		name, version := pnpmPackageNameAndVersion(key)
		addUniqueVersion(result, name, version)
	}
	return result
}
