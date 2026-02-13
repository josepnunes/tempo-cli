package detector

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultMaxAgeHours = 72

// emptyTreeSHA is the SHA of git's empty tree object, used to diff against
// when HEAD~1 doesn't exist (e.g. first commit or shallow clone).
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf899d69f82cf7186"

// sessionMaxAge returns the max session age, defaulting to 72h.
// Override with TEMPO_SESSION_MAX_AGE env var (value in hours).
func sessionMaxAge() time.Duration {
	if v := os.Getenv("TEMPO_SESSION_MAX_AGE"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return defaultMaxAgeHours * time.Hour
}

// Detect runs the full detection pipeline for the current HEAD commit.
func Detect(repoRoot string) (*Attribution, error) {
	committedFiles, err := getCommittedFiles(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("getting committed files: %w", err)
	}
	if len(committedFiles) == 0 {
		return nil, nil
	}

	commitSHA, _ := gitOutput(repoRoot, "rev-parse", "HEAD")
	commitAuthor, _ := gitOutput(repoRoot, "log", "-1", "--format=%ae")
	commitMsg, _ := gitOutput(repoRoot, "log", "-1", "--format=%B")
	repo := parseRepoFromRemote(repoRoot)

	attr := &Attribution{
		CommitSHA:    strings.TrimSpace(commitSHA),
		CommitAuthor: strings.TrimSpace(commitAuthor),
		Repo:         repo,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	committedSet := toSet(committedFiles)
	maxAge := sessionMaxAge()

	// Strategy 1: File matching (HIGH confidence)
	fileMatchDetected := make(map[Tool]bool)

	// Claude Code
	if session, err := detectClaudeCode(repoRoot, maxAge); err == nil && session != nil {
		matched := intersect(session.FilesWritten, committedSet)
		if len(matched) > 0 {
			fileMatchDetected[ToolClaudeCode] = true
			attr.Detections = append(attr.Detections, Detection{
				Tool:               ToolClaudeCode,
				Confidence:         ConfidenceHigh,
				Method:             MethodFileMatch,
				FilesMatched:       matched,
				FilesCommitted:     len(committedFiles),
				AIFiles:            len(matched),
				Model:              session.Model,
				TokenUsage:         session.TotalTokens,
				SessionDurationSec: session.SessionDurationSec,
			})
		}
	}

	// Aider
	if session, err := detectAider(repoRoot, maxAge); err == nil && session != nil {
		matched := intersect(session.FilesWritten, committedSet)
		if len(matched) > 0 {
			fileMatchDetected[ToolAider] = true
			attr.Detections = append(attr.Detections, Detection{
				Tool:           ToolAider,
				Confidence:     ConfidenceHigh,
				Method:         MethodFileMatch,
				FilesMatched:   matched,
				FilesCommitted: len(committedFiles),
				AIFiles:        len(matched),
			})
		}
	}

	// Codex
	if session, err := detectCodex(repoRoot, maxAge); err == nil && session != nil {
		matched := intersect(session.FilesWritten, committedSet)
		if len(matched) > 0 {
			fileMatchDetected[ToolCodex] = true
			attr.Detections = append(attr.Detections, Detection{
				Tool:               ToolCodex,
				Confidence:         ConfidenceHigh,
				Method:             MethodFileMatch,
				FilesMatched:       matched,
				FilesCommitted:     len(committedFiles),
				AIFiles:            len(matched),
				Model:              session.Model,
				TokenUsage:         session.TotalTokens,
				SessionDurationSec: session.SessionDurationSec,
			})
		}
	}

	// Strategy 2: Process detection (MEDIUM confidence)
	for _, tool := range detectProcesses() {
		if !fileMatchDetected[tool] {
			attr.Detections = append(attr.Detections, Detection{
				Tool:           tool,
				Confidence:     ConfidenceMedium,
				Method:         MethodProcess,
				FilesCommitted: len(committedFiles),
			})
		}
	}

	// Strategy 3: Trailer detection (MEDIUM confidence)
	alreadyDetected := make(map[Tool]bool)
	for _, d := range attr.Detections {
		alreadyDetected[d.Tool] = true
	}
	for _, d := range detectTrailers(commitMsg) {
		if !alreadyDetected[d.Tool] {
			d.FilesCommitted = len(committedFiles)
			attr.Detections = append(attr.Detections, d)
		}
	}

	if len(attr.Detections) == 0 {
		return nil, nil
	}

	return attr, nil
}

func detectClaudeCode(repoRoot string, maxAge time.Duration) (*SessionInfo, error) {
	sessionDir := claudeSessionDir(repoRoot)
	if sessionDir == "" {
		return nil, nil
	}
	jsonlPath, err := findLatestSession(sessionDir, maxAge)
	if err != nil {
		return nil, err
	}
	return parseClaudeSession(jsonlPath, repoRoot)
}

func getCommittedFiles(repoRoot string) ([]string, error) {
	output, err := gitOutput(repoRoot, "diff", "--name-only", "HEAD~1", "HEAD")
	if err != nil {
		// First commit — diff against empty tree
		output, err = gitOutput(repoRoot, "diff", "--name-only",
			emptyTreeSHA, "HEAD")
		if err != nil {
			return nil, err
		}
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(output), "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	return string(out), err
}

func parseRepoFromRemote(repoRoot string) string {
	output, err := gitOutput(repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return parseRemoteURL(strings.TrimSpace(output))
}

func parseRemoteURL(remote string) string {
	// Handle SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(remote, "git@") {
		remote = strings.TrimPrefix(remote, "git@")
		remote = strings.Replace(remote, ":", "/", 1)
	}

	// Remove protocol
	remote = strings.TrimPrefix(remote, "https://")
	remote = strings.TrimPrefix(remote, "http://")

	// Remove host
	parts := strings.SplitN(remote, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	repo := parts[1]
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

func toSet(slice []string) map[string]struct{} {
	m := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		m[s] = struct{}{}
	}
	return m
}

func intersect(aiFiles map[string]struct{}, committedFiles map[string]struct{}) []string {
	var result []string
	for f := range aiFiles {
		if _, ok := committedFiles[f]; ok {
			result = append(result, f)
		}
	}
	sort.Strings(result)
	return result
}
