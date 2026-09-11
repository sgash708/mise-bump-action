package grouping

import (
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func entries(names ...string) []outdated.Entry {
	es := make([]outdated.Entry, len(names))
	for i, n := range names {
		es[i] = outdated.Entry{Name: n}
	}
	return es
}

func TestGroup_PerToolCreatesOneGroupPerEntry(t *testing.T) {
	got, err := Group(entries("go", "node", "aqua:golangci/golangci-lint"), PerTool)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(got))
	}
	for i, g := range got {
		if len(g.Entries) != 1 {
			t.Errorf("group %d: expected 1 entry, got %d", i, len(g.Entries))
		}
	}
}

func TestGroup_SingleCreatesOneGroupWithAllEntries(t *testing.T) {
	got, err := Group(entries("go", "node", "aqua:golangci/golangci-lint"), Single)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 group, got %d", len(got))
	}
	if len(got[0].Entries) != 3 {
		t.Fatalf("expected 3 entries in the single group, got %d", len(got[0].Entries))
	}
}

func TestGroup_EmptyEntriesReturnsNoGroups(t *testing.T) {
	got, err := Group(nil, Single)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 groups for empty input, got %d", len(got))
	}
}

func TestGroup_UnknownStrategyReturnsError(t *testing.T) {
	_, err := Group(entries("go"), Strategy("bogus"))
	if err == nil {
		t.Fatal("expected an error for an unknown strategy, got nil")
	}
}
