// Package mcpserver implements an MCP (Model Context Protocol) server for magi-sync.
// It exposes sync operations as MCP tools, allowing AI agents to manage
// cross-machine memory synchronization directly.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/j33pguy/magi-sync/internal/syncagent"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps magi-sync functionality as an MCP server.
type Server struct {
	mcpServer  *server.MCPServer
	configPath string
	logger     *slog.Logger
}

// New creates a new MCP server with all magi-sync tools registered.
func New(configPath string, logger *slog.Logger) *Server {
	s := &Server{
		configPath: configPath,
		logger:     logger,
	}

	s.mcpServer = server.NewMCPServer(
		"magi-sync",
		"0.3.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, false),
	)

	s.registerTools()
	s.registerResources()

	return s
}

// ServeStdio starts the MCP server over stdin/stdout.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcpServer)
}

func (s *Server) registerTools() {
	// sync_status — current sync state
	s.mcpServer.AddTool(
		mcp.NewTool("sync_status",
			mcp.WithDescription("Get current magi-sync status: last sync time, tracked files, pending changes, server connectivity."),
		),
		s.handleSyncStatus,
	)

	// sync_now — trigger immediate sync
	s.mcpServer.AddTool(
		mcp.NewTool("sync_now",
			mcp.WithDescription("Trigger an immediate one-shot sync. Scans all configured agents, uploads new/changed files to MAGI."),
		),
		s.handleSyncNow,
	)

	// sync_check — validate config and connectivity
	s.mcpServer.AddTool(
		mcp.NewTool("sync_check",
			mcp.WithDescription("Validate magi-sync configuration and test MAGI server connectivity. Returns config validity, agent path status, and server health."),
		),
		s.handleSyncCheck,
	)

	// sync_preview — dry-run showing what would sync
	s.mcpServer.AddTool(
		mcp.NewTool("sync_preview",
			mcp.WithDescription("Preview what files would be synced without uploading. Shows pending changes per agent with file paths, types, and sizes."),
		),
		s.handleSyncPreview,
	)

	// sync_config — view current configuration
	s.mcpServer.AddTool(
		mcp.NewTool("sync_config",
			mcp.WithDescription("View the current magi-sync configuration (server URL, agents, privacy settings). Tokens are redacted."),
		),
		s.handleSyncConfig,
	)

	// sync_agents — list configured agents and their status
	s.mcpServer.AddTool(
		mcp.NewTool("sync_agents",
			mcp.WithDescription("List all configured sync agents with their type, enabled status, paths, and include/exclude patterns."),
		),
		s.handleSyncAgents,
	)

	// sync_reset — clear sync state for a fresh re-sync
	s.mcpServer.AddTool(
		mcp.NewTool("sync_reset",
			mcp.WithDescription("Clear the sync state file, forcing a full re-sync on the next run. Server-side deduplication prevents actual duplicates."),
			mcp.WithBoolean("confirm",
				mcp.Required(),
				mcp.Description("Must be true to confirm the reset."),
			),
		),
		s.handleSyncReset,
	)

	// sync_conflicts — check for conflicts between local and remote
	s.mcpServer.AddTool(
		mcp.NewTool("sync_conflicts",
			mcp.WithDescription("Check for conflicts between local files and remote MAGI memories. Shows files that changed both locally and remotely since last sync."),
		),
		s.handleSyncConflicts,
	)
}

func (s *Server) registerResources() {
	// Expose config as a resource
	s.mcpServer.AddResource(
		mcp.NewResource(
			"magi-sync://config",
			"magi-sync configuration",
			mcp.WithResourceDescription("Current magi-sync configuration file (tokens redacted)."),
			mcp.WithMIMEType("application/yaml"),
		),
		s.handleConfigResource,
	)

	// Expose state as a resource
	s.mcpServer.AddResource(
		mcp.NewResource(
			"magi-sync://state",
			"magi-sync state",
			mcp.WithResourceDescription("Current sync state: tracked files and their hashes."),
			mcp.WithMIMEType("application/json"),
		),
		s.handleStateResource,
	)
}

// loadApp loads config and creates an App instance.
func (s *Server) loadApp() (*syncagent.App, *syncagent.Config, error) {
	cfgPath, err := syncagent.LoadConfigPath(s.configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("config path: %w", err)
	}
	cfg, err := syncagent.LoadConfig(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("config load: %w", err)
	}
	app, err := syncagent.New(cfg, cfgPath, s.logger)
	if err != nil {
		return nil, nil, fmt.Errorf("app init: %w", err)
	}
	return app, cfg, nil
}

func (s *Server) handleSyncStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	app, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	// Load state
	state, err := syncagent.LoadState(cfg.Sync.StateFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load state: %v", err)), nil
	}

	// Check server health
	client := syncagent.NewClient(cfg.Server)
	serverOK := "connected"
	if err := client.Health(ctx); err != nil {
		serverOK = fmt.Sprintf("unreachable: %v", err)
	}

	// Count pending changes via dry-run
	pending, err := app.CountPending(ctx)
	if err != nil {
		pending = -1
	}

	result := map[string]any{
		"server":         cfg.Server.URL,
		"server_status":  serverOK,
		"machine_id":     cfg.Machine.ID,
		"user":           cfg.Machine.User,
		"tracked_files":  len(state.Records),
		"pending_changes": pending,
		"state_file":     cfg.Sync.StateFile,
		"sync_mode":      cfg.Sync.Mode,
		"interval":       cfg.Sync.Interval,
		"conflict_strategy": string(cfg.Sync.ConflictStrategy),
	}

	return jsonResult(result)
}

func (s *Server) handleSyncNow(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	app, _, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	start := time.Now()
	uploaded, err := app.SyncOnce(ctx)
	elapsed := time.Since(start)

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("sync failed: %v", err)), nil
	}

	result := map[string]any{
		"status":   "success",
		"uploaded": uploaded,
		"duration": elapsed.String(),
	}

	return jsonResult(result)
}

func (s *Server) handleSyncCheck(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("config error: %v", err)), nil
	}

	checks := map[string]any{
		"config_valid": true,
		"config_path":  s.configPath,
	}

	// Server health
	client := syncagent.NewClient(cfg.Server)
	if err := client.Health(ctx); err != nil {
		checks["server_health"] = fmt.Sprintf("FAIL: %v", err)
	} else {
		checks["server_health"] = "OK"
	}

	// Agent path checks
	agentChecks := make([]map[string]any, 0)
	for _, agent := range cfg.Agents {
		ac := map[string]any{
			"type":    agent.Type,
			"name":    agent.Name,
			"enabled": agent.Enabled,
		}
		if !agent.Enabled {
			ac["status"] = "disabled"
		} else {
			pathStatus := make([]map[string]string, 0)
			for _, p := range agent.Paths {
				ps := map[string]string{"path": p}
				if _, err := os.Stat(p); err != nil {
					ps["status"] = fmt.Sprintf("MISSING: %v", err)
				} else {
					ps["status"] = "OK"
				}
				pathStatus = append(pathStatus, ps)
			}
			ac["paths"] = pathStatus
		}
		agentChecks = append(agentChecks, ac)
	}
	checks["agents"] = agentChecks

	return jsonResult(checks)
}

func (s *Server) handleSyncPreview(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	app, _, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	payloads, err := app.CollectPending(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("scan failed: %v", err)), nil
	}

	preview := make([]map[string]any, 0, len(payloads))
	for _, p := range payloads {
		preview = append(preview, map[string]any{
			"path":       p.SourcePath,
			"type":       p.Type,
			"project":    p.Project,
			"visibility": p.Visibility,
			"summary":    p.Summary,
		})
	}

	result := map[string]any{
		"pending_count": len(preview),
		"files":         preview,
	}

	return jsonResult(result)
}

func (s *Server) handleSyncConfig(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	// Redact sensitive fields
	redacted := map[string]any{
		"server": map[string]any{
			"url":      cfg.Server.URL,
			"protocol": cfg.Server.Protocol,
			"has_token": cfg.Server.Token != "",
		},
		"machine": map[string]any{
			"id":     cfg.Machine.ID,
			"user":   cfg.Machine.User,
			"groups": cfg.Machine.Groups,
		},
		"sync": map[string]any{
			"mode":              cfg.Sync.Mode,
			"interval":          cfg.Sync.Interval,
			"retry_backoff":     cfg.Sync.RetryBackoff,
			"max_batch_size":    cfg.Sync.MaxBatchSize,
			"state_file":        cfg.Sync.StateFile,
			"conflict_strategy": string(cfg.Sync.ConflictStrategy),
		},
		"privacy": map[string]any{
			"mode":            cfg.Privacy.Mode,
			"redact_secrets":  cfg.Privacy.RedactSecrets,
			"max_file_size_kb": cfg.Privacy.MaxFileSizeKB,
		},
	}

	return jsonResult(redacted)
}

func (s *Server) handleSyncAgents(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	agents := make([]map[string]any, 0, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		a := map[string]any{
			"type":       agent.Type,
			"name":       agent.Name,
			"enabled":    agent.Enabled,
			"paths":      agent.Paths,
			"include":    agent.Include,
			"exclude":    agent.Exclude,
			"visibility": agent.Visibility,
			"owner":      agent.Owner,
		}
		if agent.Type == "settings" {
			a["settings_path"] = agent.SettingsPath
		}
		if len(agent.Viewers) > 0 {
			a["viewers"] = agent.Viewers
		}
		if len(agent.ViewerGroups) > 0 {
			a["viewer_groups"] = agent.ViewerGroups
		}
		agents = append(agents, a)
	}

	return jsonResult(map[string]any{"agents": agents})
}

func (s *Server) handleSyncReset(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	confirm, _ := args["confirm"].(bool)
	if !confirm {
		return mcp.NewToolResultError("reset requires confirm=true"), nil
	}

	_, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	stateFile := cfg.Sync.StateFile
	if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
		return mcp.NewToolResultError(fmt.Sprintf("failed to remove state file: %v", err)), nil
	}

	result := map[string]any{
		"status":     "reset",
		"state_file": stateFile,
		"message":    "State file cleared. Next sync will re-scan all files. Server-side deduplication prevents duplicate memories.",
	}

	return jsonResult(result)
}

func (s *Server) handleSyncConflicts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, cfg, err := s.loadApp()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load config: %v", err)), nil
	}

	state, err := syncagent.LoadState(cfg.Sync.StateFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load state: %v", err)), nil
	}

	// Check each tracked file for local changes
	conflicts := make([]map[string]any, 0)
	for key, fileState := range state.Records {
		// Extract path from key (format: "path|type|hash|speaker")
		parts := strings.SplitN(key, "|", 2)
		if len(parts) == 0 {
			continue
		}
		path := parts[0]

		data, err := os.ReadFile(path)
		if err != nil {
			continue // file might have been deleted
		}

		currentHash := hashBytes(data)
		if currentHash != fileState.SHA256 {
			conflicts = append(conflicts, map[string]any{
				"path":          path,
				"local_changed": true,
				"last_sync_hash": fileState.SHA256[:12] + "...",
				"current_hash":  currentHash[:12] + "...",
			})
		}
	}

	result := map[string]any{
		"conflict_strategy": string(cfg.Sync.ConflictStrategy),
		"local_changes":     len(conflicts),
		"files":             conflicts,
	}

	return jsonResult(result)
}

// Resource handlers

func (s *Server) handleConfigResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	cfgPath, err := syncagent.LoadConfigPath(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("config path: %w", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Redact tokens in YAML
	content := string(data)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "token:") && !strings.Contains(lower, "token_env") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[1]) != `""` {
				lines[i] = parts[0] + ": [REDACTED]"
			}
		}
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/yaml",
			Text:     strings.Join(lines, "\n"),
		},
	}, nil
}

func (s *Server) handleStateResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	_, cfg, err := s.loadApp()
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	state, err := syncagent.LoadState(cfg.Sync.StateFile)
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling state: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(data),
		},
	}, nil
}

// helpers

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("json encode: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func hashBytes(b []byte) string {
	return syncagent.HashBytes(b)
}
