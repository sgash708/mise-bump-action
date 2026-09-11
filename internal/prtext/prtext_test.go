package prtext

import (
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestBuild_SingleEntryTitleAndTrailer(t *testing.T) {
	entries := []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump go from 1.26.1 to 1.27.0"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
	if !strings.Contains(got.Commit, "updated-dependencies:") {
		t.Errorf("Commit missing updated-dependencies trailer: %q", got.Commit)
	}
	if !strings.Contains(got.Commit, "dependency-name: go") {
		t.Errorf("Commit missing dependency-name: %q", got.Commit)
	}
	if !strings.Contains(got.Commit, "dependency-version: 1.27.0") {
		t.Errorf("Commit missing dependency-version: %q", got.Commit)
	}
}

func TestBuild_SingleEntryWithMultiConfigAppendsPath(t *testing.T) {
	entries := []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "backend/mise.toml"}}

	got := Build(entries, true)

	wantTitle := "chore(deps): bump go from 1.26.1 to 1.27.0 in backend/mise.toml"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
}

func TestBuild_StripsBackendPrefixFromDisplayName(t *testing.T) {
	entries := []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Requested: "2.9.0", Latest: "2.10.0", RelPath: "mise.toml"}}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump golangci-lint from 2.9.0 to 2.10.0"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
}

func TestBuild_GroupedEntriesListsEachToolInTrailer(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump 2 mise-managed tools"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
	for _, want := range []string{"dependency-name: go", "dependency-name: node"} {
		if !strings.Contains(got.Commit, want) {
			t.Errorf("Commit missing %q: %q", want, got.Commit)
		}
	}
}
