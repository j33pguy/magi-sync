package syncagent

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunInit runs the interactive setup wizard to generate a config file.
func RunInit(configPath string, logger *slog.Logger) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║       magi-sync setup wizard         ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// Check if config already exists
	expanded, err := LoadConfigPath(configPath)
	if err == nil {
		if _, err := os.Stat(expanded); err == nil {
			fmt.Printf("⚠  Config already exists at %s\n", expanded)
			fmt.Print("Overwrite? [y/N]: ")
			answer, _ := reader.ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
				fmt.Println("Aborted.")
				return nil
			}
		}
	}

	cfg := &Config{}

	// --- Server ---
	fmt.Println("─── MAGI Server ───")
	cfg.Server.URL = prompt(reader, "Server URL", "http://localhost:8302")
	cfg.Server.Protocol = "http"
	if strings.HasPrefix(cfg.Server.URL, "https://") {
		cfg.Server.Protocol = "https"
	}

	// Test connectivity
	fmt.Printf("  Testing connection to %s... ", cfg.Server.URL)
	if err := testHealth(cfg.Server.URL); err != nil {
		fmt.Printf("⚠  %v\n", err)
		fmt.Println("  (You can fix the URL later in the config file)")
	} else {
		fmt.Println("✓ connected")
	}
	fmt.Println()

	// --- Auth ---
	fmt.Println("─── Authentication ───")
	fmt.Println("  1) I have a machine token")
	fmt.Println("  2) I have an enrollment token (server will issue a machine token)")
	fmt.Println("  3) Skip auth for now")
	authChoice := prompt(reader, "Choice [1/2/3]", "3")
	switch strings.TrimSpace(authChoice) {
	case "1":
		cfg.Server.Token = prompt(reader, "Machine token", "")
	case "2":
		cfg.Server.EnrollToken = prompt(reader, "Enrollment token", "")
		fmt.Println("  → Run `magi-sync enroll` after setup to exchange for a machine token")
	}
	fmt.Println()

	// --- Machine ---
	fmt.Println("─── Machine Identity ───")
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	cfg.Machine.ID = prompt(reader, "Machine ID", hostname)
	cfg.Machine.User = prompt(reader, "Username", username)
	fmt.Println()

	// --- Agent Discovery ---
	fmt.Println("─── Agent Discovery ───")
	fmt.Println("  Scanning for installed agents...")

	type agentCandidate struct {
		agentType string
		path      string
		includes  []string
		excludes  []string
	}

	candidates := []agentCandidate{
		{
			agentType: "claude",
			path:      "~/.claude",
			includes:  []string{"projects/**/*.jsonl", "projects/**/CLAUDE.md", "memory/**/*.md"},
			excludes:  []string{"**/tmp/**", "**/*.bin"},
		},
		{
			agentType: "openclaw",
			path:      "~/.openclaw",
			includes:  []string{"workspace/**/*.md", "agents/*/sessions/*.jsonl"},
			excludes:  []string{"**/tmp/**", "**/cache/**", "**/*.bin"},
		},
		{
			agentType: "codex",
			path:      "~/.codex",
			includes:  []string{"sessions/**/*.jsonl"},
			excludes:  []string{"**/*.bin"},
		},
	}

	for _, c := range candidates {
		expanded, err := expandPath(c.path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(expanded); err != nil {
			fmt.Printf("  ✗ %s (%s) — not found\n", c.agentType, c.path)
			continue
		}
		fmt.Printf("  ✓ %s (%s) — found!\n", c.agentType, c.path)
		enable := prompt(reader, fmt.Sprintf("    Enable %s sync? [Y/n]", c.agentType), "y")
		enabled := !strings.HasPrefix(strings.ToLower(strings.TrimSpace(enable)), "n")
		cfg.Agents = append(cfg.Agents, AgentConfig{
			Type:       c.agentType,
			Name:       c.agentType,
			Enabled:    enabled,
			Paths:      []string{c.path},
			Include:    c.includes,
			Exclude:    c.excludes,
			Visibility: "internal",
			Owner:      cfg.Machine.User,
		})
	}

	if len(cfg.Agents) == 0 {
		fmt.Println("  No agents detected. You can add them manually to the config file.")
	}
	fmt.Println()

	// --- Privacy ---
	fmt.Println("─── Privacy ───")
	fmt.Println("  Privacy modes:")
	fmt.Println("    allowlist — only included patterns sync (safest)")
	fmt.Println("    mixed    — include + exclude patterns")
	fmt.Println("    denylist — everything except excluded patterns")
	cfg.Privacy.Mode = prompt(reader, "Privacy mode", "allowlist")
	redact := prompt(reader, "Redact secrets (API keys, tokens, passwords)? [Y/n]", "y")
	cfg.Privacy.RedactSecrets = !strings.HasPrefix(strings.ToLower(strings.TrimSpace(redact)), "n")
	cfg.Privacy.MaxFileSizeKB = 512
	fmt.Println()

	// --- Sync ---
	cfg.Sync.Mode = "push"
	cfg.Sync.Interval = "30s"
	cfg.Sync.RetryBackoff = "5s"
	cfg.Sync.MaxBatchSize = 50
	cfg.Sync.ConflictStrategy = ConflictLastWriteWins

	// Resolve config path
	cfgPath, err := LoadConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	cfgDir := filepath.Dir(cfgPath)
	cfg.Sync.StateFile = filepath.Join(cfgDir, "state.json")

	// --- Write config ---
	fmt.Printf("─── Writing config to %s ───\n", cfgPath)
	if err := SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("  ✓ Config saved")
	fmt.Println()

	// --- Optional enrollment ---
	if cfg.Server.EnrollToken != "" {
		fmt.Print("Run enrollment now? [Y/n]: ")
		answer, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "n") {
			// Reload config (to pick up defaults)
			reloaded, err := LoadConfig(cfgPath)
			if err != nil {
				fmt.Printf("  ⚠  Could not reload config: %v\n", err)
			} else {
				app, err := New(reloaded, cfgPath, logger)
				if err != nil {
					fmt.Printf("  ⚠  Could not create app: %v\n", err)
				} else {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := app.Run(ctx, ModeEnroll); err != nil {
						fmt.Printf("  ⚠  Enrollment failed: %v\n", err)
					} else {
						fmt.Println("  ✓ Enrolled successfully")
					}
				}
			}
		}
		fmt.Println()
	}

	// --- Optional dry-run ---
	fmt.Print("Run a dry-run preview? [Y/n]: ")
	answer, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "n") {
		reloaded, err := LoadConfig(cfgPath)
		if err != nil {
			fmt.Printf("  ⚠  Could not reload config: %v\n", err)
		} else {
			app, err := New(reloaded, cfgPath, logger)
			if err != nil {
				fmt.Printf("  ⚠  Could not create app: %v\n", err)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := app.Run(ctx, ModeDryRun); err != nil {
					fmt.Printf("  ⚠  Dry-run failed: %v\n", err)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("═══ Setup complete! ═══")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  magi-sync --config %s check     # validate setup\n", configPath)
	fmt.Printf("  magi-sync --config %s once      # sync once\n", configPath)
	fmt.Printf("  magi-sync --config %s watch     # continuous sync\n", configPath)
	fmt.Println()
	fmt.Println("See https://github.com/j33pguy/magi-sync/wiki for full docs.")

	return nil
}

func prompt(reader *bufio.Reader, label string, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func testHealth(baseURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/health")
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return nil
}
