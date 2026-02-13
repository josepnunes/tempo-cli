package detector

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Minimal types for Codex JSONL parsing.
type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	CWD string `json:"cwd"`
}

type codexTurnContext struct {
	Model string `json:"model"`
}

type codexEventPayload struct {
	Type string              `json:"type"`
	Info *codexTokenCountInfo `json:"info,omitempty"`
}

type codexTokenCountInfo struct {
	TotalTokenUsage codexTokenUsage `json:"total_token_usage"`
}

type codexTokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type codexResponseItem struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type codexExecArgs struct {
	Cmd string `json:"cmd"`
}

// Regex patterns for extracting file paths from shell commands.
var fileWritePatterns = []*regexp.Regexp{
	// cat > PATH <<  or  cat > PATH (heredoc/redirect)
	regexp.MustCompile(`cat\s+>\s+(\S+)`),
	// tee PATH
	regexp.MustCompile(`\btee\s+(?:-a\s+)?(\S+)`),
	// touch PATH [PATH...]
	regexp.MustCompile(`\btouch\s+(.+)`),
	// cp SOURCE DEST
	regexp.MustCompile(`\bcp\s+(?:-\w+\s+)*\S+\s+(\S+)`),
	// mv SOURCE DEST
	regexp.MustCompile(`\bmv\s+(?:-\w+\s+)*\S+\s+(\S+)`),
	// sed -i ... PATH (last arg)
	regexp.MustCompile(`\bsed\s+-i[^\s]*\s+(?:'[^']*'|"[^"]*"|\S+)\s+(\S+)`),
}

// extractFilesFromCmd parses a shell command string and returns file paths
// that were likely written to. Returns paths relative to cwd.
func extractFilesFromCmd(cmd string) []string {
	var files []string
	seen := make(map[string]bool)

	for _, re := range fileWritePatterns {
		matches := re.FindAllStringSubmatch(cmd, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			// touch can have multiple space-separated paths
			if strings.Contains(re.String(), `\btouch\s+`) {
				for _, p := range strings.Fields(m[1]) {
					p = cleanPath(p)
					if p != "" && !seen[p] {
						seen[p] = true
						files = append(files, p)
					}
				}
				continue
			}
			p := cleanPath(m[1])
			if p != "" && !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
		}
	}
	return files
}

// cleanPath removes quotes, heredoc markers, and filters out non-file paths.
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	// Skip heredoc markers (<<'EOF', <<EOF)
	if strings.HasPrefix(p, "<<") {
		return ""
	}
	// Skip stdout/stderr redirects
	if p == "/dev/null" || p == "/dev/stdout" || p == "/dev/stderr" {
		return ""
	}
	// Skip flags
	if strings.HasPrefix(p, "-") {
		return ""
	}
	// Skip empty or directory-only paths
	if p == "" || strings.HasSuffix(p, "/") {
		return ""
	}
	return p
}

// findCodexSessions finds all Codex session files for a given repo root.
// Sessions are stored at ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl.
// Only returns sessions modified within maxAge whose cwd matches the repo root.
func findCodexSessions(repoRoot string, maxAge time.Duration) ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}

	sessionsDir := filepath.Join(homeDir, ".codex", "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	cutoff := time.Now().Add(-maxAge)
	pattern := filepath.Join(sessionsDir, "*", "*", "*", "rollout-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil
	}

	var sessions []string
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		// Quick check: read first line to verify cwd matches
		if matchesRepo(path, repoRoot) {
			sessions = append(sessions, path)
		}
	}
	return sessions, nil
}

// matchesRepo reads the first line (session_meta) to check if cwd matches.
func matchesRepo(jsonlPath string, repoRoot string) bool {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return false
	}

	var line codexLine
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return false
	}
	if line.Type != "session_meta" {
		return false
	}

	var meta codexSessionMeta
	if err := json.Unmarshal(line.Payload, &meta); err != nil {
		return false
	}
	return meta.CWD == repoRoot
}

// parseCodexSession streams a Codex JSONL file and extracts session info.
func parseCodexSession(jsonlPath string) (*SessionInfo, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	info := &SessionInfo{
		Tool:         ToolCodex,
		FilesWritten: make(map[string]struct{}),
	}

	var firstTimestamp, lastTimestamp time.Time
	var lastTotalTokens int64

	for scanner.Scan() {
		lineBytes := scanner.Bytes()

		var line codexLine
		if err := json.Unmarshal(lineBytes, &line); err != nil {
			continue
		}

		// Track timestamps for session duration
		if line.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, line.Timestamp); err == nil {
				if firstTimestamp.IsZero() || t.Before(firstTimestamp) {
					firstTimestamp = t
				}
				if t.After(lastTimestamp) {
					lastTimestamp = t
				}
			}
		}

		switch line.Type {
		case "turn_context":
			var tc codexTurnContext
			if err := json.Unmarshal(line.Payload, &tc); err == nil && tc.Model != "" {
				info.Model = tc.Model
			}

		case "event_msg":
			// Pre-filter: skip lines without "token_count"
			if !bytes.Contains(line.Payload, []byte(`"token_count"`)) {
				continue
			}
			var ep codexEventPayload
			if err := json.Unmarshal(line.Payload, &ep); err != nil {
				continue
			}
			if ep.Type == "token_count" && ep.Info != nil {
				lastTotalTokens = ep.Info.TotalTokenUsage.TotalTokens
			}

		case "response_item":
			// Pre-filter: skip lines without "exec_command" or "apply_patch"
			if !bytes.Contains(lineBytes, []byte(`"exec_command"`)) &&
				!bytes.Contains(lineBytes, []byte(`"apply_patch"`)) {
				continue
			}
			var ri codexResponseItem
			if err := json.Unmarshal(line.Payload, &ri); err != nil {
				continue
			}
			if ri.Type != "function_call" {
				continue
			}
			if ri.Name == "exec_command" {
				var args codexExecArgs
				if err := json.Unmarshal([]byte(ri.Arguments), &args); err != nil {
					continue
				}
				for _, fp := range extractFilesFromCmd(args.Cmd) {
					info.FilesWritten[fp] = struct{}{}
				}
			}
			// apply_patch may reference files directly — handle if seen
			if ri.Name == "apply_patch" {
				// apply_patch arguments typically contain the file path
				var args struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal([]byte(ri.Arguments), &args); err == nil && args.Path != "" {
					info.FilesWritten[args.Path] = struct{}{}
				}
			}
		}
	}

	if len(info.FilesWritten) == 0 {
		return nil, nil
	}

	info.TotalTokens = lastTotalTokens

	if !firstTimestamp.IsZero() && !lastTimestamp.IsZero() {
		info.SessionDurationSec = int64(lastTimestamp.Sub(firstTimestamp).Seconds())
	}

	return info, scanner.Err()
}

// detectCodex finds recent Codex sessions for the repo and merges their file sets.
func detectCodex(repoRoot string, maxAge time.Duration) (*SessionInfo, error) {
	sessions, err := findCodexSessions(repoRoot, maxAge)
	if err != nil || len(sessions) == 0 {
		return nil, nil
	}

	merged := &SessionInfo{
		Tool:         ToolCodex,
		FilesWritten: make(map[string]struct{}),
	}

	for _, path := range sessions {
		session, err := parseCodexSession(path)
		if err != nil || session == nil {
			continue
		}
		for f := range session.FilesWritten {
			merged.FilesWritten[f] = struct{}{}
		}
		// Use the last session's model and tokens
		if session.Model != "" {
			merged.Model = session.Model
		}
		if session.TotalTokens > merged.TotalTokens {
			merged.TotalTokens = session.TotalTokens
		}
		if session.SessionDurationSec > merged.SessionDurationSec {
			merged.SessionDurationSec = session.SessionDurationSec
		}
	}

	if len(merged.FilesWritten) == 0 {
		return nil, nil
	}
	return merged, nil
}
