package alertscmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type yarnSpec struct {
	key     string
	name    string
	version string
	deps    []bundlerDep
}

// yarnDescriptorName returns the package name from a Yarn v1 descriptor such
// as "@scope/pkg@^1.0.0" or "pkg@npm:other@^1.0.0".
func yarnDescriptorName(descriptor string) string {
	descriptor = strings.Trim(strings.TrimSpace(descriptor), `"`)
	if strings.HasPrefix(descriptor, "@") {
		if slash := strings.Index(descriptor, "/"); slash >= 0 {
			if at := strings.Index(descriptor[slash:], "@"); at >= 0 {
				return descriptor[:slash+at]
			}
		}
		return descriptor
	}
	if at := strings.Index(descriptor, "@"); at >= 0 {
		return descriptor[:at]
	}
	return descriptor
}

func yarnDependency(line string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return "", "", false
	}
	name := strings.Trim(fields[0], `"`)
	constraint := strings.Trim(strings.Join(fields[1:], " "), `"`)
	return name, constraint, name != ""
}

func parseYarnClassicLock(content string) []yarnSpec {
	var specs []yarnSpec
	var current *yarnSpec
	inDeps := false
	flush := func() {
		if current != nil && current.name != "" && current.version != "" {
			specs = append(specs, *current)
		}
		current = nil
		inDeps = false
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			flush()
			key := strings.TrimSuffix(line, ":")
			first := strings.TrimSpace(strings.Split(key, ",")[0])
			current = &yarnSpec{key: key, name: yarnDescriptorName(first)}
			continue
		}
		if current == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "version ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "version "))
			current.version = strings.Trim(value, `"`)
			inDeps = false
			continue
		}
		if trimmed == "dependencies:" || trimmed == "optionalDependencies:" {
			inDeps = true
			continue
		}
		if inDeps && strings.HasPrefix(line, "    ") {
			if name, constraint, ok := yarnDependency(line); ok {
				current.deps = append(current.deps, bundlerDep{name: name, constraint: constraint})
			}
			continue
		}
		if strings.HasPrefix(line, "  ") {
			inDeps = false
		}
	}
	flush()
	return specs
}

func buildYarnClassicGraph(lock, root string) (*DependencyGraph, error) {
	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(data), "__metadata:") {
		return buildYarnBerryGraph(lock, root, data)
	}
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("npm", relPath(root, lock), manifestDir)
	specs := parseYarnClassicLock(string(data))
	for _, spec := range specs {
		graph.addNode(spec.key, spec.name, spec.version, spec.name)
	}
	resolve := func(name string) (string, bool) {
		nodes := graph.lookup(name)
		if len(nodes) == 0 {
			return "", false
		}
		return nodes[0], true
	}
	for _, spec := range specs {
		for _, dep := range spec.deps {
			if child, ok := resolve(dep.name); ok {
				graph.addEdge(child, Edge{spec.key, dep.constraint, "", "runtime", false})
			}
		}
	}
	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	manifestData, _ := os.ReadFile(filepath.Join(filepath.Dir(lock), "package.json"))
	var doc map[string]interface{}
	_ = json.Unmarshal(manifestData, &doc)
	for _, df := range jsDepFields {
		for name, constraint := range jsMap(doc[df.field]) {
			if child, ok := resolve(name); ok {
				graph.Direct[child] = DirectDep{Constraint: toStringValue(constraint), Group: df.kind, Manifest: manifest}
			}
		}
	}
	return graph, nil
}

func loadYarnClassicLock(lock string) map[string][]string {
	result := map[string][]string{}
	data, err := os.ReadFile(lock)
	if err != nil {
		return result
	}
	if strings.Contains(string(data), "__metadata:") {
		var doc map[string]interface{}
		if yaml.Unmarshal(data, &doc) != nil {
			return result
		}
		for key, raw := range doc {
			if key == "__metadata" {
				continue
			}
			entry := jsMap(raw)
			addUniqueVersion(result, yarnDescriptorName(strings.Split(key, ",")[0]), toStringValue(entry["version"]))
		}
		return result
	}
	for _, spec := range parseYarnClassicLock(string(data)) {
		addUniqueVersion(result, spec.name, spec.version)
	}
	return result
}

// Yarn Berry lockfiles (v2+) are YAML.  Their top-level descriptor entries
// contain version and dependencies fields analogous to the Classic format.
func buildYarnBerryGraph(lock, root string, data []byte) (*DependencyGraph, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	manifestDir := relPath(root, filepath.Dir(lock))
	if manifestDir == "" {
		manifestDir = "."
	}
	graph := newDependencyGraph("npm", relPath(root, lock), manifestDir)
	for key, raw := range doc {
		if key == "__metadata" {
			continue
		}
		entry := jsMap(raw)
		name, version := yarnDescriptorName(strings.Split(key, ",")[0]), toStringValue(entry["version"])
		if name != "" && version != "" {
			graph.addNode(key, name, version, name)
		}
	}
	resolve := func(name string) (string, bool) {
		nodes := graph.lookup(name)
		if len(nodes) == 0 {
			return "", false
		}
		return nodes[0], true
	}
	for key, raw := range doc {
		if _, ok := graph.Names[key]; !ok {
			continue
		}
		entry := jsMap(raw)
		for name, constraint := range jsMap(entry["dependencies"]) {
			if child, ok := resolve(name); ok {
				graph.addEdge(child, Edge{key, toStringValue(constraint), "", "runtime", false})
			}
		}
	}
	manifest := relPath(root, filepath.Join(filepath.Dir(lock), "package.json"))
	manifestData, _ := os.ReadFile(filepath.Join(filepath.Dir(lock), "package.json"))
	var packageJSON map[string]interface{}
	_ = json.Unmarshal(manifestData, &packageJSON)
	for _, df := range jsDepFields {
		for name, constraint := range jsMap(packageJSON[df.field]) {
			if child, ok := resolve(name); ok {
				graph.Direct[child] = DirectDep{Constraint: toStringValue(constraint), Group: df.kind, Manifest: manifest}
			}
		}
	}
	return graph, nil
}
