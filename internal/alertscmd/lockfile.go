package alertscmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --------------------------------------------------------------------------
// ローカルのロックファイルからバージョンを解決
// --------------------------------------------------------------------------

var pypiNormalizeRe = regexp.MustCompile(`[-_.]+`)

// normalizePypi implements PEP 503 normalization.
func normalizePypi(name string) string {
	return strings.ToLower(pypiNormalizeRe.ReplaceAllString(name, "-"))
}

// LocalVersions resolves the actual local version of a package from
// manifest_path and package name.
type LocalVersions struct {
	root    string
	poetry  map[string]map[string]string   // lock path -> normalized name -> version
	bun     map[string]map[string][]string // lock path -> name -> versions
	bundler map[string]map[string]string   // lock path -> normalized name -> version
	js      map[string]map[string][]string // lock path -> name -> versions
}

func newLocalVersions(root string) *LocalVersions {
	return &LocalVersions{
		root:    root,
		poetry:  map[string]map[string]string{},
		bun:     map[string]map[string][]string{},
		bundler: map[string]map[string]string{},
		js:      map[string]map[string][]string{},
	}
}

func (l *LocalVersions) loadPoetry(lock string) map[string]string {
	if t, ok := l.poetry[lock]; ok {
		return t
	}
	result := map[string]string{}
	if info, err := os.Stat(lock); err == nil && !info.IsDir() {
		if data, err := os.ReadFile(lock); err == nil {
			if doc, err := parseTOML(string(data)); err == nil {
				for _, pkgRaw := range tomlGetArray(doc, "package") {
					pkg, ok := pkgRaw.(map[string]interface{})
					if !ok {
						continue
					}
					name := tomlGetString(pkg, "name")
					version := tomlGetString(pkg, "version")
					if name != "" && version != "" {
						result[normalizePypi(name)] = version
					}
				}
			}
		}
	}
	l.poetry[lock] = result
	return result
}

func (l *LocalVersions) loadBun(lock string) map[string][]string {
	if t, ok := l.bun[lock]; ok {
		return t
	}
	result := map[string][]string{}
	if info, err := os.Stat(lock); err == nil && !info.IsDir() {
		if data, err := os.ReadFile(lock); err == nil {
			if doc, err := loadsJSONC(string(data)); err == nil {
				packages, _ := doc["packages"].(map[string]interface{})
				for _, entryRaw := range packages {
					entry, ok := entryRaw.([]interface{})
					if !ok || len(entry) == 0 {
						continue
					}
					descriptor, ok := entry[0].(string)
					if !ok {
						continue
					}
					name, version := splitNPMDescriptor(descriptor)
					if name == "" || version == "" {
						continue
					}
					versions := result[name]
					found := false
					for _, v := range versions {
						if v == version {
							found = true
							break
						}
					}
					if !found {
						result[name] = append(versions, version)
					}
				}
			}
		}
	}
	l.bun[lock] = result
	return result
}

// resolve returns (versions, source) for the given ecosystem/manifest/name.
func (l *LocalVersions) resolve(ecosystem, manifestPath, name string) ([]string, string) {
	manifestDir := filepath.Dir(filepath.Join(l.root, manifestPath))

	switch ecosystem {
	case "pip":
		candidates := []string{
			filepath.Join(manifestDir, "poetry.lock"),
			filepath.Join(l.root, "poetry.lock"),
		}
		for _, lock := range candidates {
			table := l.loadPoetry(lock)
			if len(table) == 0 {
				continue
			}
			if version, ok := table[normalizePypi(name)]; ok && version != "" {
				rel, _ := filepath.Rel(l.root, lock)
				return []string{version}, rel
			}
		}
		return nil, "not-found"

	case "npm":
		candidates := []string{
			filepath.Join(manifestDir, "package-lock.json"),
			filepath.Join(manifestDir, "npm-shrinkwrap.json"),
			filepath.Join(manifestDir, "pnpm-lock.yaml"),
			filepath.Join(manifestDir, "yarn.lock"),
			filepath.Join(manifestDir, "bun.lock"),
		}
		for _, lock := range candidates {
			versions := l.loadJSLock(lock)[name]
			if len(versions) == 0 {
				continue
			}
			rel, _ := filepath.Rel(l.root, lock)
			return sortedJSVersions(versions), rel
		}
		return nil, "not-found"

	case "rubygems", "bundler":
		candidates := []string{
			filepath.Join(manifestDir, "Gemfile.lock"),
			filepath.Join(manifestDir, "gems.locked"),
			filepath.Join(l.root, "Gemfile.lock"),
			filepath.Join(l.root, "gems.locked"),
		}
		// manifest_path が Gemfile.lock 自身のときも同じディレクトリを見る
		seen := map[string]bool{}
		for _, lock := range candidates {
			if seen[lock] {
				continue
			}
			seen[lock] = true
			table := l.loadBundler(lock)
			if len(table) == 0 {
				continue
			}
			if version, ok := table[normalizeGem(name)]; ok && version != "" {
				rel, _ := filepath.Rel(l.root, lock)
				return []string{version}, rel
			}
		}
		return nil, "not-found"

	default:
		return nil, "unsupported:" + ecosystem
	}
}

func (l *LocalVersions) loadJSLock(lock string) map[string][]string {
	if cached, ok := l.js[lock]; ok {
		return cached
	}
	var versions map[string][]string
	switch filepath.Base(lock) {
	case "package-lock.json", "npm-shrinkwrap.json":
		versions = loadPackageLock(lock)
	case "pnpm-lock.yaml":
		versions = loadPnpmLock(lock)
	case "yarn.lock":
		versions = loadYarnClassicLock(lock)
	case "bun.lock":
		versions = l.loadBun(lock)
	default:
		versions = map[string][]string{}
	}
	l.js[lock] = versions
	return versions
}

func (l *LocalVersions) loadBundler(lock string) map[string]string {
	if t, ok := l.bundler[lock]; ok {
		return t
	}
	result := map[string]string{}
	if info, err := os.Stat(lock); err == nil && !info.IsDir() {
		if data, err := os.ReadFile(lock); err == nil {
			parsed := parseBundlerLock(string(data))
			for _, spec := range parsed.specs {
				if spec.name == "" || spec.version == "" {
					continue
				}
				result[normalizeGem(spec.name)] = spec.version
			}
		}
	}
	l.bundler[lock] = result
	return result
}

// --------------------------------------------------------------------------
// npm パッケージ記述子の分解
// --------------------------------------------------------------------------

// splitNPMDescriptor splits '@scope/name@1.2.3' into ('@scope/name', '1.2.3').
func splitNPMDescriptor(descriptor string) (string, string) {
	if alias := strings.Index(descriptor, "@npm:"); alias > 0 {
		head, tail := descriptor[:alias], descriptor[alias+5:]
		at := strings.LastIndex(tail, "@")
		if at > 0 {
			return head, tail[at+1:]
		}
		return head, ""
	}
	at := strings.LastIndex(descriptor, "@")
	if at <= 0 {
		return descriptor, ""
	}
	return descriptor[:at], descriptor[at+1:]
}

// splitNPMKey splits bun.lock's nested key 'a/@scope/b' into a sequence of
// package names.
func splitNPMKey(key string) []string {
	parts := strings.Split(key, "/")
	var out []string
	i := 0
	for i < len(parts) {
		if strings.HasPrefix(parts[i], "@") && i+1 < len(parts) {
			out = append(out, parts[i]+"/"+parts[i+1])
			i += 2
		} else {
			out = append(out, parts[i])
			i++
		}
	}
	return out
}

// resolveNPMKey resolves the node_modules-style lookup (nearest nesting
// outward) for a dependency's key.
func resolveNPMKey(packages map[string]interface{}, fromKey, dep string) (string, bool) {
	var chain []string
	if fromKey != "" {
		chain = splitNPMKey(fromKey)
	}
	for i := len(chain); i >= 0; i-- {
		segs := append(append([]string(nil), chain[:i]...), dep)
		candidate := strings.Join(segs, "/")
		if _, ok := packages[candidate]; ok {
			return candidate, true
		}
	}
	return "", false
}
