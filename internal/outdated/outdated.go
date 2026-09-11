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
// version mise-bump-action will bump it to.
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
	Bump      string    `json:"bump"`
	Latest    string    `json:"latest"`
	Source    rawSource `json:"source"`
}

// Run executes `mise outdated --json --bump -C <dir of configPath>` and
// parses its output for entries sourced from configPath specifically.
// `--bump` is required even for exact version pins: without it, mise only
// reports newer versions within the same version "family" as the pin (e.g.
// pinning "2.12.2" only surfaces newer 2.12.x patches, never 2.13.0), so a
// plain `mise outdated` silently misses most real upgrades. mise itself
// silently omits tools it cannot resolve (e.g. due to network errors or
// known go-install backend limitations), so a successful Run only reports
// tools mise could actually check.
func Run(ctx context.Context, repoRoot, configPath string) ([]Entry, error) {
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo root %q: %w", repoRoot, err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(repoRootAbs, configPath))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path %q: %w", configPath, err)
	}

	cmd := exec.CommandContext(ctx, "mise", "outdated", "--json", "--bump", "-C", filepath.Dir(configPath))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run mise outdated in %q (stderr: %s): %w", filepath.Dir(configPath), stderr.String(), err)
	}

	return Parse(stdout.Bytes(), repoRootAbs, targetAbs)
}

// Parse extracts outdated entries from the raw JSON produced by `mise
// outdated --json --bump`. repoRootAbs and targetConfigAbs must be absolute
// paths. Only entries whose source file is exactly targetConfigAbs are
// reported: mise merges mise.toml files from parent directories and the
// global config into the same result, and those must not leak into a bump
// run scoped to a single mise-config-path. Each reported entry's RelPath is
// computed relative to repoRootAbs.
func Parse(jsonBytes []byte, repoRootAbs, targetConfigAbs string) ([]Entry, error) {
	var raw map[string]rawEntry
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mise outdated json: %w", err)
	}

	targetConfigAbs = filepath.Clean(targetConfigAbs)

	entries := make([]Entry, 0, len(raw))
	for _, e := range raw {
		if filepath.Clean(e.Source.Path) != targetConfigAbs {
			continue
		}

		// Prefer "bump" (the next version respecting the pin's own
		// constraint, e.g. a fuzzy "2.12" pin stays within 2.12.x) over
		// "latest" (the unconstrained newest release), falling back to
		// "latest" only when mise didn't compute a bump target.
		target := e.Bump
		if target == "" {
			target = e.Latest
		}

		requestedNorm := normalizeVersion(e.Requested)
		targetNorm := normalizeVersion(target)
		if requestedNorm == targetNorm {
			continue
		}

		latest := target
		if strings.HasPrefix(e.Requested, "v") && !strings.HasPrefix(target, "v") {
			latest = "v" + target
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
// resolved target) compare equal when they represent the same version.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
