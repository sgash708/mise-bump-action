package githubapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/runner"
)

func TestReadFile(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		ref         string
		handler     http.HandlerFunc
		wantContent string
		wantSHA     string
		wantErr     bool
	}{
		{
			name: "decodes base64 content",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/sgash708/example/contents/mise.toml" {
					http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{
					"sha":     "blobsha123",
					"content": base64.StdEncoding.EncodeToString([]byte("[tools]\ngo = \"1.26.1\"\n")),
				})
			},
			wantContent: "[tools]\ngo = \"1.26.1\"\n",
			wantSHA:     "blobsha123",
		},
		{
			name: "returns an error on non-2xx response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			// path/ref must be escaped per path segment: an unescaped "#" or
			// space would either be silently dropped by net/url or corrupt
			// the request path (e.g. a "#" truncates everything after it).
			name: "escapes special characters in path and ref",
			path: "examples/a b#c/mise.toml",
			ref:  "feature/a b",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// r.URL.Path is already percent-decoded by net/http; this
				// guards against an unescaped "#" (a URL fragment delimiter)
				// truncating the request before it reaches the server.
				if r.URL.Path != "/repos/sgash708/example/contents/examples/a b#c/mise.toml" {
					http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
					return
				}
				if r.URL.Query().Get("ref") != "feature/a b" {
					http.Error(w, "unexpected ref: "+r.URL.Query().Get("ref"), http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{
					"sha":     "blobsha123",
					"content": base64.StdEncoding.EncodeToString([]byte("ok")),
				})
			},
			wantContent: "ok",
			wantSHA:     "blobsha123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			path := tt.path
			if path == "" {
				path = "mise.toml"
			}
			ref := tt.ref
			if ref == "" {
				ref = "main"
			}
			content, sha, err := c.ReadFile(context.Background(), path, ref)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			if string(content) != tt.wantContent {
				t.Errorf("content = %q, want %q", content, tt.wantContent)
			}
			if sha != tt.wantSHA {
				t.Errorf("sha = %q, want %q", sha, tt.wantSHA)
			}
		})
	}
}

// openPRStub and closedPRStub describe one canned pull request returned by
// newBumpPRMux for a head-keyed legacy-branch-name lookup: typed rather than
// map[string]any so each field's shape (an int number, a string title) is
// checked at compile time.
type openPRStub struct {
	Number int    `json:"number"`
	Title  string `json:"title,omitempty"`
}

type closedPRStub struct {
	Number   int     `json:"number"`
	Title    string  `json:"title,omitempty"`
	MergedAt *string `json:"merged_at"`
}

// strPtr takes the address of a string literal, which Go doesn't allow
// directly (&"foo" is not valid syntax).
func strPtr(s string) *string { return &s }

// refObjStub mirrors the GitHub "get ref" response shape ({"object":
// {"sha": ...}}), shared by the base-branch and stale-branch lookups.
type refObjStub struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// bumpPRMuxOpts configures newBumpPRMux's canned responses.
type bumpPRMuxOpts struct {
	openPRs      []map[string]int // exact-branch state=open lookup response
	closedPRs    []closedPRStub   // exact-branch state=closed lookup response
	branchExists bool
	otherOpenPRs []openPRRef // state=open list (no head filter), for superseded-PR search
	// openPRsByHead/closedPRsByHead, keyed by the exact "owner:branch" head
	// query value, take priority over openPRs/closedPRs when set — used to
	// give a legacy branch name a different lookup result than the primary
	// one, including a Title that exercises OpenBumpPR's title-based
	// collision guard on legacy matches.
	openPRsByHead   map[string][]openPRStub
	closedPRsByHead map[string][]closedPRStub
}

// newBumpPRMux builds a ServeMux with handlers for every endpoint OpenBumpPR
// may call, recording each call into *calls in invocation order. Individual
// tests override specific handlers to exercise existing-PR/closed-PR/
// stale-branch/superseded-PR branches.
func newBumpPRMux(t *testing.T, calls *[]string, opts bumpPRMuxOpts) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") == "closed" && q.Get("head") != "":
			*calls = append(*calls, "find-closed-pr")
			if resp, ok := opts.closedPRsByHead[q.Get("head")]; ok {
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			_ = json.NewEncoder(w).Encode(opts.closedPRs)
		case q.Get("state") == "open" && q.Get("head") != "":
			*calls = append(*calls, "find-open-pr")
			if resp, ok := opts.openPRsByHead[q.Get("head")]; ok {
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			_ = json.NewEncoder(w).Encode(opts.openPRs)
		case q.Get("state") == "open":
			*calls = append(*calls, "list-open-prs")
			_ = json.NewEncoder(w).Encode(opts.otherOpenPRs)
		default:
			t.Fatalf("unexpected GET /pulls query: %s", r.URL.RawQuery)
		}
	})
	mux.HandleFunc("GET /repos/sgash708/example/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "get-ref")
		resp := refObjStub{}
		resp.Object.SHA = "basesha"
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /repos/sgash708/example/git/ref/heads/mise-bump/go-1.27.0", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "get-branch-sha")
		if !opts.branchExists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := refObjStub{}
		resp.Object.SHA = "stalesha"
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("DELETE /repos/sgash708/example/git/refs/heads/mise-bump/go-1.27.0", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "delete-ref")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /repos/sgash708/example/git/refs", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "create-ref")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("PUT /repos/sgash708/example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "put-file")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("POST /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "create-pr")
		_ = json.NewEncoder(w).Encode(map[string]int{"number": 42})
	})
	mux.HandleFunc("PATCH /repos/sgash708/example/pulls/{number}", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "close-pr:"+r.PathValue("number"))
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("POST /repos/sgash708/example/issues/{number}/labels", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "add-labels")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /repos/sgash708/example/issues/{number}/comments", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "comment:"+r.PathValue("number"))
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})

	return mux
}

func TestOpenBumpPR(t *testing.T) {
	tests := []struct {
		name               string
		labels             []string
		branchPrefix       string
		legacyBranchNames  []string
		legacyMatchName    string
		legacyMatchVersion string
		openPRs            []map[string]int
		closedPRs          []closedPRStub
		openPRsByHead      map[string][]openPRStub
		closedPRsByHead    map[string][]closedPRStub
		branchExists       bool
		otherOpenPRs       []openPRRef
		wantCalls          []string
		wantNumber         int
		wantCreated        bool
		wantErr            error
		wantErrSubstr      string
	}{
		{
			name:        "calls branch, commit, PR, and labels in order",
			labels:      []string{"dependencies"},
			wantCalls:   []string{"find-open-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr", "add-labels"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			name:        "skips add-labels when no labels are configured",
			labels:      nil,
			wantCalls:   []string{"find-open-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			name:       "returns the existing PR number when one is already open for the branch",
			labels:     []string{"dependencies"},
			openPRs:    []map[string]int{{"number": 99}},
			wantCalls:  []string{"find-open-pr"},
			wantNumber: 99,
		},
		{
			name:         "deletes and recreates a stale branch when no open PR exists",
			labels:       []string{"dependencies"},
			branchExists: true,
			wantCalls:    []string{"find-open-pr", "find-closed-pr", "get-ref", "get-branch-sha", "delete-ref", "create-ref", "put-file", "create-pr", "add-labels"},
			wantNumber:   42,
			wantCreated:  true,
		},
		{
			// Dependabot semantics: closing a PR without merging means "don't
			// reopen this exact bump." No writes must happen past this check.
			name:          "returns ErrClosedPreviously and makes no writes when a matching PR was closed unmerged",
			labels:        []string{"dependencies"},
			closedPRs:     []closedPRStub{{Number: 7}},
			wantCalls:     []string{"find-open-pr", "find-closed-pr"},
			wantErr:       runner.ErrClosedPreviously,
			wantErrSubstr: "previously closed",
		},
		{
			// A closed PR that WAS merged must not block re-proposing the
			// same bump (e.g. it was merged, then reverted upstream).
			name:        "proceeds normally when the matching closed PR was merged",
			labels:      nil,
			closedPRs:   []closedPRStub{{Number: 7, MergedAt: strPtr("2026-01-01T00:00:00Z")}},
			wantCalls:   []string{"find-open-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			// An older open PR for the same tool (different version) must be
			// closed with a comment once the new PR is open.
			name:         "closes a superseded open PR for the same tool",
			labels:       nil,
			branchPrefix: "mise-bump/go-",
			otherOpenPRs: []openPRRef{
				{Number: 10, Head: prHeadRef{Ref: "mise-bump/go-1.26.1"}},
				{Number: 11, Head: prHeadRef{Ref: "mise-bump/go-1.27.0"}},   // excludeBranch: must not be touched
				{Number: 12, Head: prHeadRef{Ref: "mise-bump/node-20.0.0"}}, // different tool: must not be touched
			},
			wantCalls:   []string{"find-open-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr", "list-open-prs", "close-pr:10", "comment:10"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			// A branch-naming scheme change must not forget a PR opened
			// under the old scheme: it's still found via LegacyBranchNames,
			// and no new branch/PR is created (ADR 0017).
			name:               "finds an existing open PR under a legacy branch name",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "go",
			legacyMatchVersion: "1.27.0",
			openPRsByHead: map[string][]openPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {{Number: 77, Title: "chore(deps): bump go from 1.26.1 to 1.27.0"}},
			},
			wantCalls:  []string{"find-open-pr", "find-open-pr"},
			wantNumber: 77,
		},
		{
			// Same as above, but the legacy PR was closed without merging:
			// still must not reopen it as a "new" bump under the new name.
			name:               "returns ErrClosedPreviously for a bump previously closed under a legacy branch name",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "go",
			legacyMatchVersion: "1.27.0",
			closedPRsByHead: map[string][]closedPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {{Number: 7, Title: "chore(deps): bump go from 1.26.1 to 1.27.0", MergedAt: nil}},
			},
			wantCalls:     []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr"},
			wantErr:       runner.ErrClosedPreviously,
			wantErrSubstr: "previously closed",
		},
		{
			// legacyBranchNames is built from sanitize(name) alone (no
			// fingerprint), so a different tool's PR can share the exact
			// legacy branch name (ADR 0018). A title whose tool name doesn't
			// match must be treated as "not this bump," not as an existing
			// match — the bump proceeds to open its own new PR instead of
			// silently adopting someone else's.
			name:               "does not match a legacy branch name whose PR title belongs to a different tool",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "django",
			legacyMatchVersion: "1.27.0",
			openPRsByHead: map[string][]openPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {{Number: 88, Title: "chore(deps): bump foo-bar from 1.26.1 to 1.27.0"}},
			},
			wantCalls:   []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			// The realistic case ADR 0018's legacy-name collision actually
			// arises from: two different original tool names whose
			// ShortName differs only by "/" versus "-" (e.g.
			// "go:github.com/foo/bar" -> "bar" and "go:github.com/foo-bar"
			// -> "foo-bar") sanitize to the identical legacy branch name.
			// "bar" must not match a title bumping "foo-bar": a bare \b
			// word-boundary check would wrongly match here, since "-" is
			// not a word character to Go's regexp package and "bar" is a
			// whole word inside "foo-bar" (ADR 0019 fixed this).
			name:               "does not match a legacy branch name whose title bumps a tool name this tool's name is a suffix of",
			legacyBranchNames:  []string{"mise-bump/go-github.com-foo-bar-1.27.0-legacy"},
			legacyMatchName:    "bar",
			legacyMatchVersion: "1.27.0",
			openPRsByHead: map[string][]openPRStub{
				"sgash708:mise-bump/go-github.com-foo-bar-1.27.0-legacy": {{Number: 88, Title: "chore(deps): bump foo-bar from 1.26.1 to 1.27.0"}},
			},
			wantCalls:   []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr"},
			wantNumber:  42,
			wantCreated: true,
		},
		{
			// The legacy PR's title still encodes the "from" version as of
			// when it was opened. If the pin was manually bumped partway
			// since (a common thing to do after closing an unwanted PR),
			// that "from" no longer matches what runner would regenerate
			// today — an exact in.PRTitle comparison would miss this and
			// resurrect the bump. Matching on tool name + target version
			// alone must still find it (ADR 0019).
			name:               "matches a legacy branch name whose title has a different 'from' version",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "go",
			legacyMatchVersion: "1.27.0",
			closedPRsByHead: map[string][]closedPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {{Number: 7, Title: "chore(deps): bump go from 1.26.5 to 1.27.0", MergedAt: nil}},
			},
			wantCalls:     []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr"},
			wantErr:       runner.ErrClosedPreviously,
			wantErrSubstr: "previously closed",
		},
		{
			// Likewise, the legacy PR's title may carry an " in <path>"
			// suffix from when mise-config-path had more than one entry
			// (or may lack it, if it now does but didn't before). That
			// suffix must not affect the match.
			name:               "matches a legacy branch name whose title has a different mise-config-path suffix",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "go",
			legacyMatchVersion: "1.27.0",
			closedPRsByHead: map[string][]closedPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {{Number: 7, Title: "chore(deps): bump go from 1.26.1 to 1.27.0 in tools/mise.toml", MergedAt: nil}},
			},
			wantCalls:     []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr"},
			wantErr:       runner.ErrClosedPreviously,
			wantErrSubstr: "previously closed",
		},
		{
			// A branch can have more than one closed PR in its history
			// (e.g. closed, force-pushed a different bump, closed again).
			// findClosedUnmergedPR must check every one against the match
			// predicate rather than stopping at the first — a non-matching
			// closed PR listed before a matching one must not hide it.
			name:               "matches a closed PR that is not first in the list under a legacy branch name",
			legacyBranchNames:  []string{"mise-bump/go-1.27.0-legacy"},
			legacyMatchName:    "go",
			legacyMatchVersion: "1.27.0",
			closedPRsByHead: map[string][]closedPRStub{
				"sgash708:mise-bump/go-1.27.0-legacy": {
					{Number: 6, Title: "chore(deps): bump go from 1.25.0 to 1.26.0", MergedAt: nil},
					{Number: 7, Title: "chore(deps): bump go from 1.26.1 to 1.27.0", MergedAt: nil},
				},
			},
			wantCalls:     []string{"find-open-pr", "find-open-pr", "find-closed-pr", "find-closed-pr"},
			wantErr:       runner.ErrClosedPreviously,
			wantErrSubstr: "previously closed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			mux := newBumpPRMux(t, &calls, bumpPRMuxOpts{
				openPRs:         tt.openPRs,
				closedPRs:       tt.closedPRs,
				openPRsByHead:   tt.openPRsByHead,
				closedPRsByHead: tt.closedPRsByHead,
				branchExists:    tt.branchExists,
				otherOpenPRs:    tt.otherOpenPRs,
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			number, created, err := c.OpenBumpPR(context.Background(), runner.BumpPRInput{
				BaseBranch:         "main",
				BranchName:         "mise-bump/go-1.27.0",
				BranchPrefix:       tt.branchPrefix,
				LegacyBranchNames:  tt.legacyBranchNames,
				LegacyMatchName:    tt.legacyMatchName,
				LegacyMatchVersion: tt.legacyMatchVersion,
				FilePath:           "mise.toml",
				FileContent:        []byte("[tools]\ngo = \"1.27.0\"\n"),
				FileSHA:            "blobsha123",
				CommitMessage:      "chore(deps): bump go from 1.26.1 to 1.27.0",
				PRTitle:            "chore(deps): bump go from 1.26.1 to 1.27.0",
				PRBody:             "Bumps go.",
				Labels:             tt.labels,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.wantErr)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrSubstr)
				}
			} else if err != nil {
				t.Fatalf("OpenBumpPR returned error: %v", err)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
			}
			if created != tt.wantCreated {
				t.Errorf("created = %v, want %v", created, tt.wantCreated)
			}
			if len(calls) != len(tt.wantCalls) {
				t.Fatalf("calls = %+v, want %+v", calls, tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if calls[i] != want {
					t.Errorf("calls[%d] = %q, want %q", i, calls[i], want)
				}
			}
		})
	}
}

// prWithLabelsStub is openPRRef plus a labels field GitHub's real API
// includes but listOpenPRRefs never reads — used only to prove counting is
// based on branch name, not label (ADR 0016).
type prWithLabelsStub struct {
	Number int           `json:"number"`
	Labels []prLabelStub `json:"labels,omitempty"`
	Head   prHeadRef     `json:"head"`
}

type prLabelStub struct {
	Name string `json:"name"`
}

func TestCountOpenBumpPRs(t *testing.T) {
	tests := []struct {
		name      string
		prs       []prWithLabelsStub
		wantCount int
	}{
		{
			name: "counts only PRs whose branch is in this action's namespace",
			prs: []prWithLabelsStub{
				{Number: 1, Head: prHeadRef{Ref: "mise-bump/go_1.27.0"}},
				{Number: 2, Head: prHeadRef{Ref: "dependabot/npm_and_yarn/left-pad-1.3.0"}},
				{Number: 3, Head: prHeadRef{Ref: "mise-bump/batch-abc123"}},
			},
			wantCount: 2,
		},
		{
			// A repo running both mise-bump-action and Dependabot: Dependabot
			// defaults to the same "dependencies" label mise-bump-action
			// does, so counting by label would wrongly count Dependabot's
			// own PRs against max-open-prs (ADR 0016).
			name: "does not count another tool's PR sharing the default dependencies label",
			prs: []prWithLabelsStub{
				{Number: 1, Labels: []prLabelStub{{Name: "dependencies"}}, Head: prHeadRef{Ref: "dependabot/npm_and_yarn/left-pad-1.3.0"}},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.prs)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")
			count, err := c.CountOpenBumpPRs(context.Background(), "main")
			if err != nil {
				t.Fatalf("CountOpenBumpPRs returned error: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

// TestListOpenPRRefsFetchesAllPages guards against undercounting/missing
// superseded PRs in a repository with more than one page (100) of open PRs
// into base: CountOpenBumpPRs/HasOpenPRWithPrefix/closeSupersededPRs all
// share listOpenPRRefs, so fixing pagination there fixes it everywhere.
func TestListOpenPRRefsFetchesAllPages(t *testing.T) {
	page1 := make([]openPRRef, 100)
	for i := range page1 {
		page1[i] = openPRRef{Number: i + 1, Head: prHeadRef{Ref: fmt.Sprintf("mise-bump/tool%d-1.0.0", i)}}
	}
	page2 := []openPRRef{
		{Number: 101, Head: prHeadRef{Ref: "mise-bump/last-1.0.0"}},
	}

	var gotPages []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		gotPages = append(gotPages, page)
		if page == "2" {
			_ = json.NewEncoder(w).Encode(page2)
			return
		}
		_ = json.NewEncoder(w).Encode(page1)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")
	count, err := c.CountOpenBumpPRs(context.Background(), "main")
	if err != nil {
		t.Fatalf("CountOpenBumpPRs returned error: %v", err)
	}
	if count != 101 {
		t.Errorf("count = %d, want 101 (across two pages)", count)
	}
	if len(gotPages) != 2 || gotPages[0] != "1" || gotPages[1] != "2" {
		t.Errorf("expected requests for page=1 then page=2, got %+v", gotPages)
	}
}

func TestHasOpenPRWithPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		prs    []openPRRef
		want   bool
	}{
		{
			name:   "finds a PR whose branch starts with prefix",
			prefix: "mise-bump/go_",
			prs: []openPRRef{
				{Number: 1, Head: prHeadRef{Ref: "mise-bump/go_1.26.1"}},
			},
			want: true,
		},
		{
			name:   "does not match a different tool sharing a name prefix",
			prefix: "mise-bump/go_",
			prs: []openPRRef{
				{Number: 1, Head: prHeadRef{Ref: "mise-bump/go-github.com-matryer-moq_v0.7.1"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.prs)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")
			has, err := c.HasOpenPRWithPrefix(context.Background(), "main", tt.prefix)
			if err != nil {
				t.Fatalf("HasOpenPRWithPrefix returned error: %v", err)
			}
			if has != tt.want {
				t.Errorf("has = %v, want %v", has, tt.want)
			}
		})
	}
}

func TestDo_RetriesOnceOn429WithRetryAfter(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": "blobsha123", "content": ""})
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

	if err := c.do(context.Background(), http.MethodGet, "/repos/sgash708/example/x", nil, &struct{}{}); err != nil {
		t.Fatalf("do returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry after 429)", attempts)
	}
}

func TestDo_DoesNotRetryTwice(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

	err := c.do(context.Background(), http.MethodGet, "/repos/sgash708/example/x", nil, nil)
	if err == nil {
		t.Fatal("expected an error after exhausting the single retry, got nil")
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (initial + one retry, no more)", attempts)
	}
}
