package githubapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				// net/http decodes percent-escapes when populating r.URL.Path,
				// so the decoded form is what a correctly-escaped request
				// looks like here. What this test actually guards against is
				// the "#" being sent unescaped: an unescaped "#" is a URL
				// fragment delimiter and never reaches the server at all, so
				// the request would arrive truncated instead of with a wrong
				// literal path.
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

// newBumpPRMux builds a ServeMux with handlers for every endpoint OpenBumpPR
// may call, recording each call into *calls in invocation order. Individual
// tests override specific handlers to exercise existing-PR/stale-branch
// branches.
func newBumpPRMux(t *testing.T, calls *[]string, opts struct {
	openPRs      []map[string]int
	branchExists bool
	prNumber     int
}) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "find-open-pr")
		_ = json.NewEncoder(w).Encode(opts.openPRs)
	})
	mux.HandleFunc("GET /repos/sgash708/example/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "get-ref")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": map[string]string{"sha": "basesha"},
		})
	})
	mux.HandleFunc("GET /repos/sgash708/example/git/ref/heads/mise-bump/go-1.27.0", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "get-branch-sha")
		if !opts.branchExists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": map[string]string{"sha": "stalesha"},
		})
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
	mux.HandleFunc("POST /repos/sgash708/example/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, "add-labels")
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func TestOpenBumpPR(t *testing.T) {
	tests := []struct {
		name         string
		labels       []string
		openPRs      []map[string]int
		branchExists bool
		wantCalls    []string
		wantNumber   int
	}{
		{
			name:       "calls branch, commit, PR, and labels in order",
			labels:     []string{"dependencies"},
			wantCalls:  []string{"find-open-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr", "add-labels"},
			wantNumber: 42,
		},
		{
			name:       "skips add-labels when no labels are configured",
			labels:     nil,
			wantCalls:  []string{"find-open-pr", "get-ref", "get-branch-sha", "create-ref", "put-file", "create-pr"},
			wantNumber: 42,
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
			wantCalls:    []string{"find-open-pr", "get-ref", "get-branch-sha", "delete-ref", "create-ref", "put-file", "create-pr", "add-labels"},
			wantNumber:   42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			mux := newBumpPRMux(t, &calls, struct {
				openPRs      []map[string]int
				branchExists bool
				prNumber     int
			}{openPRs: tt.openPRs, branchExists: tt.branchExists})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			number, err := c.OpenBumpPR(context.Background(), runner.BumpPRInput{
				BaseBranch:    "main",
				BranchName:    "mise-bump/go-1.27.0",
				FilePath:      "mise.toml",
				FileContent:   []byte("[tools]\ngo = \"1.27.0\"\n"),
				FileSHA:       "blobsha123",
				CommitMessage: "chore(deps): bump go from 1.26.1 to 1.27.0",
				PRTitle:       "chore(deps): bump go from 1.26.1 to 1.27.0",
				PRBody:        "Bumps go.",
				Labels:        tt.labels,
			})
			if err != nil {
				t.Fatalf("OpenBumpPR returned error: %v", err)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
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
