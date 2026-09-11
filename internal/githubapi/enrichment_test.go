package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// releaseStub mirrors the fields of GitHub's release API this package reads,
// typed instead of map[string]any so a stubbed test payload can't silently
// drift from the shape ReleaseNotesHTML actually decodes.
type releaseStub struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft,omitempty"`
}

func TestReleaseNotesHTML(t *testing.T) {
	releases := []releaseStub{
		{TagName: "v2.13.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.13.2", Body: "notes 2.13.2"},
		{TagName: "v2.13.1", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.13.1", Body: "notes 2.13.1"},
		{TagName: "v2.12.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.12.2", Body: "notes 2.12.2"},
		{TagName: "v2.12.1", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.12.1", Body: "notes 2.12.1"},
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
		{
			name: "from appears at a lower index than to in the list (backport) does not panic",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode([]releaseStub{
					{TagName: "v2.12.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.12.2", Body: "notes 2.12.2 backported later"},
					{TagName: "v2.13.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.13.2", Body: "notes 2.13.2"},
				})
			},
			from:        "2.12.2",
			to:          "2.13.2",
			wantOK:      true,
			wantContain: []string{"notes 2.13.2"},
		},
		{
			name: "excludes draft releases",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode([]releaseStub{
					{TagName: "v2.14.0", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.14.0", Body: "draft notes", Draft: true},
					{TagName: "v2.13.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.13.2", Body: "notes 2.13.2"},
					{TagName: "v2.12.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.12.2", Body: "notes 2.12.2"},
				})
			},
			from:        "2.12.2",
			to:          "2.13.2",
			wantOK:      true,
			wantContain: []string{"notes 2.13.2"},
		},
		{
			name: "sanitizes mentions and issue references in the release body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode([]releaseStub{
					{TagName: "v2.13.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.13.2", Body: "fix by @octocat in #123"},
					{TagName: "v2.12.2", HTMLURL: "https://github.com/golangci/golangci-lint/releases/tag/v2.12.2", Body: "notes 2.12.2"},
				})
			},
			from:   "2.12.2",
			to:     "2.13.2",
			wantOK: true,
			// The raw "@octocat" and bare "#123" must not survive as-is, since
			// GitHub would turn them into a real mention / a cross-reference
			// to *our* repo's issue #123 rather than the upstream one.
			wantContain: []string{"@\u200boctocat", "https://redirect.github.com/golangci/golangci-lint/issues/123"},
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

// compareStub and commitStub mirror the fields this package reads from
// GitHub's "compare" API response, typed instead of map[string]any.
type compareStub struct {
	HTMLURL string       `json:"html_url"`
	Commits []commitStub `json:"commits"`
}

type commitStub struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
	} `json:"commit"`
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
				resp := compareStub{
					HTMLURL: "https://github.com/matryer/moq/compare/v0.6.0...v0.7.1",
					Commits: []commitStub{
						{SHA: "abc1234567890", HTMLURL: "https://github.com/matryer/moq/commit/abc1234567890"},
					},
				}
				resp.Commits[0].Commit.Message = "feat: add thing\n\nlonger body"
				_ = json.NewEncoder(w).Encode(resp)
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
