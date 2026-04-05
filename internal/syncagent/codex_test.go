package syncagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexAdapterScanJSONL(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a JSONL session file
	content := `{"type":"message","role":"user","content":"fix the build"}
{"type":"message","role":"assistant","content":"I found the issue in main.go line 42"}
`
	if err := os.WriteFile(filepath.Join(sessDir, "session1.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Machine: MachineConfig{ID: "test", User: "tester"},
	}
	agent := AgentConfig{
		Type:       "codex",
		Name:       "codex",
		Enabled:    true,
		Paths:      []string{dir},
		Include:    []string{"sessions/**/*.jsonl"},
		Visibility: "internal",
		Owner:      "tester",
	}
	privacy := PrivacyConfig{
		Mode:          "allowlist",
		RedactSecrets: false,
		MaxFileSizeKB: 512,
	}

	payloads, err := (codexAdapter{}).Scan(cfg, agent, privacy)
	if err != nil {
		t.Fatal(err)
	}

	if len(payloads) == 0 {
		t.Fatal("expected at least one payload")
	}

	// Check source tags are codex, not claude
	for _, p := range payloads {
		for _, tag := range p.Tags {
			if tag == "source:claude_sync" {
				t.Error("codex adapter should not produce claude_sync tags")
			}
		}
		if p.Source != "magi-sync" {
			t.Errorf("expected source magi-sync, got %s", p.Source)
		}
	}
}

func TestCodexAdapterScanMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My Project\nInstructions here"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Machine: MachineConfig{ID: "test", User: "tester"},
	}
	agent := AgentConfig{
		Type:       "codex",
		Name:       "codex",
		Enabled:    true,
		Paths:      []string{dir},
		Include:    []string{"**/*.md"},
		Visibility: "internal",
		Owner:      "tester",
	}
	privacy := PrivacyConfig{
		Mode:          "allowlist",
		MaxFileSizeKB: 512,
	}

	payloads, err := (codexAdapter{}).Scan(cfg, agent, privacy)
	if err != nil {
		t.Fatal(err)
	}

	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	if payloads[0].Type != "project_context" {
		t.Errorf("expected type project_context, got %s", payloads[0].Type)
	}
}

func TestCodexAdapterSkipsExcluded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), []byte("binary data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Machine: MachineConfig{ID: "test", User: "tester"},
	}
	agent := AgentConfig{
		Type:       "codex",
		Name:       "codex",
		Enabled:    true,
		Paths:      []string{dir},
		Include:    []string{"**/*"},
		Exclude:    []string{"**/*.bin"},
		Visibility: "internal",
		Owner:      "tester",
	}
	privacy := PrivacyConfig{MaxFileSizeKB: 512}

	payloads, err := (codexAdapter{}).Scan(cfg, agent, privacy)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range payloads {
		if filepath.Ext(p.SourcePath) == ".bin" {
			t.Error("should have excluded .bin files")
		}
	}
}
