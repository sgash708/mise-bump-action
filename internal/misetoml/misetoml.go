// Package misetoml applies targeted version bumps to mise.toml content
// without a full TOML round-trip, so comments and formatting elsewhere in the
// file are preserved byte-for-byte.
package misetoml

import (
	"fmt"
	"regexp"
	"strings"
)

// Bump replaces the pinned version for toolKey in content, matching both
// plain (`go = "1.26.1"`) and quoted/prefixed (`"aqua:owner/repo" = "1.0.0"`)
// key forms. It returns an error if toolKey is not found, or if its current
// value does not match oldVersion (guarding against a stale bump target).
func Bump(content []byte, toolKey, oldVersion, newVersion string) ([]byte, error) {
	pattern := regexp.MustCompile(
		`^(\s*"?` + regexp.QuoteMeta(toolKey) + `"?\s*=\s*")` +
			regexp.QuoteMeta(oldVersion) +
			`("\s*)$`,
	)

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if m := pattern.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + newVersion + m[2]
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("tool %q with version %q not found in mise.toml content", toolKey, oldVersion)
	}

	return []byte(strings.Join(lines, "\n")), nil
}
