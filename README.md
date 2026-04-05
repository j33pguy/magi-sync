<p align="center">
  <img src="assets/dont-panic-header.svg" width="500" alt="Don't Panic">
</p>
<h1 align="center">magi-sync</h1>
<p align="center"><strong>Cross-machine memory sync agent for <a href="https://github.com/j33pguy/magi">MAGI</a></strong></p>

---

`magi-sync` watches local AI agent files (Claude, OpenClaw, Codex) and syncs them to a central MAGI server. Install it on every machine where you use AI agents, and your context follows you everywhere.

## Quick Start

```bash
# Download (Linux amd64)
curl -L https://github.com/j33pguy/magi-sync/releases/latest/download/magi-sync-linux-amd64 -o magi-sync
chmod +x magi-sync
sudo mv magi-sync /usr/local/bin/

# Interactive setup
magi-sync init
```

## Install

Pre-built binaries for every platform:

| Platform | Download |
|----------|----------|
| Linux amd64 | `magi-sync-linux-amd64` |
| Linux arm64 | `magi-sync-linux-arm64` |
| macOS Intel | `magi-sync-darwin-amd64` |
| macOS Apple Silicon | `magi-sync-darwin-arm64` |
| Windows | `magi-sync-windows-amd64.exe` |

All binaries are pure Go — no CGO, no dependencies. Download, make executable, run.

### macOS (Homebrew — coming soon)

```bash
# brew install j33pguy/tap/magi-sync
```

### Windows

```powershell
Invoke-WebRequest -Uri "https://github.com/j33pguy/magi-sync/releases/latest/download/magi-sync-windows-amd64.exe" -OutFile "magi-sync.exe"
.\magi-sync.exe init
```

## Setup

### Interactive Wizard (recommended)

```bash
magi-sync init
```

The wizard walks you through:
1. **Server URL** — your MAGI server address (validates via health check)
2. **Authentication** — enroll token or existing machine token
3. **Machine identity** — auto-detects hostname and user
4. **Agent discovery** — scans for installed agents:
   - Claude (`~/.claude/`)
   - OpenClaw (`~/.openclaw/`)
   - Codex (`~/.codex/`)
5. **Privacy** — allowlist/mixed/denylist mode, secret redaction
6. **Config write** — saves to `~/.config/magi-sync/config.yaml`
7. **Optional enrollment** — enroll with the server immediately
8. **Optional dry-run** — preview what would sync

### Manual Config

Create `~/.config/magi-sync/config.yaml`:

```yaml
server:
  url: http://your-magi-server:8302
  token: your-machine-token
  protocol: http

machine:
  id: my-laptop
  user: alice

sync:
  mode: push
  watch: true
  interval: 30s

privacy:
  mode: allowlist
  redact_secrets: true

agents:
  - type: claude
    enabled: true
    paths:
      - ~/.claude
    include:
      - "projects/**/*.jsonl"
      - "projects/**/CLAUDE.md"
      - "memory/**/*.md"
    exclude:
      - "**/tmp/**"
      - "**/*.bin"

  - type: openclaw
    enabled: true
    paths:
      - ~/.openclaw
    include:
      - "workspace/**/*.md"
      - "agents/*/sessions/*.jsonl"
    exclude:
      - "**/tmp/**"
      - "**/cache/**"
      - "**/*.bin"
```

## Modes

| Mode | Command | Description |
|------|---------|-------------|
| **init** | `magi-sync init` | Interactive setup wizard |
| **enroll** | `magi-sync enroll` | Register this machine with the server |
| **check** | `magi-sync check` | Verify config and server connectivity |
| **dry-run** | `magi-sync dry-run` | Preview what would sync (no upload) |
| **once** | `magi-sync once` | Sync once and exit |
| **run** | `magi-sync run` | Sync on interval (default 30s) |
| **watch** | `magi-sync watch` | Sync on file changes (fsnotify) |

## Run as a Service

### Linux (systemd)

```ini
# /etc/systemd/system/magi-sync.service
[Unit]
Description=magi-sync agent
After=network-online.target

[Service]
Type=simple
User=your-user
ExecStart=/usr/local/bin/magi-sync watch
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now magi-sync
```

### macOS (launchd)

```xml
<!-- ~/Library/LaunchAgents/com.magi-sync.agent.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.magi-sync.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/magi-sync</string>
        <string>watch</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.magi-sync.agent.plist
```

### Windows (Task Scheduler)

```powershell
schtasks /create /tn "magi-sync" /tr "C:\path\to\magi-sync.exe watch" /sc onlogon /rl highest
```

## How It Works

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Your Laptop   │     │  MAGI Server    │     │  Your Desktop   │
│                 │     │                 │     │                 │
│  ~/.claude/     │     │  Shared Memory  │     │  ~/.claude/     │
│  ~/.openclaw/   │────▶│  Store (SQLite  │◀────│  ~/.openclaw/   │
│  ~/.codex/      │     │  / Postgres)    │     │  ~/.codex/      │
│                 │     │                 │     │                 │
│  magi-sync      │     │                 │     │  magi-sync      │
│  (watch mode)   │     │                 │     │  (watch mode)   │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

1. **magi-sync** watches configured agent directories for file changes
2. New/modified files are matched against include/exclude patterns
3. Content is optionally redacted (secrets, credentials)
4. Memories are uploaded to MAGI via the `/sync/memories` or `/remember` endpoint
5. **Repository tags** are auto-detected from git remotes and attached to each payload
6. Other machines running magi-sync can recall shared context through MAGI

### Repository Tags

magi-sync automatically detects the git remote for each scanned directory and tags
uploaded memories with a compact repo identifier:

| Git Host | Tag Format | Example |
|----------|------------|----------|
| GitHub | `ghrepo:<owner>/<repo>` | `ghrepo:j33pguy/magi` |
| GitLab | `glrepo:<owner>/<repo>` | `glrepo:org/project` |
| Other | `repo:<host>/<owner>/<repo>` | `repo:git.example.com/team/app` |

This enables querying MAGI by repository:

```bash
# All memories from a specific repo
curl "http://magi:8302/memories?tags=ghrepo:j33pguy/magi"

# All tracked project/repo registry entries
curl "http://magi:8302/memories?tags=inventory"
```

## Privacy

magi-sync never uploads anything you don't explicitly allow:

- **allowlist** (default) — only files matching `include` patterns are synced
- **mixed** — include patterns sync, exclude patterns are blocked
- **denylist** — everything syncs except exclude patterns
- **Secret redaction** — strips Vault tokens, API keys, bearer tokens before upload
- **File size limit** — files over 512KB (configurable) are skipped

## Requirements

- A running [MAGI](https://github.com/j33pguy/magi) server (v0.4.1+)
- No dependencies — single static binary

## License

[Elastic License 2.0 (ELv2)](LICENSE)
