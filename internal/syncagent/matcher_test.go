package syncagent

import "testing"

func TestMatchGlobPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		want    bool
	}{
		// Basic wildcards
		{"*.md", "README.md", true},
		{"*.md", "src/README.md", false},
		{"*.md", "file.txt", false},

		// ** matches zero or more directories
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/README.md", true},
		{"**/*.md", "a/b/c/README.md", true},

		// Prefix + **
		{"sessions/**/*.jsonl", "sessions/s1.jsonl", true},
		{"sessions/**/*.jsonl", "sessions/2026/s1.jsonl", true},
		{"sessions/**/*.jsonl", "sessions/a/b/c/s1.jsonl", true},
		{"sessions/**/*.jsonl", "other/s1.jsonl", false},

		// ** at end
		{"docs/**", "docs/readme.md", true},
		{"docs/**", "docs/sub/file.txt", true},

		// Exclude patterns
		{"**/tmp/**", "tmp/file.txt", true},
		{"**/tmp/**", "a/tmp/file.txt", true},
		{"**/*.bin", "model.bin", true},
		{"**/*.bin", "deep/path/model.bin", true},

		// projects/**/CLAUDE.md
		{"projects/**/CLAUDE.md", "projects/CLAUDE.md", true},
		{"projects/**/CLAUDE.md", "projects/myapp/CLAUDE.md", true},
		{"projects/**/CLAUDE.md", "projects/a/b/CLAUDE.md", true},

		// No match
		{"*.go", "main.rs", false},
		{"src/**/*.go", "lib/main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"→"+tt.target, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.target)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.target, got, tt.want)
			}
		})
	}
}

func TestShouldInclude(t *testing.T) {
	tests := []struct {
		name     string
		rel      string
		includes []string
		excludes []string
		want     bool
	}{
		{"no filters", "file.md", nil, nil, true},
		{"include match", "docs/readme.md", []string{"**/*.md"}, nil, true},
		{"include no match", "file.bin", []string{"**/*.md"}, nil, false},
		{"exclude match", "tmp/cache.md", []string{"**/*.md"}, []string{"**/tmp/**"}, false},
		{"exclude wins", "tmp/file.md", []string{"**/*.md"}, []string{"**/tmp/**"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInclude(tt.rel, tt.includes, tt.excludes)
			if got != tt.want {
				t.Errorf("shouldInclude(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}
