# Tempo CLI

Lightweight git hooks that detect AI tool usage per commit with file-level attribution. Tempo CLI parses local AI session data, matches files the AI wrote against your commit diff, and saves structured attribution metadata — no source code ever leaves your machine.

## How it works

Tempo CLI uses three detection strategies, applied in order of confidence:

| Strategy | Confidence | How it works |
|----------|-----------|--------------|
| **Session file matching** | High | Parses local AI tool session data (Claude Code JSONL, Codex JSONL, Copilot Agent JSON, Aider history) to identify exactly which files the AI wrote, then intersects with your committed files |
| **Process detection** | Medium | Checks if AI tool processes (Cursor, Copilot, etc.) are running at commit time |
| **Git trailers** | Medium | Parses `Co-Authored-By` trailers in commit messages |

## Install
Supported platforms: macOS and Linux (Intel & ARM).

**Homebrew:**

```sh
brew install josepnunes/tap/tempo-cli
```

**curl:**

```sh
curl -fsSL https://get.tempo.dev/install.sh | sh
```

**Go:**

```sh
go install github.com/josepnunes/tempo-cli@latest
```

## Quick start

```sh
# Optional: connect to Tempo cloud for team dashboards
tempo-cli auth <your-token>

# Install hooks in your repo
cd /path/to/your/repo
tempo-cli enable

# Verify detection works
tempo-cli test

# That's it — attribution runs automatically on every commit.
```

## Commands

| Command | Description |
|---------|-------------|
| `tempo-cli enable` | Install post-commit and pre-push hooks |
| `tempo-cli disable` | Remove Tempo hooks (preserves other hooks) |
| `tempo-cli auth <token>` | Save API token for Tempo cloud |
| `tempo-cli status` | Show hooks, pending records, and config |
| `tempo-cli test` | Dry-run detection against the last commit |
| `tempo-cli test --json` | Same as above, but output raw JSON |

## Supported tools

| Tool | File matching | Process detection | Git trailers |
|------|:---:|:---:|:---:|
| Claude Code | Yes | Yes | Yes |
| Aider | Yes | Yes | Yes |
| Cursor | — | Yes | Yes |
| GitHub Copilot | Yes | Yes | Yes |
| Codex | Yes | Yes | — |

## Example output

```
$ tempo-cli test
Commit:  a1b2c3d
Author:  jose@tempo.dev
Repo:    tempo-metrics/tempo

🟢  claude-code (high confidence, file-match)
   Files: 2/5 committed files matched
     - src/auth.ts
     - src/auth.test.ts
   Model: claude-opus-4-6
   Tokens: 24500
   Session: 14m0s
```

## Attribution payload

Each detection produces a JSON file in `.tempo/pending/`. No source code, diffs, prompts, or conversation transcripts are ever included — only metadata:

```json
{
  "commit_sha": "a1b2c3d",
  "commit_author": "jose@tempo.dev",
  "repo": "tempo-metrics/tempo",
  "timestamp": "2026-02-12T17:08:00Z",
  "detections": [
    {
      "tool": "claude-code",
      "confidence": "high",
      "method": "file-match",
      "files_matched": ["src/auth.ts", "src/auth.test.ts"],
      "files_committed": 5,
      "ai_files": 2,
      "model": "claude-opus-4-6",
      "token_usage": 24500,
      "session_duration_sec": 840
    }
  ]
}
```

## Privacy

Tempo CLI runs entirely locally. The only data that leaves your machine (if you configure an API token) is the attribution metadata shown above. Specifically, Tempo CLI **never** collects:

- Source code or file contents
- Diffs or patches
- AI prompts or conversation transcripts
- Personal information beyond git commit author email

## Configuration

Config is stored at `~/.tempo/config.json`:

```json
{
  "api_token": "tpo_abc123...",
  "endpoint": "https://api.tempo.dev"
}
```

**Environment variables:**

| Variable | Description |
|----------|-------------|
| `TEMPO_API_ENDPOINT` | Override the API endpoint |
| `TEMPO_SESSION_MAX_AGE` | Session recency window in hours (default: 72) |

## Offline mode

If no API token is configured, Tempo CLI works exactly the same — detection runs, JSON files are saved to `.tempo/pending/`, but nothing is sent to the cloud. Use this for:

- Local AI usage tracking
- Evaluating before connecting to Tempo cloud
- Inspecting attribution data (`cat .tempo/pending/*.json`)

## Development

```sh
git clone https://github.com/josepnunes/tempo-cli
cd tempo-cli
go build ./...
go test ./...
```

## License

Apache-2.0
