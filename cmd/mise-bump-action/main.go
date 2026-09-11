package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to load configuration: %w", err))
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

	if err := run(context.Background(), cfg, os.Stderr, summary, outdated.Run, gh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// lookupFunc matches outdated.Run's signature. Injecting it lets tests
// exercise run's orchestration logic without shelling out to a real mise
// binary.
type lookupFunc func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error)

func run(ctx context.Context, cfg config.Config, stderr, summary io.Writer, lookup lookupFunc, gh runner.GitHub) error {
	var allEntries []outdated.Entry
	for _, path := range cfg.MiseConfigPaths {
		entries, err := lookup(ctx, ".", path)
		if err != nil {
			return fmt.Errorf("failed to check outdated tools for %s: %w", path, err)
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 {
		_, _ = fmt.Fprintln(stderr, "no outdated mise-managed tools found")
		return nil
	}

	numbers, err := runner.Run(ctx, cfg, allEntries, gh, summary)
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
