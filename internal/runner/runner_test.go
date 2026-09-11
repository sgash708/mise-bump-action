package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/grouping"
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
			cfg:     config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", Labels: []string{"dependencies"}},
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
			cfg:     config.Config{PRStrategy: grouping.Single, BaseBranch: "main"},
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
			cfg:  config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"},
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
			// A failure in one group must not stop later groups from being
			// attempted: with a fail-fast Run, group 3 would never run and
			// its PR would never open just because group 2 hit a transient
			// error (e.g. a rate limit). All groups must be tried, with
			// errors aggregated rather than returned on the first failure.
			name: "attempts every group even when an earlier one fails",
			cfg:  config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"},
			entries: []outdated.Entry{
				{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
				{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
				{Name: "terraform", Requested: "1.7.5", Latest: "1.14.7", RelPath: "mise.toml"},
			},
			newGitHub: func(t *testing.T) *GitHubMock {
				calls := 0
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\nterraform = \"1.7.5\"\n"), "blobsha", nil
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
			wantPRCount:   2,
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

func TestBranchName(t *testing.T) {
	tests := []struct {
		name    string
		entries []outdated.Entry
		want    string
	}{
		{
			name:    "single entry uses the full tool name, not just the last path segment",
			entries: []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Latest: "2.13.2"}},
			want:    "mise-bump/aqua-golangci-golangci-lint-2.13.2",
		},
		{
			// Two different backends can share a trailing path segment (both
			// end in "/cli"); using only the last segment would collide both
			// into "mise-bump/cli-...". The full sanitized name must not.
			name:    "different backends with the same trailing segment do not collide",
			entries: []outdated.Entry{{Name: "go:github.com/bar/cli", Latest: "1.0.0"}},
			want:    "mise-bump/go-github.com-bar-cli-1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branchName(tt.entries)
			if got != tt.want {
				t.Errorf("branchName(%+v) = %q, want %q", tt.entries, got, tt.want)
			}
		})
	}
}

func TestBranchName_GroupedEntriesAreDeterministic(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Latest: "1.27.0"},
		{Name: "node", Latest: "24.13.0"},
	}

	first := branchName(entries)
	second := branchName([]outdated.Entry{
		{Name: "go", Latest: "1.27.0"},
		{Name: "node", Latest: "24.13.0"},
	})
	if first != second {
		t.Errorf("branchName is not deterministic for the same entries: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "mise-bump/batch-") {
		t.Errorf("branchName(%+v) = %q, want a mise-bump/batch-* name for a grouped set", entries, first)
	}
}

func TestBranchNameCollision(t *testing.T) {
	aqua := branchName([]outdated.Entry{{Name: "aqua:foo/cli", Latest: "1.0.0"}})
	goInstall := branchName([]outdated.Entry{{Name: "go:github.com/bar/cli", Latest: "1.0.0"}})
	if aqua == goInstall {
		t.Errorf("branch names for different backends collided: both are %q", aqua)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "leaves safe characters alone", in: "golangci-lint2.13", want: "golangci-lint2.13"},
		{name: "replaces colon and slash", in: "aqua:foo/bar", want: "aqua-foo-bar"},
		{name: "replaces unicode", in: "café", want: "caf-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.in); got != tt.want {
				t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
