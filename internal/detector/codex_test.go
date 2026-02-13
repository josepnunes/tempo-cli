package detector

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFilesFromCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{
			name: "cat heredoc",
			cmd:  `cat > backend/app/main.py <<'EOF'\nfrom fastapi import FastAPI\nEOF`,
			want: []string{"backend/app/main.py"},
		},
		{
			name: "cat redirect",
			cmd:  `cat > src/index.ts`,
			want: []string{"src/index.ts"},
		},
		{
			name: "touch single",
			cmd:  `touch backend/app/__init__.py`,
			want: []string{"backend/app/__init__.py"},
		},
		{
			name: "touch multiple",
			cmd:  `touch backend/app/__init__.py backend/app/core/__init__.py`,
			want: []string{"backend/app/__init__.py", "backend/app/core/__init__.py"},
		},
		{
			name: "tee",
			cmd:  `echo "hello" | tee output.txt`,
			want: []string{"output.txt"},
		},
		{
			name: "tee append",
			cmd:  `echo "hello" | tee -a output.txt`,
			want: []string{"output.txt"},
		},
		{
			name: "cp",
			cmd:  `cp src/old.go src/new.go`,
			want: []string{"src/new.go"},
		},
		{
			name: "mv",
			cmd:  `mv src/old.go src/new.go`,
			want: []string{"src/new.go"},
		},
		{
			name: "sed in-place",
			cmd:  `sed -i 's/old/new/g' config.yaml`,
			want: []string{"config.yaml"},
		},
		{
			name: "mkdir ignored",
			cmd:  `mkdir -p backend/app/core backend/app/routers`,
			want: nil,
		},
		{
			name: "ls ignored",
			cmd:  `ls -la`,
			want: nil,
		},
		{
			name: "echo redirect not matched",
			cmd:  `echo "package main" > main.go`,
			want: nil,
		},
		{
			name: "echo append not matched",
			cmd:  `echo "more" >> main.go`,
			want: nil,
		},
		{
			name: "dev null ignored",
			cmd:  `cat > /dev/null`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilesFromCmd(tt.cmd)
			if len(got) != len(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"main.go"`, "main.go"},
		{`'main.go'`, "main.go"},
		{"<<EOF", ""},
		{"<<'EOF'", ""},
		{"/dev/null", ""},
		{"-rf", ""},
		{"src/", ""},
		{"", ""},
		{"main.go", "main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanPath(tt.input)
			if got != tt.want {
				t.Errorf("cleanPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

const testCodexJSONL = `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"id":"019c4716","timestamp":"2026-02-10T10:25:57.659Z","cwd":"/Users/jose/myproject","originator":"codex_cli","source":"cli","model_provider":"openai"}}
{"timestamp":"2026-02-10T10:25:57.753Z","type":"turn_context","payload":{"cwd":"/Users/jose/myproject","model":"gpt-5.3-codex"}}
{"timestamp":"2026-02-10T10:25:58.000Z","type":"event_msg","payload":{"type":"user_message","message":"create a main.go file"}}
{"timestamp":"2026-02-10T10:26:10.889Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":9158,"cached_input_tokens":8960,"output_tokens":360,"reasoning_output_tokens":222,"total_tokens":9518},"last_token_usage":{"input_tokens":9158,"cached_input_tokens":8960,"output_tokens":360,"reasoning_output_tokens":222,"total_tokens":9518}}}}
{"timestamp":"2026-02-10T10:26:36.040Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"cat > backend/app/main.py <<'EOF'\\nfrom fastapi import FastAPI\\nEOF\"}","call_id":"call_01"}}
{"timestamp":"2026-02-10T10:26:40.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"touch backend/app/__init__.py backend/app/core/__init__.py\"}","call_id":"call_02"}}
{"timestamp":"2026-02-10T10:27:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":17992,"cached_input_tokens":16000,"output_tokens":529,"reasoning_output_tokens":291,"total_tokens":18521},"last_token_usage":{"input_tokens":8834,"cached_input_tokens":7040,"output_tokens":169,"reasoning_output_tokens":69,"total_tokens":9003}}}}
{"timestamp":"2026-02-10T10:27:30.040Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"cat > backend/app/main.py <<'EOF'\\nupdated content\\nEOF\"}","call_id":"call_03"}}`

func TestParseCodexSession_Basic(t *testing.T) {
	path := writeTestJSONL(t, testCodexJSONL)
	info, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil session info")
	}

	// Verify files: backend/app/main.py (deduped), backend/app/__init__.py, backend/app/core/__init__.py
	wantFiles := []string{"backend/app/__init__.py", "backend/app/core/__init__.py", "backend/app/main.py"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}

	if info.Model != "gpt-5.3-codex" {
		t.Errorf("model: got %q, want %q", info.Model, "gpt-5.3-codex")
	}

	// Token usage: last total_token_usage.total_tokens = 18521
	if info.TotalTokens != 18521 {
		t.Errorf("tokens: got %d, want %d", info.TotalTokens, 18521)
	}

	// Session duration: 10:25:57.694 to 10:27:30.040 ≈ 92 seconds
	if info.SessionDurationSec < 90 || info.SessionDurationSec > 95 {
		t.Errorf("duration: got %d, want ~92", info.SessionDurationSec)
	}

	if info.Tool != ToolCodex {
		t.Errorf("tool: got %q, want %q", info.Tool, ToolCodex)
	}
}

func TestParseCodexSession_NoWrites(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"cwd":"/Users/jose/myproject"}}
{"timestamp":"2026-02-10T10:25:57.753Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}
{"timestamp":"2026-02-10T10:26:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"hello"}}`

	path := writeTestJSONL(t, content)
	info, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for no writes, got %+v", info)
	}
}

func TestParseCodexSession_MalformedLines(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"cat > a.go <<'EOF'\\npackage main\\nEOF\"}"}}
this is not valid json
{"also bad json
{"timestamp":"2026-02-10T10:26:00.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"touch b.go\"}"}}`

	path := writeTestJSONL(t, content)
	info, err := parseCodexSession(path)
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

func TestExtractFilesFromPatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single file",
			input: "*** Begin Patch\n*** Update File: src/main.go\n@@ -1,3 +1,4 @@\n+import \"fmt\"\n",
			want:  []string{"src/main.go"},
		},
		{
			name:  "multiple files",
			input: "*** Begin Patch\n*** Update File: src/main.go\n@@ -1,3 +1,4 @@\n+line\n*** Update File: src/utils.go\n@@ -5,2 +5,3 @@\n+line\n",
			want:  []string{"src/main.go", "src/utils.go"},
		},
		{
			name:  "duplicate file deduped",
			input: "*** Update File: a.go\n@@ ...\n*** Update File: a.go\n@@ ...\n",
			want:  []string{"a.go"},
		},
		{
			name:  "no update file lines",
			input: "*** Begin Patch\nsome other content\n",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilesFromPatch(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseCodexSession_ApplyPatch(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: src/main.go\n@@ -1,3 +1,4 @@\n+import \"fmt\"\n"}}`

	path := writeTestJSONL(t, content)
	info, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	wantFiles := []string{"src/main.go"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}
}

func TestParseCodexSession_ApplyPatchMultiFile(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: src/main.go\n@@ -1,3 +1,4 @@\n+line\n*** Update File: src/utils.go\n@@ -5,2 +5,3 @@\n+line\n"}}`

	path := writeTestJSONL(t, content)
	info, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	wantFiles := []string{"src/main.go", "src/utils.go"}
	gotFiles := sortedKeys(info.FilesWritten)
	if !equal(gotFiles, wantFiles) {
		t.Errorf("files: got %v, want %v", gotFiles, wantFiles)
	}
}

func TestParseCodexSession_ModelUpdate(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.753Z","type":"turn_context","payload":{"model":"gpt-5-codex"}}
{"timestamp":"2026-02-10T10:26:00.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"touch a.go\"}"}}
{"timestamp":"2026-02-10T10:27:00.000Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}`

	path := writeTestJSONL(t, content)
	info, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "gpt-5.3-codex" {
		t.Errorf("model: got %q, want last model %q", info.Model, "gpt-5.3-codex")
	}
}

func TestMatchesRepo(t *testing.T) {
	// Matching cwd
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"cwd":"/Users/jose/myproject"}}`
	path := writeTestJSONL(t, content)
	if !matchesRepo(path, "/Users/jose/myproject") {
		t.Error("expected match for same cwd")
	}

	// Non-matching cwd
	if matchesRepo(path, "/Users/jose/other-project") {
		t.Error("expected no match for different cwd")
	}
}

func TestMatchesRepo_InvalidFile(t *testing.T) {
	path := writeTestJSONL(t, "not valid json")
	if matchesRepo(path, "/Users/jose/myproject") {
		t.Error("expected no match for invalid file")
	}
}

func TestMatchesRepo_NonSessionMeta(t *testing.T) {
	content := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}`
	path := writeTestJSONL(t, content)
	if matchesRepo(path, "/Users/jose/myproject") {
		t.Error("expected no match when first line is not session_meta")
	}
}

func TestFindCodexSessions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := "/Users/jose/myproject"
	sessionDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a matching session file
	matchContent := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"cwd":"/Users/jose/myproject"}}`
	matchPath := filepath.Join(sessionDir, "rollout-2026-02-10T10-25-57-abc123.jsonl")
	if err := os.WriteFile(matchPath, []byte(matchContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-matching session file (different repo)
	otherContent := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"cwd":"/Users/jose/other-project"}}`
	otherPath := filepath.Join(sessionDir, "rollout-2026-02-10T11-00-00-def456.jsonl")
	if err := os.WriteFile(otherPath, []byte(otherContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an old session (should be excluded by maxAge)
	oldPath := filepath.Join(sessionDir, "rollout-2026-02-10T08-00-00-old789.jsonl")
	if err := os.WriteFile(oldPath, []byte(matchContent), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}

	sessions, err := findCodexSessions(repoRoot, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %v", len(sessions), sessions)
	}
	if sessions[0] != matchPath {
		t.Errorf("got %q, want %q", sessions[0], matchPath)
	}
}

func TestFindCodexSessions_NoSessionsDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	sessions, err := findCodexSessions("/some/repo", 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if sessions != nil {
		t.Errorf("expected nil for missing sessions dir, got %v", sessions)
	}
}

func TestDetectCodex_MergesSessions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := "/Users/jose/myproject"
	sessionDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Session 1: writes a.go
	session1 := `{"timestamp":"2026-02-10T10:25:57.694Z","type":"session_meta","payload":{"cwd":"/Users/jose/myproject"}}
{"timestamp":"2026-02-10T10:25:57.753Z","type":"turn_context","payload":{"model":"gpt-5-codex"}}
{"timestamp":"2026-02-10T10:26:00.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"touch a.go\"}"}}`
	path1 := filepath.Join(sessionDir, "rollout-2026-02-10T10-25-57-aaa.jsonl")
	if err := os.WriteFile(path1, []byte(session1), 0644); err != nil {
		t.Fatal(err)
	}

	// Session 2: writes b.go with newer model
	session2 := `{"timestamp":"2026-02-10T11:00:00.000Z","type":"session_meta","payload":{"cwd":"/Users/jose/myproject"}}
{"timestamp":"2026-02-10T11:00:00.100Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}
{"timestamp":"2026-02-10T11:00:01.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"touch b.go\"}"}}`
	path2 := filepath.Join(sessionDir, "rollout-2026-02-10T11-00-00-bbb.jsonl")
	if err := os.WriteFile(path2, []byte(session2), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := detectCodex(repoRoot, 72*time.Hour)
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

	if info.Model != "gpt-5.3-codex" {
		t.Errorf("model: got %q, want %q", info.Model, "gpt-5.3-codex")
	}
}
