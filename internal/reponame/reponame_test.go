package reponame

import "testing"

func TestFromToolName(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
		wantOK   bool
	}{
		{
			name:     "aqua backend",
			toolName: "aqua:golangci/golangci-lint",
			want:     "golangci/golangci-lint",
			wantOK:   true,
		},
		{
			name:     "go install backend on github.com",
			toolName: "go:github.com/matryer/moq",
			want:     "matryer/moq",
			wantOK:   true,
		},
		{
			name:     "go install backend with a subpackage path",
			toolName: "go:github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen",
			want:     "oapi-codegen/oapi-codegen",
			wantOK:   true,
		},
		{
			name:     "core tool with no resolvable repo",
			toolName: "go",
			wantOK:   false,
		},
		{
			name:     "go install backend not hosted on github.com",
			toolName: "go:golang.org/x/vuln/cmd/govulncheck",
			wantOK:   false,
		},
		{
			name:     "aqua backend with an unexpected shape",
			toolName: "aqua:not-owner-slash-repo",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FromToolName(tt.toolName)
			if ok != tt.wantOK {
				t.Fatalf("FromToolName(%q) ok = %v, want %v", tt.toolName, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("FromToolName(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}
