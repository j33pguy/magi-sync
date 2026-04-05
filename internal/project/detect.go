package project

import (
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// DetectProject returns the canonical project key for a directory.
// It attempts to read the git origin remote and falls back to the
// directory basename when git metadata is unavailable.
func DetectProject(dir string) string {
	remote := gitOrigin(dir)
	if key := parseRemote(remote); key != "" {
		return key
	}

	if dir == "" {
		dir = "."
	}
	return filepath.Base(dir)
}

func gitOrigin(dir string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RepoTag returns a compact repo tag from a project key detected via git remote.
// For GitHub repos it returns "ghrepo:owner/repo", for GitLab "glrepo:owner/repo",
// and for other hosts "repo:host/owner/repo". Returns "" if the key has no host component.
func RepoTag(projectKey string) string {
	if projectKey == "" {
		return ""
	}
	parts := strings.SplitN(projectKey, "/", 2)
	if len(parts) < 2 {
		return "" // no host component (bare directory name)
	}
	host := parts[0]
	path := parts[1]
	if path == "" {
		return ""
	}
	switch {
	case strings.Contains(host, "github"):
		return "ghrepo:" + path
	case strings.Contains(host, "gitlab"):
		return "glrepo:" + path
	default:
		return "repo:" + projectKey
	}
}

func parseRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	remote = strings.TrimSuffix(remote, "/")
	remote = strings.TrimSuffix(remote, ".git")

	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err == nil && u.Host != "" {
			path := strings.TrimPrefix(u.Path, "/")
			if path == "" {
				return ""
			}
			return u.Host + "/" + path
		}
	}

	if strings.Contains(remote, ":") {
		parts := strings.SplitN(remote, ":", 2)
		if len(parts) == 2 {
			hostPart := parts[0]
			path := parts[1]
			if at := strings.LastIndex(hostPart, "@"); at != -1 {
				hostPart = hostPart[at+1:]
			}
			path = strings.TrimPrefix(path, "/")
			if hostPart != "" && path != "" {
				return hostPart + "/" + path
			}
		}
	}

	if strings.Contains(remote, "/") {
		return remote
	}

	return ""
}
