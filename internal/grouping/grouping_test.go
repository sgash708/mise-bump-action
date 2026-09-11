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

func TestGroup(t *testing.T) {
	tests := []struct {
		name        string
		entries     []outdated.Entry
		strategy    Strategy
		wantGroups  int
		wantPerSize []int // length of Entries in each returned group, in order
		wantErr     bool
	}{
		{
			name:        "per-tool creates one group per entry",
			entries:     entries("go", "node", "aqua:golangci/golangci-lint"),
			strategy:    PerTool,
			wantGroups:  3,
			wantPerSize: []int{1, 1, 1},
		},
		{
			name:        "single creates one group with all entries",
			entries:     entries("go", "node", "aqua:golangci/golangci-lint"),
			strategy:    Single,
			wantGroups:  1,
			wantPerSize: []int{3},
		},
		{
			name:       "empty entries returns no groups",
			entries:    nil,
			strategy:   Single,
			wantGroups: 0,
		},
		{
			name:     "unknown strategy returns error",
			entries:  entries("go"),
			strategy: Strategy("bogus"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Group(tt.entries, tt.strategy)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Group(strategy=%q) = nil error, want an error", tt.strategy)
				}
				return
			}
			if err != nil {
				t.Fatalf("Group returned error: %v", err)
			}
			if len(got) != tt.wantGroups {
				t.Fatalf("expected %d groups, got %d", tt.wantGroups, len(got))
			}
			for i, wantSize := range tt.wantPerSize {
				if len(got[i].Entries) != wantSize {
					t.Errorf("group %d: expected %d entries, got %d", i, wantSize, len(got[i].Entries))
				}
			}
		})
	}
}
