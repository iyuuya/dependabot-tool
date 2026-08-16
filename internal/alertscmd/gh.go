package alertscmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// --------------------------------------------------------------------------
// アラート取得
// --------------------------------------------------------------------------

func detectRepo() (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("リポジトリを特定できません: %s", strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func fetchAlerts(repo string, params map[string]string) ([]map[string]interface{}, error) {
	args := []string{"api", "--paginate", "-X", "GET", "/repos/" + repo + "/dependabot/alerts", "-f", "per_page=100"}
	for key, value := range params {
		args = append(args, "-f", key+"="+value)
	}
	cmd := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api に失敗しました:\n%s", strings.TrimSpace(stderr.String()))
	}
	return decodeAlerts(stdout.String())
}

// decodeAlerts decodes gh api --paginate output, which may be a single JSON
// array, or several JSON array literals concatenated back-to-back (one per
// page).
func decodeAlerts(text string) ([]map[string]interface{}, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}

	var single []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &single); err == nil {
		return single, nil
	}

	var alerts []map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	for dec.More() {
		var chunk []map[string]interface{}
		if err := dec.Decode(&chunk); err != nil {
			return nil, err
		}
		alerts = append(alerts, chunk...)
	}
	return alerts, nil
}
