package runner

import (
	"bytes"
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
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						openedTitles = append(openedTitles, in.PRTitle)
						for _, want := range []string{
							"chore(deps): bump go from 1.26.1 to 1.27.0",
							"chore(deps): bump node from 24.12.0 to 24.13.0",
						} {
							if in.PRTitle == want {
								return len(openedTitles), true, nil
							}
						}
						t.Errorf("unexpected PR title: %q", in.PRTitle)
						return len(openedTitles), true, nil
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
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						content := string(in.FileContent)
						if !strings.Contains(content, "1.27.0") || !strings.Contains(content, "24.13.0") {
							t.Errorf("expected bundled file content to contain both bumped versions, got %q", content)
						}
						return 1, true, nil
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
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
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
						return 1, true, nil
					},
				}
			},
			wantPRCount: 2,
		},
		{
			// buildGrouped only ever renders Enrichment.RepoURL (a pure string
			// derivation), never ReleaseNotesHTML/CommitsHTML — so fetching
			// those for a multi-entry group would be a wasted GitHub API call.
			name: "single strategy with multiple entries does not fetch full enrichment details",
			cfg:  config.Config{PRStrategy: grouping.Single, BaseBranch: "main"},
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
						t.Errorf("ReleaseNotesHTML should not be called for a grouped (>1 entry) bump, got repo %q", repo)
						return "", false
					},
					CommitsHTMLFunc: func(ctx context.Context, repo, from, to string) (string, bool) {
						t.Errorf("CommitsHTML should not be called for a grouped (>1 entry) bump, got repo %q", repo)
						return "", false
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						if !strings.Contains(in.PRBody, "[golangci-lint](https://github.com/golangci/golangci-lint)") {
							t.Errorf("expected grouped PR body to still contain a repo link, got %q", in.PRBody)
						}
						return 1, true, nil
					},
				}
			},
			wantPRCount: 1,
		},
		{
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
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						calls++
						if calls == 2 {
							return 0, false, errors.New("boom")
						}
						return calls, true, nil
					},
				}
			},
			wantPRCount:   2,
			wantErr:       true,
			wantErrSubstr: "boom",
		},
		{
			name:    "passes a version-independent branch prefix for a single-entry group",
			cfg:     config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"},
			entries: []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}},
			newGitHub: func(t *testing.T) *GitHubMock {
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						want := branchPrefix([]outdated.Entry{{Name: "go"}})
						if in.BranchPrefix != want {
							t.Errorf("BranchPrefix = %q, want %q", in.BranchPrefix, want)
						}
						return 1, true, nil
					},
				}
			},
			wantPRCount: 1,
		},
		{
			name:    "leaves branch prefix empty for a grouped bump",
			cfg:     config.Config{PRStrategy: grouping.Single, BaseBranch: "main"},
			entries: twoEntries,
			newGitHub: func(t *testing.T) *GitHubMock {
				return &GitHubMock{
					ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
						return []byte(baseContent), "blobsha", nil
					},
					OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
						if in.BranchPrefix != "" {
							t.Errorf("BranchPrefix = %q, want empty for a grouped bump", in.BranchPrefix)
						}
						return 1, true, nil
					},
				}
			},
			wantPRCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := tt.newGitHub(t)
			var out bytes.Buffer

			numbers, err := Run(context.Background(), tt.cfg, tt.entries, gh, &out)
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

func TestRun_SkipsClosedPreviouslyWithoutFailing(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			if strings.Contains(in.PRTitle, "go") {
				return 0, false, ErrClosedPreviously
			}
			return 5, true, nil
		},
	}
	var out bytes.Buffer

	numbers, err := Run(context.Background(), config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"}, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v, want nil (a previously-closed bump is not a failure)", err)
	}
	if len(numbers) != 1 || numbers[0] != 5 {
		t.Errorf("numbers = %+v, want [5] (the skipped bump must not appear)", numbers)
	}
	if !strings.Contains(out.String(), "previously closed") {
		t.Errorf("expected a skip notice in out, got:\n%s", out.String())
	}
}

// TestRun_DoesNotCountAnAlreadyOpenPRAsNewlyOpened guards opened-count/
// pr-numbers' meaning: rediscovering a PR that was already open from a prior
// run must not be reported as if a new PR were opened this run, or a
// workflow polling those outputs would fire the same notification every
// time it reruns against an unmerged bump.
func TestRun_DoesNotCountAnAlreadyOpenPRAsNewlyOpened(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
	}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			return 42, false, nil // already open; nothing newly created
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 0 {
		t.Errorf("expected an already-open PR not to be reported as newly opened, got %+v", numbers)
	}
}

// TestRun_PassesLegacyBranchNamesForASingleEntryGroup guards against a
// branch-naming scheme change (as happened between v1.4.0, v1.5.0, and this
// version) silently forgetting PRs opened under a previous scheme: it must
// still recognize them as idempotency candidates (ADR 0017).
func TestRun_PassesLegacyBranchNamesForASingleEntryGroup(t *testing.T) {
	entries := []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			want := legacyBranchNames([]outdated.Entry{{Name: "go", Latest: "1.27.0"}})
			if len(in.LegacyBranchNames) != len(want) {
				t.Fatalf("LegacyBranchNames = %+v, want %+v", in.LegacyBranchNames, want)
			}
			for i := range want {
				if in.LegacyBranchNames[i] != want[i] {
					t.Errorf("LegacyBranchNames[%d] = %q, want %q", i, in.LegacyBranchNames[i], want[i])
				}
			}
			if in.LegacyMatchName != "go" {
				t.Errorf("LegacyMatchName = %q, want %q", in.LegacyMatchName, "go")
			}
			if in.LegacyMatchVersion != "1.27.0" {
				t.Errorf("LegacyMatchVersion = %q, want %q", in.LegacyMatchVersion, "1.27.0")
			}
			return 1, true, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"}
	if _, err := Run(context.Background(), cfg, entries, gh, &out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRun_FiltersIgnoredEntries(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "terraform", Requested: "1.7.5", Latest: "1.14.7", RelPath: "mise.toml"},
		{Name: "aqua:foo/bar", Requested: "1.0.0", Latest: "1.1.0", RelPath: "mise.toml"},
	}
	var openedNames []string
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nterraform = \"1.7.5\"\n\"aqua:foo/bar\" = \"1.0.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			openedNames = append(openedNames, in.PRTitle)
			return 1, true, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", Ignore: []string{"terraform", "aqua:foo/*"}}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 1 {
		t.Fatalf("expected 1 PR (only \"go\" is not ignored), got %d: %+v", len(numbers), numbers)
	}
	if len(openedNames) != 1 || !strings.Contains(openedNames[0], "go") {
		t.Errorf("expected only the \"go\" PR to open, got %+v", openedNames)
	}
	for _, want := range []string{"terraform", "aqua:foo/bar"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected an [ignored] notice mentioning %q, got:\n%s", want, out.String())
		}
	}
}

func TestRun_StopsOpeningPRsAtMaxOpenPRs(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		CountOpenBumpPRsFunc: func(ctx context.Context, base string) (int, error) {
			return 2, nil
		},
		HasOpenPRWithPrefixFunc: func(ctx context.Context, base, prefix string) (bool, error) {
			return false, nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			t.Fatal("OpenBumpPR must not be called once max-open-prs is already reached")
			return 0, false, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", MaxOpenPRs: 2}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 0 {
		t.Errorf("expected no new PRs once the cap is reached, got %+v", numbers)
	}
	if !strings.Contains(out.String(), "max-open-prs") {
		t.Errorf("expected a max-open-prs skip notice, got:\n%s", out.String())
	}
}

func TestRun_MaxOpenPRsStopsPartway(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
		{Name: "terraform", Requested: "1.7.5", Latest: "1.14.7", RelPath: "mise.toml"},
	}
	opened := 0
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\nterraform = \"1.7.5\"\n"), "blobsha", nil
		},
		CountOpenBumpPRsFunc: func(ctx context.Context, base string) (int, error) {
			return 1, nil
		},
		HasOpenPRWithPrefixFunc: func(ctx context.Context, base, prefix string) (bool, error) {
			return false, nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			opened++
			return opened, true, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", MaxOpenPRs: 2}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Already at 1 with a cap of 2: only 1 more group may open before the cap
	// (1 existing + 1 new = 2) is reached, leaving the 3rd group skipped.
	if len(numbers) != 1 {
		t.Errorf("expected exactly 1 new PR before hitting the cap, got %+v", numbers)
	}
}

// TestRun_ReplacingAStaleBumpBypassesAnAlreadySaturatedCap guards against a
// deadlock: if a stale PR could only ever be closed by its replacement
// succeeding, but max-open-prs never let that replacement through while the
// stale PR still occupied a slot, the cap would never recover on its own.
func TestRun_ReplacingAStaleBumpBypassesAnAlreadySaturatedCap(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	var openedBranches []string
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		CountOpenBumpPRsFunc: func(ctx context.Context, base string) (int, error) {
			return 1, nil
		},
		HasOpenPRWithPrefixFunc: func(ctx context.Context, base, prefix string) (bool, error) {
			return prefix == branchPrefix([]outdated.Entry{{Name: "go"}}), nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			openedBranches = append(openedBranches, in.BranchName)
			return 1, true, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", MaxOpenPRs: 1}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wantBranch := branchName([]outdated.Entry{{Name: "go", Latest: "1.27.0"}})
	if len(numbers) != 1 || len(openedBranches) != 1 || openedBranches[0] != wantBranch {
		t.Errorf("expected only go's replacement bump to open despite an already-saturated cap, got numbers=%+v branches=%+v", numbers, openedBranches)
	}
	if !strings.Contains(out.String(), "max-open-prs") {
		t.Errorf("expected node to still be skipped with a max-open-prs notice, got:\n%s", out.String())
	}
}

func TestRun_SkipsEntryWithUnsupportedValueFormButBumpsTheRest(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "python", Requested: "3.11", Latest: "3.12", RelPath: "mise.toml"},
	}
	var prBody string
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\npython = { version = \"3.11\" }\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			prBody = in.PRBody
			return 1, true, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.Single, BaseBranch: "main"}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v, want nil (an unsupported value form must not fail the whole group)", err)
	}
	if len(numbers) != 1 {
		t.Fatalf("expected 1 PR containing the still-bumpable \"go\" entry, got %+v", numbers)
	}
	if strings.Contains(prBody, "python") {
		t.Errorf("expected the skipped \"python\" entry to be excluded from the PR body, got %q", prBody)
	}
	if !strings.Contains(out.String(), "python") || !strings.Contains(out.String(), "can't rewrite") {
		t.Errorf("expected a skip notice mentioning \"python\", got:\n%s", out.String())
	}
}

func TestRun_SkipsWholeGroupWhenEveryEntryHasAnUnsupportedValueForm(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "python", Requested: "3.11", Latest: "3.12", RelPath: "mise.toml"},
	}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\npython = { version = \"3.11\" }\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			t.Fatal("OpenBumpPR must not be called when every entry in the group is unsupported")
			return 0, false, nil
		},
	}
	var out bytes.Buffer

	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main"}
	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 0 {
		t.Errorf("expected no PRs, got %+v", numbers)
	}
}

func TestRun_DryRun(t *testing.T) {
	cfg := config.Config{PRStrategy: grouping.PerTool, BaseBranch: "main", DryRun: true}
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
	}
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, bool, error) {
			t.Fatal("OpenBumpPR must not be called in dry-run mode")
			return 0, false, nil
		},
	}
	var out bytes.Buffer

	numbers, err := Run(context.Background(), cfg, entries, gh, &out)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 0 {
		t.Errorf("expected no PR numbers in dry-run mode, got %+v", numbers)
	}

	preview := out.String()
	for _, want := range []string{
		branchName([]outdated.Entry{{Name: "go", Latest: "1.27.0"}}),
		"chore(deps): bump go from 1.26.1 to 1.27.0",
		`-go = "1.26.1"`,
		`+go = "1.27.0"`,
	} {
		if !strings.Contains(preview, want) {
			t.Errorf("dry-run preview missing %q, got:\n%s", want, preview)
		}
	}
}

func TestLineDiff(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
		want   string
	}{
		{
			name:   "no change yields empty diff",
			before: "[tools]\ngo = \"1.26.1\"\n",
			after:  "[tools]\ngo = \"1.26.1\"\n",
			want:   "",
		},
		{
			name:   "single changed line",
			before: "[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n",
			after:  "[tools]\ngo = \"1.27.0\"\nnode = \"24.12.0\"\n",
			want:   "-go = \"1.26.1\"\n+go = \"1.27.0\"\n",
		},
		{
			name:   "multiple changed lines",
			before: "go = \"1.26.1\"\nnode = \"24.12.0\"\n",
			after:  "go = \"1.27.0\"\nnode = \"24.13.0\"\n",
			want:   "-go = \"1.26.1\"\n+go = \"1.27.0\"\n-node = \"24.12.0\"\n+node = \"24.13.0\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lineDiff([]byte(tt.before), []byte(tt.after))
			if got != tt.want {
				t.Errorf("lineDiff(%q, %q) = %q, want %q", tt.before, tt.after, got, tt.want)
			}
		})
	}
}

func TestBranchName(t *testing.T) {
	tests := []struct {
		name        string
		entries     []outdated.Entry
		wantContain string
	}{
		{
			name:        "single entry uses the full tool name, not just the last path segment",
			entries:     []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Latest: "2.13.2"}},
			wantContain: "aqua-golangci-golangci-lint",
		},
		{
			// Two different backends can share a trailing path segment (both
			// end in "/cli"); using only the last segment would collide both
			// into "mise-bump/cli-...". The full sanitized name must not.
			name:        "different backends with the same trailing segment do not collide",
			entries:     []outdated.Entry{{Name: "go:github.com/bar/cli", Latest: "1.0.0"}},
			wantContain: "go-github.com-bar-cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branchName(tt.entries)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("branchName(%+v) = %q, want it to contain %q", tt.entries, got, tt.wantContain)
			}
			wantSuffix := "_" + sanitize(tt.entries[0].Latest)
			if !strings.HasSuffix(got, wantSuffix) {
				t.Errorf("branchName(%+v) = %q, want it to end with %q", tt.entries, got, wantSuffix)
			}
		})
	}
}

// TestBranchNameDoesNotCollideWhenSanitizeIsLossy guards against sanitize's
// lossiness itself causing a collision: ':', '/', and literal '-' all map to
// '-', so two different tool names can sanitize to an identical string.
func TestBranchNameDoesNotCollideWhenSanitizeIsLossy(t *testing.T) {
	a := "go:github.com/foo/bar"
	b := "go:github.com/foo-bar"
	if sanitize(a) != sanitize(b) {
		t.Fatalf("test premise violated: sanitize(%q)=%q and sanitize(%q)=%q are no longer equal", a, sanitize(a), b, sanitize(b))
	}
	nameA := branchName([]outdated.Entry{{Name: a, Latest: "1.0.0"}})
	nameB := branchName([]outdated.Entry{{Name: b, Latest: "1.0.0"}})
	if nameA == nameB {
		t.Errorf("branch names for different tool names collided: both are %q", nameA)
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

func TestBranchPrefixDoesNotMatchAnUnrelatedToolSharingAPrefix(t *testing.T) {
	// "go" and "go:github.com/matryer/moq" both sanitize to strings starting
	// with "go": without an unambiguous separator, go's branchPrefix would
	// wrongly match moq's branch name too, causing closeSupersededPRs to
	// close an unrelated tool's PR.
	goPrefix := branchPrefix([]outdated.Entry{{Name: "go", Latest: "1.27.0"}})
	moqBranch := branchName([]outdated.Entry{{Name: "go:github.com/matryer/moq", Latest: "v0.7.1"}})
	if strings.HasPrefix(moqBranch, goPrefix) {
		t.Errorf("go's branchPrefix %q wrongly matches an unrelated tool's branch %q", goPrefix, moqBranch)
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
