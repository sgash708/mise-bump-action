package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseNotesHTML(t *testing.T) {
	releases := []map[string]string{
		{"tag_name": "v2.13.2", "html_url": "https://github.com/golangci/golangci-lint/releases/tag/v2.13.2", "body": "notes 2.13.2"},
		{"tag_name": "v2.13.1", "html_url": "https://github.com/golangci/golangci-lint/releases/tag/v2.13.1", "body": "notes 2.13.1"},
		{"tag_name": "v2.12.2", "html_url": "https://github.com/golangci/golangci-lint/releases/tag/v2.12.2", "body": "notes 2.12.2"},
		{"tag_name": "v2.12.1", "html_url": "https://github.com/golangci/golangci-lint/releases/tag/v2.12.1", "body": "notes 2.12.1"},
	}

	tests := []struct {
		name        string
		handler     http.HandlerFunc
		from        string
		to          string
		wantOK      bool
		wantContain []string
	}{
		{
			name: "renders releases strictly between from and to",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(releases)
			},
			from:        "2.12.2",
			to:          "2.13.2",
			wantOK:      true,
			wantContain: []string{"notes 2.13.2", "notes 2.13.1", "Release notes"},
		},
		{
			name: "to version not found returns not ok",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(releases)
			},
			from:   "2.12.2",
			to:     "9.9.9",
			wantOK: false,
		},
		{
			name: "non-2xx response returns not ok",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			from:   "2.12.2",
			to:     "2.13.2",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			got, ok := c.ReleaseNotesHTML(context.Background(), "golangci/golangci-lint", tt.from, tt.to)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (html: %q)", ok, tt.wantOK, got)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("html missing %q: %q", want, got)
				}
			}
		})
	}
}

func TestCommitsHTML(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantOK      bool
		wantContain []string
	}{
		{
			name: "renders commits from the compare API",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"html_url": "https://github.com/matryer/moq/compare/v0.6.0...v0.7.1",
					"commits": []map[string]any{
						{"sha": "abc1234567890", "html_url": "https://github.com/matryer/moq/commit/abc1234567890", "commit": map[string]string{"message": "feat: add thing\n\nlonger body"}},
					},
				})
			},
			wantOK:      true,
			wantContain: []string{"abc1234", "feat: add thing", "compare view"},
		},
		{
			name: "not found on all ref candidates returns not ok",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			got, ok := c.CommitsHTML(context.Background(), "matryer/moq", "v0.6.0", "v0.7.1")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (html: %q)", ok, tt.wantOK, got)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("html missing %q: %q", want, got)
				}
			}
		})
	}
}
