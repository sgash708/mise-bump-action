package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/githubapi"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/runner"
)

func main() {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to load configuration: %w", err))
		os.Exit(1)
	}
	gh := githubapi.NewClient(http.DefaultClient, cfg.APIURL, cfg.GitHubToken, cfg.Repository)

	if err := run(context.Background(), os.Getenv, os.Stderr, outdated.Run, gh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// lookupFunc matches outdated.Run's signature. Injecting it lets tests
// exercise run's orchestration logic without shelling out to a real mise
// binary.
type lookupFunc func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error)

func run(ctx context.Context, getenv func(string) string, stderr io.Writer, lookup lookupFunc, gh runner.GitHub) error {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	var allEntries []outdated.Entry
	for _, path := range cfg.MiseConfigPaths {
		entries, err := lookup(ctx, ".", filepath.Dir(path))
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
		return fmt.Errorf("failed to bump outdated tools (opened %d pull requests before failing): %w", len(numbers), err)
	}

	_, _ = fmt.Fprintf(stderr, "opened %d pull request(s): %v\n", len(numbers), numbers)
	return nil
}
