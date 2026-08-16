// Package cache provides a small on-disk JSON cache under ~/.cache/dependabot-tool/.
//
// Callers pass a namespace (e.g. "alerts", "history") and an arbitrary key
// string describing the cached request (repo, filters, commit hash, date,
// ...). The key is hashed internally, so callers don't need to worry about
// filesystem-safe characters or collisions.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// BaseDir returns ~/.cache/dependabot-tool, creating no directories.
func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "dependabot-tool"), nil
}

func path(namespace, key string) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(base, namespace, name), nil
}

// Load reads the cached value for (namespace, key) into v.
// found is false (with a nil error) when there is no cache entry yet.
func Load(namespace, key string, v any) (found bool, err error) {
	p, err := path(namespace, key)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}

// Save writes v as the cached value for (namespace, key), creating parent
// directories as needed.
func Save(namespace, key string, v any) error {
	p, err := path(namespace, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
