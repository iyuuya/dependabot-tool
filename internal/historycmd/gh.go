// Package historycmd implements the "history" subcommand, which reports how
// the number of Dependabot alerts changed around recently merged Dependabot
// pull requests. It is a Go port of dependabot_history.py.
package historycmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ghAPI runs `gh api --paginate <endpoint>` and decodes the (possibly
// multi-page) JSON output into a slice of T. Each page can be either a JSON
// array or a single JSON object, mirroring gh's --paginate behaviour; this
// mirrors the Python gh_api() helper which concatenates and decodes pages
// one JSON value at a time.
func ghAPI[T any](endpoint string) ([]T, error) {
	cmd := exec.Command("gh", "api", "--paginate", endpoint)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", endpoint, err, strings.TrimSpace(stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return nil, nil
	}

	var values []T

	dec := json.NewDecoder(strings.NewReader(output))
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("gh api %s: decoding response: %w", endpoint, err)
		}

		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var page []T
			if err := json.Unmarshal(raw, &page); err != nil {
				return nil, fmt.Errorf("gh api %s: decoding page: %w", endpoint, err)
			}
			values = append(values, page...)
			continue
		}

		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("gh api %s: decoding value: %w", endpoint, err)
		}
		values = append(values, value)
	}

	return values, nil
}

// getRepo determines the current repository via `gh repo view`, mirroring
// the Python get_repo() helper.
func getRepo() (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh repo view: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
