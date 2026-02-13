package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupFakeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstall_EmptyHooksDir(t *testing.T) {
	repo := setupFakeRepo(t)
	if err := Install(repo); err != nil {
		t.Fatal(err)
	}

	// Check post-commit exists with shebang and markers
	data, err := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "#!/bin/sh\n") {
		t.Error("missing shebang")
	}
	if !strings.Contains(content, startMarker) {
		t.Error("missing start marker")
	}
	if !strings.Contains(content, endMarker) {
		t.Error("missing end marker")
	}
	if !strings.Contains(content, "tempo-cli _detect") {
		t.Error("missing detect command")
	}

	// Check pre-push
	data, err = os.ReadFile(filepath.Join(repo, ".git", "hooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tempo-cli _sync") {
		t.Error("missing sync command in pre-push")
	}

	// Check .tempo/pending/ directory
	info, err := os.Stat(filepath.Join(repo, ".tempo", "pending"))
	if err != nil {
		t.Fatal("missing .tempo/pending directory")
	}
	if !info.IsDir() {
		t.Error(".tempo/pending is not a directory")
	}

	// Check .gitignore
	gitignore, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitignore), ".tempo/") {
		t.Error(".gitignore missing .tempo/")
	}
}

func TestInstall_AppendToExistingHook(t *testing.T) {
	repo := setupFakeRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

	// Write an existing hook (e.g., from husky)
	existing := "#!/bin/sh\necho 'husky hook'\n"
	if err := os.WriteFile(hookPath, []byte(existing), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Install(repo); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(hookPath)
	content := string(data)

	// Original content preserved
	if !strings.Contains(content, "echo 'husky hook'") {
		t.Error("existing hook content lost")
	}
	// Tempo section appended
	if !strings.Contains(content, startMarker) {
		t.Error("tempo section not appended")
	}
}

func TestInstall_ReplaceExistingTempoSection(t *testing.T) {
	repo := setupFakeRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

	// Write a hook that already has a Tempo section
	existing := "#!/bin/sh\necho 'other hook'\n" + startMarker + "\nold tempo content\n" + endMarker + "\n"
	if err := os.WriteFile(hookPath, []byte(existing), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Install(repo); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(hookPath)
	content := string(data)

	// Old Tempo content replaced
	if strings.Contains(content, "old tempo content") {
		t.Error("old tempo content not replaced")
	}
	// New Tempo content present
	if !strings.Contains(content, "tempo-cli _detect") {
		t.Error("new tempo content missing")
	}
	// Other hook content preserved
	if !strings.Contains(content, "echo 'other hook'") {
		t.Error("other hook content lost")
	}
	// Exactly one start marker
	if strings.Count(content, startMarker) != 1 {
		t.Errorf("expected 1 start marker, got %d", strings.Count(content, startMarker))
	}
}

func TestInstall_Idempotent(t *testing.T) {
	repo := setupFakeRepo(t)

	if err := Install(repo); err != nil {
		t.Fatal(err)
	}
	if err := Install(repo); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	content := string(data)

	if strings.Count(content, startMarker) != 1 {
		t.Errorf("expected 1 start marker after double install, got %d", strings.Count(content, startMarker))
	}
}

func TestUninstall_RemovesTempoSection(t *testing.T) {
	repo := setupFakeRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

	existing := "#!/bin/sh\necho 'other'\n" + startMarker + "\ntempo stuff\n" + endMarker + "\n"
	if err := os.WriteFile(hookPath, []byte(existing), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(repo); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if strings.Contains(content, startMarker) {
		t.Error("start marker still present")
	}
	if !strings.Contains(content, "echo 'other'") {
		t.Error("other hook content lost")
	}
}

func TestUninstall_DeletesFileIfOnlyTempo(t *testing.T) {
	repo := setupFakeRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

	content := "#!/bin/sh\n" + startMarker + "\ntempo stuff\n" + endMarker + "\n"
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(repo); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Error("hook file should be deleted when only Tempo content")
	}
}

func TestUninstall_NoopWhenNoHook(t *testing.T) {
	repo := setupFakeRepo(t)
	if err := Uninstall(repo); err != nil {
		t.Fatal(err)
	}
}

func TestUninstall_NoopWhenNoTempoSection(t *testing.T) {
	repo := setupFakeRepo(t)
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho 'other'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(repo); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(hookPath)
	if !strings.Contains(string(data), "echo 'other'") {
		t.Error("hook content should be preserved")
	}
}

func TestIsInstalled(t *testing.T) {
	repo := setupFakeRepo(t)

	if IsInstalled(repo) {
		t.Error("should not be installed initially")
	}

	if err := Install(repo); err != nil {
		t.Fatal(err)
	}

	if !IsInstalled(repo) {
		t.Error("should be installed after Install()")
	}
}

func TestEnsureGitignore_CreatesNew(t *testing.T) {
	repo := setupFakeRepo(t)
	ensureGitignore(repo)

	data, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if !strings.Contains(string(data), ".tempo/") {
		t.Error("missing .tempo/ in new .gitignore")
	}
}

func TestEnsureGitignore_AppendsToExisting(t *testing.T) {
	repo := setupFakeRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ensureGitignore(repo)

	data, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	content := string(data)
	if !strings.Contains(content, "node_modules/") {
		t.Error("existing content lost")
	}
	if !strings.Contains(content, ".tempo/") {
		t.Error("missing .tempo/")
	}
}

func TestEnsureGitignore_Idempotent(t *testing.T) {
	repo := setupFakeRepo(t)
	ensureGitignore(repo)
	ensureGitignore(repo)

	data, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if strings.Count(string(data), ".tempo/") != 1 {
		t.Error(".tempo/ should appear exactly once")
	}
}
