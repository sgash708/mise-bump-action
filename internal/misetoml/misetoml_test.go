package misetoml

import (
	"strings"
	"testing"
)

func TestBump(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		toolKey       string
		oldVersion    string
		newVersion    string
		want          string
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:       "plain key",
			content:    "[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n",
			toolKey:    "go",
			oldVersion: "1.26.1",
			newVersion: "1.27.0",
			want:       "[tools]\ngo = \"1.27.0\"\nnode = \"24.12.0\"\n",
		},
		{
			name:       "prefixed quoted key preserves comments",
			content:    "[tools]\ngo = \"1.26.1\"\n\n# aqua registry 経由のツール\n\"aqua:golangci/golangci-lint\" = \"2.9.0\"\n",
			toolKey:    "aqua:golangci/golangci-lint",
			oldVersion: "2.9.0",
			newVersion: "2.10.0",
			want:       "[tools]\ngo = \"1.26.1\"\n\n# aqua registry 経由のツール\n\"aqua:golangci/golangci-lint\" = \"2.10.0\"\n",
		},
		{
			name:       "tool not found",
			content:    "[tools]\ngo = \"1.26.1\"\n",
			toolKey:    "node",
			oldVersion: "24.12.0",
			newVersion: "24.13.0",
			wantErr:    true,
		},
		{
			name:       "version mismatch",
			content:    "[tools]\ngo = \"1.26.1\"\n",
			toolKey:    "go",
			oldVersion: "1.25.0",
			newVersion: "1.27.0",
			wantErr:    true,
		},
		{
			name:       "preserves a trailing inline comment",
			content:    "[tools]\ngo = \"1.26.1\" # pinned for CI\n",
			toolKey:    "go",
			oldVersion: "1.26.1",
			newVersion: "1.27.0",
			want:       "[tools]\ngo = \"1.27.0\" # pinned for CI\n",
		},
		{
			name:       "handles CRLF line endings",
			content:    "[tools]\r\ngo = \"1.26.1\"\r\nnode = \"24.12.0\"\r\n",
			toolKey:    "go",
			oldVersion: "1.26.1",
			newVersion: "1.27.0",
			want:       "[tools]\r\ngo = \"1.27.0\"\r\nnode = \"24.12.0\"\r\n",
		},
		{
			name:       "handles tabs around the equals sign",
			content:    "[tools]\ngo\t=\t\"1.26.1\"\n",
			toolKey:    "go",
			oldVersion: "1.26.1",
			newVersion: "1.27.0",
			want:       "[tools]\ngo\t=\t\"1.27.0\"\n",
		},
		{
			name:       "handles no trailing newline at end of file",
			content:    "[tools]\ngo = \"1.26.1\"",
			toolKey:    "go",
			oldVersion: "1.26.1",
			newVersion: "1.27.0",
			want:       "[tools]\ngo = \"1.27.0\"",
		},
		{
			// The key exists but its value isn't a simple quoted version
			// string (e.g. an inline table or array) — mise.toml supports
			// these forms but Bump doesn't. The error must say so distinctly
			// from "key not found at all", so a user isn't left guessing.
			name:          "clear error when the key's value is not a simple version string",
			content:       "[tools]\ngo = { version = \"1.26.1\" }\n",
			toolKey:       "go",
			oldVersion:    "1.26.1",
			newVersion:    "1.27.0",
			wantErr:       true,
			wantErrSubstr: "not a simple version string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Bump([]byte(tt.content), tt.toolKey, tt.oldVersion, tt.newVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Bump(%q, %q, %q) = nil error, want an error", tt.toolKey, tt.oldVersion, tt.newVersion)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Bump returned error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("content mismatch:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}
