// Package reponame derives a GitHub "owner/repo" slug from a mise tool
// identifier, when the tool is unambiguously backed by a single GitHub repo.
package reponame

import "strings"

// FromToolName returns the GitHub "owner/repo" slug for name, and false if
// name isn't backed by a single resolvable GitHub repo (e.g. core tools like
// "go" or "node", or go-install tools hosted outside github.com).
func FromToolName(name string) (string, bool) {
	switch {
	case strings.HasPrefix(name, "aqua:"):
		repo := strings.TrimPrefix(name, "aqua:")
		if strings.Count(repo, "/") != 1 {
			return "", false
		}
		return repo, true
	case strings.HasPrefix(name, "go:github.com/"):
		rest := strings.TrimPrefix(name, "go:github.com/")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", false
		}
		return parts[0] + "/" + parts[1], true
	default:
		return "", false
	}
}
