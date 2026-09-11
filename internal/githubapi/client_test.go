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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

			content, sha, err := c.ReadFile(context.Background(), "mise.toml", "main")
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

func TestOpenBumpPR(t *testing.T) {
	tests := []struct {
		name      string
		labels    []string
		wantCalls []string
	}{
		{
			name:      "calls branch, commit, PR, and labels in order",
			labels:    []string{"dependencies"},
			wantCalls: []string{"get-ref", "create-ref", "put-file", "create-pr", "add-labels"},
		},
		{
			name:      "skips add-labels when no labels are configured",
			labels:    nil,
			wantCalls: []string{"get-ref", "create-ref", "put-file", "create-pr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/sgash708/example/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "get-ref")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"object": map[string]string{"sha": "basesha"},
				})
			})
			mux.HandleFunc("/repos/sgash708/example/git/refs", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "create-ref")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]string{})
			})
			mux.HandleFunc("/repos/sgash708/example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "put-file")
				_ = json.NewEncoder(w).Encode(map[string]string{})
			})
			mux.HandleFunc("/repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "create-pr")
				_ = json.NewEncoder(w).Encode(map[string]int{"number": 42})
			})
			mux.HandleFunc("/repos/sgash708/example/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "add-labels")
				w.WriteHeader(http.StatusOK)
			})
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
			if number != 42 {
				t.Errorf("number = %d, want 42", number)
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
