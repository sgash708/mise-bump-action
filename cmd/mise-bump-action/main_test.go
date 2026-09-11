package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/runner"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestRun_ReturnsErrorWhenConfigInvalid(t *testing.T) {
	var stderr bytes.Buffer
	lookup := func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
		t.Fatal("lookup should not be called when config is invalid")
		return nil, nil
	}
	err := run(context.Background(), func(string) string { return "" }, &stderr, lookup, &runner.GitHubMock{})
	if err == nil {
		t.Fatal("expected an error when required env vars are missing, got nil")
	}
}

func TestRun_ReportsNoOutdatedToolsWithoutCallingGitHub(t *testing.T) {
	var stderr bytes.Buffer
	env := fakeEnv(map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	})
	lookup := func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
		return nil, nil
	}
	gh := &runner.GitHubMock{
		OpenBumpPRFunc: func(ctx context.Context, in runner.BumpPRInput) (int, error) {
			t.Fatal("OpenBumpPR should not be called when nothing is outdated")
			return 0, nil
		},
	}

	err := run(context.Background(), env, &stderr, lookup, gh)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "no outdated mise-managed tools found") {
		t.Errorf("stderr = %q, want a message about no outdated tools", stderr.String())
	}
}

func TestRun_OpensPRsForOutdatedTools(t *testing.T) {
	var stderr bytes.Buffer
	env := fakeEnv(map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	})
	lookup := func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
		return []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}, nil
	}
	gh := &runner.GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in runner.BumpPRInput) (int, error) {
			return 7, nil
		},
	}

	err := run(context.Background(), env, &stderr, lookup, gh)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "opened 1 pull request(s)") {
		t.Errorf("stderr = %q, want a summary of opened pull requests", stderr.String())
	}
}

func TestRun_PropagatesLookupError(t *testing.T) {
	var stderr bytes.Buffer
	env := fakeEnv(map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	})
	lookup := func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
		return nil, errors.New("mise not trusted")
	}

	err := run(context.Background(), env, &stderr, lookup, &runner.GitHubMock{})
	if err == nil || !strings.Contains(err.Error(), "mise not trusted") {
		t.Errorf("expected the lookup error to propagate, got: %v", err)
	}
}
