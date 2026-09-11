// Package outdated runs `mise outdated --json` and reports tools whose pinned
// version differs from the latest version mise can resolve.
package outdated

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is a single mise-managed tool whose pinned version is behind the
// latest version mise resolved for it.
type Entry struct {
	Name      string
	Requested string
	Latest    string
	RelPath   string
}

type rawSource struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type rawEntry struct {
	Name      string    `json:"name"`
	Requested string    `json:"requested"`
	Latest    string    `json:"latest"`
	Source    rawSource `json:"source"`
}

// Run executes `mise outdated --json --bump -C configDir` and parses its
// output. `--bump` is required even for exact version pins: without it, mise
// only reports newer versions within the same version "family" as the pin
// (e.g. pinning "2.12.2" only surfaces newer 2.12.x patches, never 2.13.0),
// so a plain `mise outdated` silently misses most real upgrades. mise itself
// silently omits tools it cannot resolve (e.g. due to network errors or
// known go-install backend limitations), so a successful Run only reports
// tools mise could actually check.
func Run(ctx context.Context, repoRoot, configDir string) ([]Entry, error) {
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo root %q: %w", repoRoot, err)
	}

	cmd := exec.CommandContext(ctx, "mise", "outdated", "--json", "--bump", "-C", configDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run mise outdated in %q (stderr: %s): %w", configDir, stderr.String(), err)
	}

	return Parse(stdout.Bytes(), repoRootAbs)
}

// Parse extracts outdated entries from the raw JSON produced by `mise
// outdated --json`. repoRootAbs must be an absolute path; each entry's
// RelPath is computed relative to it.
func Parse(jsonBytes []byte, repoRootAbs string) ([]Entry, error) {
	var raw map[string]rawEntry
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mise outdated json: %w", err)
	}

	entries := make([]Entry, 0, len(raw))
	for _, e := range raw {
		requestedNorm := normalizeVersion(e.Requested)
		latestNorm := normalizeVersion(e.Latest)
		if requestedNorm == latestNorm {
			continue
		}

		latest := e.Latest
		if strings.HasPrefix(e.Requested, "v") && !strings.HasPrefix(e.Latest, "v") {
			latest = "v" + e.Latest
		}

		relPath, err := filepath.Rel(repoRootAbs, e.Source.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to compute relative path for %q: %w", e.Name, err)
		}

		entries = append(entries, Entry{
			Name:      e.Name,
			Requested: e.Requested,
			Latest:    latest,
			RelPath:   relPath,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// normalizeVersion strips a leading "v" so that go-install-backend versions
// (which mise reports with an inconsistent "v" prefix between requested and
// latest) compare equal when they represent the same version.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
