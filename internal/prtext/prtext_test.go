package prtext

import (
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name            string
		entries         []outdated.Entry
		multiConfig     bool
		enrichment      map[string]Enrichment
		wantTitle       string
		wantBodyParts   []string
		wantCommitParts []string
	}{
		{
			name:            "single entry title and trailer, no enrichment falls back to backticks",
			entries:         []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}},
			wantTitle:       "chore(deps): bump go from 1.26.1 to 1.27.0",
			wantBodyParts:   []string{"Bumps `go` from `1.26.1` to `1.27.0`."},
			wantCommitParts: []string{"updated-dependencies:", "dependency-name: go", "dependency-version: 1.27.0"},
		},
		{
			name:        "single entry with multi config appends path",
			entries:     []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "backend/mise.toml"}},
			multiConfig: true,
			wantTitle:   "chore(deps): bump go from 1.26.1 to 1.27.0 in backend/mise.toml",
		},
		{
			name:      "strips backend prefix from display name",
			entries:   []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Requested: "2.9.0", Latest: "2.10.0", RelPath: "mise.toml"}},
			wantTitle: "chore(deps): bump golangci-lint from 2.9.0 to 2.10.0",
		},
		{
			name:    "single entry with enrichment renders a linked bumps line and details blocks",
			entries: []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Requested: "2.12.2", Latest: "2.13.2", RelPath: "mise.toml"}},
			enrichment: map[string]Enrichment{
				"aqua:golangci/golangci-lint": {
					RepoURL:          "https://github.com/golangci/golangci-lint",
					ReleaseNotesHTML: "<details>\n<summary>Release notes</summary>\n...\n</details>",
					CommitsHTML:      "<details>\n<summary>Commits</summary>\n...\n</details>",
				},
			},
			wantTitle: "chore(deps): bump golangci-lint from 2.12.2 to 2.13.2",
			wantBodyParts: []string{
				"Bumps [golangci-lint](https://github.com/golangci/golangci-lint) from 2.12.2 to 2.13.2.",
				"<summary>Release notes</summary>",
				"<summary>Commits</summary>",
			},
		},
		{
			name: "grouped entries lists each tool in trailer",
			entries: []outdated.Entry{
				{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
				{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
			},
			wantTitle:       "chore(deps): bump 2 mise-managed tools",
			wantCommitParts: []string{"dependency-name: go", "dependency-name: node"},
			wantBodyParts:   []string{"- Bumps `go` from `1.26.1` to `1.27.0`.", "- Bumps `node` from `24.12.0` to `24.13.0`."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Build(tt.entries, tt.multiConfig, tt.enrichment)

			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			for _, want := range tt.wantBodyParts {
				if !strings.Contains(got.Body, want) {
					t.Errorf("Body missing %q: %q", want, got.Body)
				}
			}
			for _, want := range tt.wantCommitParts {
				if !strings.Contains(got.Commit, want) {
					t.Errorf("Commit missing %q: %q", want, got.Commit)
				}
			}
		})
	}
}
