package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestRun_PerToolOpensOnePRPerEntry(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "per-tool", BaseBranch: "main", Labels: []string{"dependencies"}}

	var openedTitles []string
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			openedTitles = append(openedTitles, in.PRTitle)
			return len(openedTitles), nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 2 {
		t.Fatalf("expected 2 PR numbers, got %d: %+v", len(numbers), numbers)
	}
	wantTitles := []string{
		"chore(deps): bump go from 1.26.1 to 1.27.0",
		"chore(deps): bump node from 24.12.0 to 24.13.0",
	}
	for _, want := range wantTitles {
		found := false
		for _, got := range openedTitles {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a PR titled %q, got titles %+v", want, openedTitles)
		}
	}
}

func TestRun_SingleBundlesIntoOnePR(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "single", BaseBranch: "main"}

	callCount := 0
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			callCount++
			content := string(in.FileContent)
			if !contains(content, "1.27.0") || !contains(content, "24.13.0") {
				t.Errorf("expected bundled file content to contain both bumped versions, got %q", content)
			}
			return 1, nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 1 || callCount != 1 {
		t.Fatalf("expected exactly 1 PR to be opened, got %d (callCount=%d)", len(numbers), callCount)
	}
}

func TestRun_ReturnsPartialResultsOnError(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "per-tool", BaseBranch: "main"}

	calls := 0
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			calls++
			if calls == 2 {
				return 0, errors.New("boom")
			}
			return calls, nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err == nil {
		t.Fatal("expected an error from the second PR creation, got nil")
	}
	if len(numbers) != 1 {
		t.Fatalf("expected the first successful PR number to be returned, got %+v", numbers)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
