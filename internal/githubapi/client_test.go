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

func TestReadFile_DecodesBase64Content(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/sgash708/example/contents/mise.toml" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sha":     "blobsha123",
			"content": base64.StdEncoding.EncodeToString([]byte("[tools]\ngo = \"1.26.1\"\n")),
		})
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

	content, sha, err := c.ReadFile(context.Background(), "mise.toml", "main")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "[tools]\ngo = \"1.26.1\"\n" {
		t.Errorf("content = %q", content)
	}
	if sha != "blobsha123" {
		t.Errorf("sha = %q, want blobsha123", sha)
	}
}

func TestOpenBumpPR_CallsBranchCommitPRAndLabelsInOrder(t *testing.T) {
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
		Labels:        []string{"dependencies"},
	})
	if err != nil {
		t.Fatalf("OpenBumpPR returned error: %v", err)
	}
	if number != 42 {
		t.Errorf("number = %d, want 42", number)
	}
	wantCalls := []string{"get-ref", "create-ref", "put-file", "create-pr", "add-labels"}
	if len(calls) != len(wantCalls) {
		t.Fatalf("calls = %+v, want %+v", calls, wantCalls)
	}
	for i, want := range wantCalls {
		if calls[i] != want {
			t.Errorf("calls[%d] = %q, want %q", i, calls[i], want)
		}
	}
}
