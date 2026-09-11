package config

import (
	"reflect"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/grouping"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults when inputs empty",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "main",
			},
			want: Config{
				MiseConfigPaths: []string{"mise.toml"},
				PRStrategy:      grouping.PerTool,
				Labels:          []string{"dependencies"},
				BaseBranch:      "main",
				GitHubToken:     "tok",
				Repository:      "sgash708/example",
				APIURL:          "https://api.github.com",
			},
		},
		{
			name: "parses multiline paths and custom values",
			env: map[string]string{
				"GITHUB_TOKEN":           "tok",
				"GITHUB_REPOSITORY":      "sgash708/example",
				"GITHUB_REF_NAME":        "main",
				"INPUT_MISE_CONFIG_PATH": "mise.toml\nbackend/mise.toml",
				"INPUT_PR_STRATEGY":      "single",
				"INPUT_LABELS":           "dependencies,mise",
				"INPUT_BASE_BRANCH":      "develop",
			},
			want: Config{
				MiseConfigPaths: []string{"mise.toml", "backend/mise.toml"},
				PRStrategy:      grouping.Single,
				Labels:          []string{"dependencies", "mise"},
				BaseBranch:      "develop",
				GitHubToken:     "tok",
				Repository:      "sgash708/example",
				APIURL:          "https://api.github.com",
			},
		},
		{
			name: "missing token returns error",
			env: map[string]string{
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "main",
			},
			wantErr: true,
		},
		{
			name: "missing repository returns error",
			env: map[string]string{
				"GITHUB_TOKEN":    "tok",
				"GITHUB_REF_NAME": "main",
			},
			wantErr: true,
		},
		{
			name: "invalid pr-strategy returns error",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "main",
				"INPUT_PR_STRATEGY": "bogus",
			},
			wantErr: true,
		},
		{
			// Neither base-branch nor GITHUB_REF_NAME is set: with no fallback
			// left, BaseBranch would end up "" and later fail confusingly deep
			// inside the GitHub API call (404 on /git/ref/heads/). Reject it here
			// instead, where the cause is obvious.
			name: "missing base branch returns error",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
			},
			wantErr: true,
		},
		{
			// On a pull_request event, GITHUB_REF_NAME is "123/merge", not a
			// real branch. Using it as BaseBranch would silently target a
			// nonexistent branch, so it must be rejected rather than passed
			// through.
			name: "pull_request-shaped GITHUB_REF_NAME returns error",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "123/merge",
			},
			wantErr: true,
		},
		{
			name: "dry-run input true enables DryRun",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "main",
				"INPUT_DRY_RUN":     "true",
			},
			want: Config{
				MiseConfigPaths: []string{"mise.toml"},
				PRStrategy:      grouping.PerTool,
				Labels:          []string{"dependencies"},
				BaseBranch:      "main",
				GitHubToken:     "tok",
				Repository:      "sgash708/example",
				APIURL:          "https://api.github.com",
				DryRun:          true,
			},
		},
		{
			name: "dry-run input false leaves DryRun disabled",
			env: map[string]string{
				"GITHUB_TOKEN":      "tok",
				"GITHUB_REPOSITORY": "sgash708/example",
				"GITHUB_REF_NAME":   "main",
				"INPUT_DRY_RUN":     "false",
			},
			want: Config{
				MiseConfigPaths: []string{"mise.toml"},
				PRStrategy:      grouping.PerTool,
				Labels:          []string{"dependencies"},
				BaseBranch:      "main",
				GitHubToken:     "tok",
				Repository:      "sgash708/example",
				APIURL:          "https://api.github.com",
				DryRun:          false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromEnv(fakeEnv(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("FromEnv returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Config mismatch:\n got  %+v\n want %+v", got, tt.want)
			}
		})
	}
}
