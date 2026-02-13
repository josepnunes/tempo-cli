package detector

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testAiderHistory = `# aider chat started at 2026-02-12 10:00:00

#### src/main.go

some edit content here

#### tests/main_test.go

some test content here

> Applied edit to src/main.go
> Applied edit to tests/main_test.go

#### src/main.go

another edit to the same file
`

func TestDetectAider_Basic(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, ".aider.chat.history.md")
	if err := os.WriteFile(historyPath, []byte(testAiderHistory), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := detectAider(dir, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	// src/main.go appears twice but should be deduped
	wantFiles := []string{"src/main.go", "tests/main_test.go"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}

	if info.Tool != ToolAider {
		t.Errorf("tool: got %q, want %q", info.Tool, ToolAider)
	}
}

func TestDetectAider_NoFile(t *testing.T) {
	info, err := detectAider(t.TempDir(), 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for missing file, got %+v", info)
	}
}

func TestDetectAider_OldFile(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, ".aider.chat.history.md")
	if err := os.WriteFile(historyPath, []byte(testAiderHistory), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(historyPath, old, old); err != nil {
		t.Fatal(err)
	}

	info, err := detectAider(dir, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for old file, got %+v", info)
	}
}

func TestDetectAider_NoFilePaths(t *testing.T) {
	dir := t.TempDir()
	content := "# aider chat started\n\nsome regular text\n> output line\n"
	if err := os.WriteFile(filepath.Join(dir, ".aider.chat.history.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := detectAider(dir, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for no file paths, got %+v", info)
	}
}
