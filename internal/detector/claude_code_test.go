package detector

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const testRepoRoot = "/Users/jose/myproject"

const testJSONLBasic = `{"type":"queue-operation","operation":"dequeue","timestamp":"2026-02-12T10:00:00Z","sessionId":"test-session"}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"timestamp":"2026-02-12T10:00:01Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Edit","input":{"file_path":"/Users/jose/myproject/src/main.go","old_string":"old","new_string":"new"}}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":300}},"timestamp":"2026-02-12T10:01:00Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"toolu_02","name":"Write","input":{"file_path":"/Users/jose/myproject/tests/main_test.go","content":"package main"}}],"usage":{"input_tokens":80,"output_tokens":40,"cache_creation_input_tokens":0,"cache_read_input_tokens":500}},"timestamp":"2026-02-12T10:02:00Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"toolu_03","name":"Read","input":{"file_path":"/Users/jose/myproject/go.mod"}}],"usage":{"input_tokens":50,"output_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"timestamp":"2026-02-12T10:03:00Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","role":"assistant","content":[{"type":"tool_use","id":"toolu_04","name":"Edit","input":{"file_path":"/Users/jose/myproject/src/main.go","old_string":"another","new_string":"change"}}],"usage":{"input_tokens":90,"output_tokens":30,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}},"timestamp":"2026-02-12T10:04:00Z"}`

func writeTestJSONL(t *testing.T, content string) string {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "test.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return tmpFile
}

func TestParseClaudeSession_Basic(t *testing.T) {
	path := writeTestJSONL(t, testJSONLBasic)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil session info")
	}

	// Verify files: src/main.go (deduped from 2 edits) and tests/main_test.go
	// Read tool_use for go.mod should be excluded
	wantFiles := []string{"src/main.go", "tests/main_test.go"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}

	if info.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q, want %q", info.Model, "claude-opus-4-6")
	}

	// Token sum: (100+50+200+300) + (80+40+0+500) + (50+10+0+0) + (90+30+0+100) = 1550
	if info.TotalTokens != 1550 {
		t.Errorf("tokens: got %d, want %d", info.TotalTokens, 1550)
	}

	// Session duration: 10:01:00 to 10:04:00 = 180 seconds
	// (first assistant timestamp is 10:01:00, last is 10:04:00)
	if info.SessionDurationSec != 180 {
		t.Errorf("duration: got %d, want %d", info.SessionDurationSec, 180)
	}

	if info.Tool != ToolClaudeCode {
		t.Errorf("tool: got %q, want %q", info.Tool, ToolClaudeCode)
	}
}

func TestParseClaudeSession_MalformedLines(t *testing.T) {
	content := `{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/Users/jose/myproject/a.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:00:00Z"}
this is not valid json
{"also bad json
{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/Users/jose/myproject/b.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:01:00Z"}`

	path := writeTestJSONL(t, content)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	wantFiles := []string{"a.go", "b.go"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}
}

func TestParseClaudeSession_EmptySession(t *testing.T) {
	path := writeTestJSONL(t, `{"type":"queue-operation","timestamp":"2026-02-12T10:00:00Z"}`)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for empty session, got %+v", info)
	}
}

func TestParseClaudeSession_NoWrites(t *testing.T) {
	content := `{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/Users/jose/myproject/go.mod"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:00:00Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:01:00Z"}`

	path := writeTestJSONL(t, content)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for no writes, got %+v", info)
	}
}

func TestParseClaudeSession_FileOutsideRepo(t *testing.T) {
	content := `{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/Users/jose/other-project/main.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:00:00Z"}`

	path := writeTestJSONL(t, content)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for file outside repo, got %+v", info)
	}
}

func TestParseClaudeSession_ModelUpdate(t *testing.T) {
	content := `{"type":"assistant","message":{"model":"claude-sonnet-4-20250514","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/Users/jose/myproject/a.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:00:00Z"}
{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/Users/jose/myproject/b.go"}}],"usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2026-02-12T10:01:00Z"}`

	path := writeTestJSONL(t, content)
	info, err := parseClaudeSession(path, testRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q, want last model %q", info.Model, "claude-opus-4-6")
	}
}

func TestClaudeSessionDir(t *testing.T) {
	dir := claudeSessionDir("/Users/jose/projects/tempo")
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
	if !strings.Contains(dir, "-Users-jose-projects-tempo") {
		t.Errorf("expected encoded path in %q", dir)
	}
}

func TestFindLatestSession(t *testing.T) {
	dir := t.TempDir()

	// Create an agent file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "agent-sub.jsonl"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-jsonl file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a valid session file
	sessionPath := filepath.Join(dir, "abc123.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := findLatestSession(dir, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got != sessionPath {
		t.Errorf("got %q, want %q", got, sessionPath)
	}
}

func TestFindLatestSession_NoRecentFiles(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set mod time to 5 days ago
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(sessionPath, old, old); err != nil {
		t.Fatal(err)
	}

	_, err := findLatestSession(dir, 72*time.Hour)
	if err == nil {
		t.Error("expected error for no recent files")
	}
}

func TestFindLatestSession_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := findLatestSession(dir, 72*time.Hour)
	if err == nil {
		t.Error("expected error for empty dir")
	}
}

// --- helpers ---

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

