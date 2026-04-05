package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/j33pguy/magi-sync/internal/mcpserver"
	"github.com/j33pguy/magi-sync/internal/syncagent"
)

// Set via -ldflags at build time
var version = "dev"

const usage = `magi-sync — cross-machine memory sync agent for MAGI

Usage:
  magi-sync [flags] <command>

Commands:
  init        Interactive setup wizard (creates config)
  enroll      Register this machine with the MAGI server
  check       Validate config and test server connectivity
  dry-run     Preview what would sync (no upload)
  once        Sync once and exit
  run         Sync on a repeating interval (default 30s)
  watch       Real-time sync via filesystem events (recommended)

Flags:
  --config <path>   Config file path (default: ~/.config/magi-sync/config.yaml)
  --mcp             Run as MCP server over stdin/stdout
  --version         Print version and exit
  --help            Show this help

Examples:
  magi-sync init                         # first-time setup
  magi-sync check                        # validate everything
  magi-sync once                         # one-shot sync
  magi-sync watch                        # continuous sync (production)
  magi-sync --mcp                        # MCP server for AI agents
  magi-sync --config /path/to/cfg watch  # custom config location

Docs: https://github.com/j33pguy/magi-sync/wiki
`

func main() {
	configPath := flag.String("config", syncagent.DefaultConfigPath(), "Path to magi-sync config file")
	mcpOnly := flag.Bool("mcp", false, "Run as MCP server over stdin/stdout")
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("magi-sync %s\n", version)
		return
	}

	// MCP mode — run as Model Context Protocol server
	if *mcpOnly {
		logger := syncagent.NewLogger()
		srv := mcpserver.New(*configPath, logger)
		if err := srv.ServeStdio(); err != nil {
			fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	mode := syncagent.ModeOnce
	if flag.NArg() > 0 {
		mode = syncagent.Mode(flag.Arg(0))
	} else if flag.NArg() == 0 {
		// No command given — show help
		flag.Usage()
		os.Exit(0)
	}

	// Init mode runs before config loading (config may not exist yet)
	if mode == syncagent.ModeInit {
		logger := syncagent.NewLogger()
		if err := syncagent.RunInit(*configPath, logger); err != nil {
			fmt.Fprintf(os.Stderr, "init error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	validModes := map[syncagent.Mode]bool{
		syncagent.ModeEnroll: true,
		syncagent.ModeCheck:  true,
		syncagent.ModeDryRun: true,
		syncagent.ModeOnce:   true,
		syncagent.ModeRun:    true,
		syncagent.ModeWatch:  true,
	}
	if !validModes[mode] {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", mode)
		flag.Usage()
		os.Exit(1)
	}

	cfgPath, err := syncagent.LoadConfigPath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config path error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := syncagent.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nRun 'magi-sync init' to create a config file.\n")
		os.Exit(1)
	}

	app, err := syncagent.New(cfg, cfgPath, syncagent.NewLogger())
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := app.Run(ctx, mode); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "run error: %v\n", err)
		os.Exit(1)
	}
}
