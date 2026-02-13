package sender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/josepnunes/tempo-cli/internal/config"
	"github.com/josepnunes/tempo-cli/internal/detector"
)

// SavePending atomically writes an attribution to .tempo/pending/.
func SavePending(repoRoot string, attr *detector.Attribution) error {
	dir := filepath.Join(repoRoot, ".tempo", "pending")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(attr, "", "  ")
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%d.json", time.Now().UnixMilli())
	tmpPath := filepath.Join(dir, ".tmp-"+filename)
	finalPath := filepath.Join(dir, filename)

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

// PendingCount returns the number of pending attribution files.
func PendingCount(repoRoot string) int {
	dir := filepath.Join(repoRoot, ".tempo", "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".tmp-") {
			count++
		}
	}
	return count
}

// Sync reads all pending attributions and sends them to the API.
// On success, deletes the sent files. On failure, keeps them.
// If no API token, silently returns nil (offline mode).
func Sync(repoRoot string, version string) error {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	if cfg.APIToken == "" {
		return nil
	}

	dir := filepath.Join(repoRoot, ".tempo", "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var attributions []*detector.Attribution
	var filePaths []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var attr detector.Attribution
		if err := json.Unmarshal(data, &attr); err != nil {
			continue
		}
		attributions = append(attributions, &attr)
		filePaths = append(filePaths, path)
	}

	if len(attributions) == 0 {
		return nil
	}

	payload := map[string]any{
		"attributions": attributions,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", cfg.Endpoint+"/v1/attributions", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("User-Agent", "tempo-cli/"+version)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tempo-cli: warning: API unreachable, keeping pending files\n")
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		for _, p := range filePaths {
			os.Remove(p)
		}
	} else {
		fmt.Fprintf(os.Stderr, "tempo-cli: warning: API returned %d, keeping pending files\n", resp.StatusCode)
	}
	return nil
}
