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
// key forms, with an optional trailing inline comment (`go = "1.26.1" #
// pinned`). It handles CRLF line endings and tabs around "=". It returns an
// error if toolKey is not found, if its current value does not match
// oldVersion (guarding against a stale bump target), or if toolKey is found
// but its value isn't a simple quoted version string (e.g. an inline table
// or array — valid mise.toml, but not a form Bump can rewrite in place).
func Bump(content []byte, toolKey, oldVersion, newVersion string) ([]byte, error) {
	keyPrefix := regexp.MustCompile(`^\s*"?` + regexp.QuoteMeta(toolKey) + `"?\s*=`)
	valuePattern := regexp.MustCompile(
		`^(\s*"?` + regexp.QuoteMeta(toolKey) + `"?\s*=\s*")` +
			regexp.QuoteMeta(oldVersion) +
			`("[ \t]*(?:#.*)?\r?)$`,
	)

	lines := strings.Split(string(content), "\n")
	found := false
	keySeenWithDifferentShape := false
	for i, line := range lines {
		if m := valuePattern.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + newVersion + m[2]
			found = true
			break
		}
		if keyPrefix.MatchString(line) {
			keySeenWithDifferentShape = true
		}
	}
	if !found {
		if keySeenWithDifferentShape {
			return nil, fmt.Errorf("tool %q was found but its value is not a simple version string (inline tables and arrays are not supported)", toolKey)
		}
		return nil, fmt.Errorf("tool %q with version %q not found in mise.toml content", toolKey, oldVersion)
	}

	return []byte(strings.Join(lines, "\n")), nil
}
