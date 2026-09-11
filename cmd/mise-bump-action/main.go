package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/githubapi"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/runner"
)

// httpTimeout bounds every GitHub API call. Without it, http.DefaultClient
// has no timeout at all, so a hung connection would block the whole action
// indefinitely instead of failing with a clear error.
const httpTimeout = 30 * time.Second

// version is stamped at build time via -ldflags "-X main.version=...";
// release.yml sets it to the release tag. It stays "dev" for local builds.
var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "mise-bump-action %s\n", version)

	// $GITHUB_OUTPUT is how a composite action step exposes step outputs
	// (action.yml wires them up as `pr-numbers`/`opened-count`). Absent
	// outside GitHub Actions, in which case outputs are simply discarded.
	// Opened before config.FromEnv (and before any other failure path) so
	// that ADR 0015's "opened-count/pr-numbers are always set" guarantee
	// also covers a configuration error — a consumer workflow step reading
	// them under `if: always()` must see "0", never an unset/empty value,
	// regardless of which step failed.
	output := io.Writer(io.Discard)
	if path := os.Getenv("GITHUB_OUTPUT"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to open GITHUB_OUTPUT (%v); outputs will not be set\n", err)
		} else {
			defer func() { _ = f.Close() }()
			output = f
		}
	}

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to load configuration: %w", err))
		writeOutputs(output, nil)
		os.Exit(1)
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	gh := githubapi.NewClient(httpClient, cfg.APIURL, cfg.GitHubToken, cfg.Repository)

	// dry-run previews are written to $GITHUB_STEP_SUMMARY when set (any
	// composite/job step on GitHub Actions has it), so they render as
	// Markdown in the job summary UI instead of being buried in logs.
	// Falling back to stderr keeps `run` usable outside of GitHub Actions.
	summary := io.Writer(os.Stderr)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to open GITHUB_STEP_SUMMARY (%v); dry-run preview will go to stderr instead\n", err)
		} else {
			defer func() { _ = f.Close() }()
			summary = f
		}
	}

	if err := run(context.Background(), cfg, os.Stderr, summary, output, outdated.Run, gh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// lookupFunc matches outdated.Run's signature. Injecting it lets tests
// exercise run's orchestration logic without shelling out to a real mise
// binary.
type lookupFunc func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error)

func run(ctx context.Context, cfg config.Config, stderr, summary, output io.Writer, lookup lookupFunc, gh runner.GitHub) error {
	var allEntries []outdated.Entry
	for _, path := range cfg.MiseConfigPaths {
		entries, err := lookup(ctx, ".", path)
		if err != nil {
			// ADR 0015 requires opened-count/pr-numbers to always be set, so
			// a consumer workflow step reading them (e.g. under
			// `if: always()`) never sees an unset/empty value in place of "0".
			writeOutputs(output, nil)
			return fmt.Errorf("failed to check outdated tools for %s: %w", path, err)
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 {
		_, _ = fmt.Fprintln(stderr, "no outdated mise-managed tools found")
		writeOutputs(output, nil)
		return nil
	}

	numbers, err := runner.Run(ctx, cfg, allEntries, gh, summary)
	writeOutputs(output, numbers)
	if err != nil {
		return fmt.Errorf("failed to bump some outdated tools (opened %d pull request(s) successfully): %w", len(numbers), err)
	}

	if cfg.DryRun {
		_, _ = fmt.Fprintln(stderr, "[dry-run] no pull requests were created; see the job summary for what would have been opened")
		return nil
	}

	_, _ = fmt.Fprintf(stderr, "opened %d pull request(s): %v\n", len(numbers), numbers)
	return nil
}

// writeOutputs sets this action's `pr-numbers` (comma-separated) and
// `opened-count` outputs, in the `name=value` line format GITHUB_OUTPUT
// expects. Written even when Run returned a partial-failure error, so
// callers can see what did succeed.
func writeOutputs(output io.Writer, numbers []int) {
	strs := make([]string, len(numbers))
	for i, n := range numbers {
		strs[i] = strconv.Itoa(n)
	}
	_, _ = fmt.Fprintf(output, "opened-count=%d\n", len(numbers))
	_, _ = fmt.Fprintf(output, "pr-numbers=%s\n", strings.Join(strs, ","))
}
