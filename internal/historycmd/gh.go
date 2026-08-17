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

// runGhAPI runs `gh api <args...>` and returns trimmed stdout.
func runGhAPI(args ...string) (string, error) {
	cmd := exec.Command("gh", append([]string{"api"}, args...)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh api %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// ghAPI runs `gh api --paginate <endpoint>` and decodes the (possibly
// multi-page) JSON output into a slice of T. Each page can be either a JSON
// array or a single JSON object, mirroring gh's --paginate behaviour; this
// mirrors the Python gh_api() helper which concatenates and decodes pages
// one JSON value at a time.
func ghAPI[T any](endpoint string) ([]T, error) {
	output, err := runGhAPI("--paginate", endpoint)
	if err != nil {
		return nil, err
	}

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

// ghAPIPage runs `gh api <endpoint>` for a single page (appending
// `page=<page>` to the query string) and decodes the JSON array response
// into a slice of T. Unlike ghAPI, it does not follow pagination, so callers
// can inspect a page's contents and decide whether to fetch the next one.
func ghAPIPage[T any](endpoint string, page int) ([]T, error) {
	sep := "&"
	if !strings.Contains(endpoint, "?") {
		sep = "?"
	}
	pagedEndpoint := fmt.Sprintf("%s%spage=%d", endpoint, sep, page)

	output, err := runGhAPI(pagedEndpoint)
	if err != nil {
		return nil, err
	}

	if output == "" {
		return nil, nil
	}

	var values []T
	if err := json.Unmarshal([]byte(output), &values); err != nil {
		return nil, fmt.Errorf("gh api %s: decoding page: %w", pagedEndpoint, err)
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
