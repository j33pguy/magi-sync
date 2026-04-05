package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `server:
  url: http://localhost:9999
  token: test-token
machine:
  id: test-machine
  user: tester
sync:
  mode: push
  interval: 30s
  state_file: ` + filepath.Join(dir, "state.json") + `
privacy:
  mode: allowlist
  redact_secrets: true
agents:
  - type: claude
    enabled: true
    paths:
      - ` + filepath.Join(dir, "agents") + `
    include:
      - "**/*.md"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create agent dir
	agentDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestNewServer(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv := New(cfgPath, logger)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.mcpServer == nil {
		t.Fatal("expected non-nil MCP server")
	}
}

func TestHandleSyncConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv := New(cfgPath, logger)
	result, err := srv.handleSyncConfig(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// Extract text content
	text := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			text = tc.Text
		}
	}
	if text == "" {
		t.Fatal("expected text content in result")
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	server, ok := data["server"].(map[string]any)
	if !ok {
		t.Fatal("expected server field")
	}
	if server["url"] != "http://localhost:9999" {
		t.Errorf("expected url http://localhost:9999, got %v", server["url"])
	}
	// Token should show has_token, not the actual token
	if server["has_token"] != true {
		t.Error("expected has_token=true")
	}
	if _, exists := server["token"]; exists {
		t.Error("token should be redacted, not present in output")
	}
}

func TestHandleSyncAgents(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv := New(cfgPath, logger)
	result, err := srv.handleSyncAgents(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}

	text := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			text = tc.Text
		}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	agents, ok := data["agents"].([]any)
	if !ok || len(agents) == 0 {
		t.Fatal("expected at least one agent")
	}

	agent := agents[0].(map[string]any)
	if agent["type"] != "claude" {
		t.Errorf("expected agent type claude, got %v", agent["type"])
	}
	if agent["enabled"] != true {
		t.Error("expected agent enabled=true")
	}
}

func TestHandleSyncResetRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv := New(cfgPath, logger)

	// Without confirm
	result, err := srv.handleSyncReset(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error when confirm is not set")
	}
}

func TestHandleSyncPreview(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Create a test markdown file in agents dir (at root level to match **/*.md)
	agentDir := filepath.Join(dir, "agents")
	subDir := filepath.Join(agentDir, "project")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "test.md"), []byte("# Test\nSome content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also at root level
	if err := os.WriteFile(filepath.Join(agentDir, "root.md"), []byte("# Root\nContent"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(cfgPath, logger)
	result, err := srv.handleSyncPreview(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}

	text := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			text = tc.Text
		}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	count, ok := data["pending_count"].(float64)
	if !ok {
		t.Fatal("expected pending_count")
	}
	if count < 1 {
		t.Errorf("expected at least 1 pending file, got %v", count)
	}
}

func TestHandleSyncStatus(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	srv := New(cfgPath, logger)
	result, err := srv.handleSyncStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}

	text := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			text = tc.Text
		}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if data["machine_id"] != "test-machine" {
		t.Errorf("expected machine_id test-machine, got %v", data["machine_id"])
	}
	if data["server"] != "http://localhost:9999" {
		t.Errorf("expected server http://localhost:9999, got %v", data["server"])
	}
	// Server should be unreachable in test
	status, ok := data["server_status"].(string)
	if !ok || status == "connected" {
		t.Logf("server_status: %v (expected unreachable in test env)", status)
	}
}
