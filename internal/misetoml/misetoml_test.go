package misetoml

import "testing"

func TestBump(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		toolKey    string
		oldVersion string
		newVersion string
		want       string
		wantErr    bool
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Bump([]byte(tt.content), tt.toolKey, tt.oldVersion, tt.newVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Bump(%q, %q, %q) = nil error, want an error", tt.toolKey, tt.oldVersion, tt.newVersion)
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
