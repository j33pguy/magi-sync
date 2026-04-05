// Command mcp-config outputs a valid MCP JSON config block for Claude/Codex/OpenClaw integration.
//
// Usage: magi-sync-mcp-config
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func generateConfig() mcpConfig {
	return mcpConfig{
		MCPServers: map[string]mcpServerEntry{
			"magi-sync": {
				Command: "magi-sync",
				Args:    []string{"--mcp"},
				Env: map[string]string{
					"MAGI_TOKEN":            "${MAGI_TOKEN}",
					"MAGI_SYNC_CONFIG":      "${MAGI_SYNC_CONFIG}",
				},
			},
		},
	}
}

func run() error {
	cfg := generateConfig()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
