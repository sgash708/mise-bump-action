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
	if err := run(context.Background(), os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	var allEntries []outdated.Entry
	for _, path := range cfg.MiseConfigPaths {
		entries, err := outdated.Run(ctx, ".", filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("failed to check outdated tools for %s: %w", path, err)
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 {
		_, _ = fmt.Fprintln(stderr, "no outdated mise-managed tools found")
		return nil
	}

	gh := githubapi.NewClient(http.DefaultClient, cfg.APIURL, cfg.GitHubToken, cfg.Repository)

	numbers, err := runner.Run(ctx, cfg, allEntries, gh)
	if err != nil {
		return fmt.Errorf("failed to bump outdated tools (opened %d pull requests before failing): %w", len(numbers), err)
	}

	_, _ = fmt.Fprintf(stderr, "opened %d pull request(s): %v\n", len(numbers), numbers)
	return nil
}
