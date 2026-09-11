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

func TestRun(t *testing.T) {
	validEnv := map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	}

	tests := []struct {
		name              string
		env               map[string]string
		lookup            lookupFunc
		gh                runner.GitHub
		wantErr           bool
		wantErrSubstr     string
		wantStderrSubstr  string
		lookupMustNotCall bool
	}{
		{
			name:              "returns error when config invalid",
			env:               nil,
			lookup:            func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) { return nil, nil },
			gh:                &runner.GitHubMock{},
			wantErr:           true,
			lookupMustNotCall: true,
		},
		{
			name: "reports no outdated tools without calling github",
			env:  validEnv,
			lookup: func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
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
			env:  validEnv,
			lookup: func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
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
			env:  validEnv,
			lookup: func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
				return nil, errors.New("mise not trusted")
			},
			gh:            &runner.GitHubMock{},
			wantErr:       true,
			wantErrSubstr: "mise not trusted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			lookup := tt.lookup
			if tt.lookupMustNotCall {
				lookup = func(ctx context.Context, repoRoot, configDir string) ([]outdated.Entry, error) {
					t.Fatal("lookup should not be called when config is invalid")
					return nil, nil
				}
			}

			err := run(context.Background(), fakeEnv(tt.env), &stderr, lookup, tt.gh)
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
