package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/grouping"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/runner"
)

func TestRun(t *testing.T) {
	validCfg := config.Config{
		MiseConfigPaths: []string{"mise.toml"},
		PRStrategy:      grouping.PerTool,
		GitHubToken:     "tok",
		Repository:      "sgash708/example",
		BaseBranch:      "main",
	}

	tests := []struct {
		name             string
		cfg              config.Config
		lookup           lookupFunc
		gh               runner.GitHub
		wantErr          bool
		wantErrSubstr    string
		wantStderrSubstr string
	}{
		{
			name: "reports no outdated tools without calling github",
			cfg:  validCfg,
			lookup: func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error) {
				return nil, nil
			},
			gh: &runner.GitHubMock{
				OpenBumpPRFunc: func(ctx context.Context, in runner.BumpPRInput) (int, error) {
					t.Fatal("OpenBumpPR should not be called when nothing is outdated")
					return 0, nil
				},
			},
			wantStderrSubstr: "no outdated mise-managed tools found",
		},
		{
			name: "opens PRs for outdated tools",
			cfg:  validCfg,
			lookup: func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error) {
				return []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}, nil
			},
			gh: &runner.GitHubMock{
				ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
					return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
				},
				OpenBumpPRFunc: func(ctx context.Context, in runner.BumpPRInput) (int, error) {
					return 7, nil
				},
			},
			wantStderrSubstr: "opened 1 pull request(s)",
		},
		{
			name: "propagates lookup error",
			cfg:  validCfg,
			lookup: func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error) {
				return nil, errors.New("mise not trusted")
			},
			gh:            &runner.GitHubMock{},
			wantErr:       true,
			wantErrSubstr: "mise not trusted",
		},
		{
			name: "dry-run does not open PRs and reports via stderr",
			cfg: config.Config{
				MiseConfigPaths: []string{"mise.toml"},
				PRStrategy:      grouping.PerTool,
				GitHubToken:     "tok",
				Repository:      "sgash708/example",
				BaseBranch:      "main",
				DryRun:          true,
			},
			lookup: func(ctx context.Context, repoRoot, configPath string) ([]outdated.Entry, error) {
				return []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}, nil
			},
			gh: &runner.GitHubMock{
				ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
					return []byte("[tools]\ngo = \"1.26.1\"\n"), "blobsha", nil
				},
				OpenBumpPRFunc: func(ctx context.Context, in runner.BumpPRInput) (int, error) {
					t.Fatal("OpenBumpPR should not be called in dry-run mode")
					return 0, nil
				},
			},
			wantStderrSubstr: "[dry-run] no pull requests were created",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr, summary bytes.Buffer

			err := run(context.Background(), tt.cfg, &stderr, &summary, tt.lookup, tt.gh)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("expected error to contain %q, got: %v", tt.wantErrSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("run returned error: %v", err)
			}
			if tt.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tt.wantStderrSubstr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderrSubstr)
			}
		})
	}
}
