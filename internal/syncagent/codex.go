package syncagent

import (
	"io/fs"
	"path/filepath"
	"strings"
)

type codexAdapter struct{}

// Scan reads Codex session files (JSONL) and produces payloads.
// Codex stores sessions under ~/.codex/sessions/ as JSONL files,
// using the same message format as Claude (role + content).
func (codexAdapter) Scan(cfg *Config, agent AgentConfig, privacy PrivacyConfig) ([]Payload, error) {
	var payloads []Payload
	maxKB := privacy.MaxFileSizeKB
	if maxKB <= 0 {
		maxKB = 512
	}
	maxBytes := maxKB * 1024

	for _, root := range agent.Paths {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			if !shouldInclude(rel, agent.Include, agent.Exclude) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Size() > maxBytes {
				return nil
			}

			switch {
			case strings.HasSuffix(strings.ToLower(path), ".jsonl"):
				// Reuse the Claude JSONL parser — Codex uses a compatible format
				ps := jsonlPayloads(cfg, path, agent, privacy)
				// Override source tag to identify as codex
				for i := range ps {
					ps[i].Source = "magi-sync"
					// Fix tags to say codex, not claude
					for j, tag := range ps[i].Tags {
						if tag == "source:claude_sync" {
							ps[i].Tags[j] = "source:codex_sync"
						}
					}
				}
				payloads = append(payloads, ps...)
			case strings.HasSuffix(strings.ToLower(path), ".md"):
				p, ok := markdownPayload(cfg, path, agent, privacy)
				if ok {
					payloads = append(payloads, p)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return payloads, nil
}
