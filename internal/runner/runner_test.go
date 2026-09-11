package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestRun(t *testing.T) {
	baseContent := "[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"
	twoEntries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}

	tests := []struct {
		name          string
		cfg           config.Config
		entries       []outdated.Entry
		newGitHub     func(t *testing.T) *GitHubMock
		wantPRCount   int
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "per-tool opens one PR per entry",
			cfg:     config.Config{PRStrategy: "per-tool", BaseBranch: "main", Labels: []string{"dependencies"}},
			entries: twoEntries,
			newGitHub: func(t *testing.T) *GitHubMock {
				var openedTitles []string
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte(baseContent), "blobsha", nil
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
						openedTitles = append(openedTitles, in.PRTitle)
						for _, want := range []string{
							"chore(deps): bump go from 1.26.1 to 1.27.0",
							"chore(deps): bump node from 24.12.0 to 24.13.0",
						} {
							if in.PRTitle == want {
								return len(openedTitles), nil
							}
						}
						t.Errorf("unexpected PR title: %q", in.PRTitle)
						return len(openedTitles), nil
					},
				}
			},
			wantPRCount: 2,
		},
		{
			name:    "single bundles into one PR containing both bumps",
			cfg:     config.Config{PRStrategy: "single", BaseBranch: "main"},
			entries: twoEntries,
			newGitHub: func(t *testing.T) *GitHubMock {
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte(baseContent), "blobsha", nil
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
						content := string(in.FileContent)
						if !strings.Contains(content, "1.27.0") || !strings.Contains(content, "24.13.0") {
							t.Errorf("expected bundled file content to contain both bumped versions, got %q", content)
						}
						return 1, nil
					},
				}
			},
			wantPRCount: 1,
		},
		{
			name: "fetches enrichment for entries backed by a resolvable github repo",
			cfg:  config.Config{PRStrategy: "per-tool", BaseBranch: "main"},
			entries: []outdated.Entry{
				{Name: "aqua:golangci/golangci-lint", Requested: "2.12.2", Latest: "2.13.2", RelPath: "mise.toml"},
				{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
			},
			newGitHub: func(t *testing.T) *GitHubMock {
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte("[tools]\ngo = \"1.26.1\"\n\"aqua:golangci/golangci-lint\" = \"2.12.2\"\n"), "blobsha", nil
					},
					ReleaseNotesHTMLFunc: func(ctx context.Context, repo, from, to string) (string, bool) {
						if repo != "golangci/golangci-lint" {
							t.Errorf("ReleaseNotesHTML called with unexpected repo %q (should never be called for the \"go\" entry)", repo)
						}
						return "<details>release notes</details>", true
					},
					CommitsHTMLFunc: func(ctx context.Context, repo, from, to string) (string, bool) {
						if repo != "golangci/golangci-lint" {
							t.Errorf("CommitsHTML called with unexpected repo %q (should never be called for the \"go\" entry)", repo)
						}
						return "<details>commits</details>", true
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
						if strings.Contains(in.PRTitle, "golangci-lint") {
							if !strings.Contains(in.PRBody, "<details>release notes</details>") || !strings.Contains(in.PRBody, "<details>commits</details>") {
								t.Errorf("expected golangci-lint PR body to contain enrichment, got %q", in.PRBody)
							}
							if !strings.Contains(in.PRBody, "[golangci-lint](https://github.com/golangci/golangci-lint)") {
								t.Errorf("expected golangci-lint PR body to contain a repo link, got %q", in.PRBody)
							}
						} else {
							if strings.Contains(in.PRBody, "<details>") {
								t.Errorf("expected the \"go\" PR body to have no enrichment (not resolvable to a repo), got %q", in.PRBody)
							}
						}
						return 1, nil
					},
				}
			},
			wantPRCount: 2,
		},
		{
			name:    "returns partial results when a later PR creation fails",
			cfg:     config.Config{PRStrategy: "per-tool", BaseBranch: "main"},
			entries: twoEntries,
			newGitHub: func(t *testing.T) *GitHubMock {
				calls := 0
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte(baseContent), "blobsha", nil
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
						calls++
						if calls == 2 {
							return 0, errors.New("boom")
						}
						return calls, nil
					},
				}
			},
			wantPRCount:   1,
			wantErr:       true,
			wantErrSubstr: "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := tt.newGitHub(t)

			numbers, err := Run(context.Background(), tt.cfg, tt.entries, gh)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("expected error to contain %q, got: %v", tt.wantErrSubstr, err)
				}
			} else if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if len(numbers) != tt.wantPRCount {
				t.Fatalf("expected %d PR numbers, got %d: %+v", tt.wantPRCount, len(numbers), numbers)
			}
		})
	}
}
