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

func main() {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to load configuration: %w", err))
		os.Exit(1)
	}
	httpClient := &http.Client{Timeout: httpTimeout}
	gh := githubapi.NewClient(httpClient, cfg.APIURL, cfg.GitHubToken, cfg.Repository)

	if err := run(context.Background(), cfg, os.Stderr, outdated.Run, gh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// lookupFunc matches outdated.Run's signature. Injecting it lets tests
// exercise run's orchestration logic without shelling out to a real mise
// binary.
type lookupFunc func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error)

func run(ctx context.Context, cfg config.Config, stderr io.Writer, lookup lookupFunc, gh runner.GitHub) error {
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

	numbers, err := runner.Run(ctx, cfg, allEntries, gh)
	if err != nil {
		return fmt.Errorf("failed to bump some outdated tools (opened %d pull request(s) successfully): %w", len(numbers), err)
	}

	_, _ = fmt.Fprintf(stderr, "opened %d pull request(s): %v\n", len(numbers), numbers)
	return nil
}
