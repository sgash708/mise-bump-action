// Package e2e_test builds the real mise-bump-action binary and runs it as a
// subprocess against a fake `mise` CLI and a mocked GitHub API, so the full
// pipeline (outdated detection -> grouping -> PR text -> GitHub API calls)
// is exercised the same way action.yml exercises it, without needing a real
// GitHub Actions run.
package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// binary builds cmd/mise-bump-action once for the whole test run and returns
// its path.
func binary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("e2e test requires a POSIX shell for the fake mise script")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mise-bump-action-e2e")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "mise-bump-action")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/sgash708/mise-bump-action/cmd/mise-bump-action")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build failed: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("%v", buildErr)
	}
	return binPath
}

// realTempDir returns t.TempDir(), resolved through any symlinks (macOS's
// /tmp is a symlink to /private/tmp). The subprocess's own working-directory
// resolution follows the real path, so comparing raw vs. resolved forms
// would spuriously mismatch when the binary computes absolute paths.
func realTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to resolve temp dir symlinks: %v", err)
	}
	return resolved
}

// installFakeMise writes script as an executable "mise" and returns the
// directory it lives in, to be prepended to the subprocess's PATH.
func installFakeMise(t *testing.T, script string) string {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "mise"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake mise script: %v", err)
	}
	return binDir
}

// recordedRequest captures one request the mock GitHub API server received.
// body is kept as raw JSON (rather than decoded into a shared map[string]any)
// since a single recorder here sees requests of many different shapes
// (create-ref, put-file, create-pr, ...); each assertion below decodes only
// the specific fields it needs into its own small typed struct.
type recordedRequest struct {
	method string
	path   string
	body   json.RawMessage
}

// mockGitHub builds an httptest server covering every endpoint the binary's
// happy path calls, recording each request into *requests. No open PR and no
// stale branch exist, so every scenario takes the create-branch-and-PR path.
func mockGitHub(t *testing.T, requests *[]recordedRequest) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	record := func(r *http.Request) json.RawMessage {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*requests = append(*requests, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		mu.Unlock()
		return body
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sgash708/e2e-example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sha":     "blobsha",
			"content": base64.StdEncoding.EncodeToString([]byte(fixtureMiseToml)),
		})
	})
	mux.HandleFunc("GET /repos/sgash708/e2e-example/pulls", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode([]map[string]int{})
	})
	mux.HandleFunc("GET /repos/sgash708/e2e-example/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		resp := struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}{}
		resp.Object.SHA = "basesha"
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /repos/sgash708/e2e-example/git/ref/heads/", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("POST /repos/sgash708/e2e-example/git/refs", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("PUT /repos/sgash708/e2e-example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("POST /repos/sgash708/e2e-example/pulls", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_ = json.NewEncoder(w).Encode(map[string]int{"number": 7})
	})
	mux.HandleFunc("POST /repos/sgash708/e2e-example/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

const fakeMiseOutdatedTemplate = `#!/bin/sh
cat <<EOF
{
  "aqua:golangci/golangci-lint": {
    "name": "aqua:golangci/golangci-lint",
    "requested": "2.12.2",
    "current": "2.12.2",
    "bump": "2.13.2",
    "latest": "2.13.2",
    "source": { "type": "mise.toml", "path": "%s/mise.toml" }
  }
}
EOF
`

const fixtureMiseToml = "[tools]\n\"aqua:golangci/golangci-lint\" = \"2.12.2\"\n"

func TestE2E_OpensRealPullRequest(t *testing.T) {
	bin := binary(t)
	repoDir := realTempDir(t)
	if err := os.WriteFile(filepath.Join(repoDir, "mise.toml"), []byte(fixtureMiseToml), 0o644); err != nil {
		t.Fatalf("failed to write fixture mise.toml: %v", err)
	}
	fakeMiseDir := installFakeMise(t, fmt.Sprintf(fakeMiseOutdatedTemplate, repoDir))

	var requests []recordedRequest
	srv := mockGitHub(t, &requests)
	defer srv.Close()

	outputFile := filepath.Join(repoDir, "github-output")
	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"PATH="+fakeMiseDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_TOKEN=tok",
		"GITHUB_REPOSITORY=sgash708/e2e-example",
		"GITHUB_API_URL="+srv.URL,
		"GITHUB_REF_NAME=main",
		"GITHUB_OUTPUT="+outputFile,
		"INPUT_MISE_CONFIG_PATH=mise.toml",
		"INPUT_PR_STRATEGY=per-tool",
		"INPUT_LABELS=dependencies",
		"INPUT_DRY_RUN=false",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("binary exited with error: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "opened 1 pull request(s): [7]") {
		t.Errorf("expected stdout to report the opened PR, got:\n%s", out)
	}

	outputs, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read GITHUB_OUTPUT file: %v", err)
	}
	if !strings.Contains(string(outputs), "opened-count=1") || !strings.Contains(string(outputs), "pr-numbers=7") {
		t.Errorf("expected GITHUB_OUTPUT to record the opened PR, got:\n%s", outputs)
	}

	var createPR, putFile *recordedRequest
	for i := range requests {
		switch {
		case requests[i].method == "POST" && requests[i].path == "/repos/sgash708/e2e-example/pulls":
			createPR = &requests[i]
		case requests[i].method == "PUT" && requests[i].path == "/repos/sgash708/e2e-example/contents/mise.toml":
			putFile = &requests[i]
		}
	}
	if createPR == nil {
		t.Fatal("expected a POST to /pulls, got none")
	}
	wantTitle := "chore(deps): bump golangci-lint from 2.12.2 to 2.13.2"
	var prBody struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(createPR.body, &prBody)
	if prBody.Title != wantTitle {
		t.Errorf("PR title = %q, want %q", prBody.Title, wantTitle)
	}

	if putFile == nil {
		t.Fatal("expected a PUT to /contents/mise.toml, got none")
	}
	var fileBody struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(putFile.body, &fileBody)
	decoded, err := base64.StdEncoding.DecodeString(fileBody.Content)
	if err != nil {
		t.Fatalf("failed to decode committed file content: %v", err)
	}
	if !strings.Contains(string(decoded), `"aqua:golangci/golangci-lint" = "2.13.2"`) {
		t.Errorf("committed mise.toml = %q, want it to contain the bumped version", decoded)
	}
}

func TestE2E_DryRunOpensNoPullRequest(t *testing.T) {
	bin := binary(t)
	repoDir := realTempDir(t)
	if err := os.WriteFile(filepath.Join(repoDir, "mise.toml"), []byte(fixtureMiseToml), 0o644); err != nil {
		t.Fatalf("failed to write fixture mise.toml: %v", err)
	}
	fakeMiseDir := installFakeMise(t, fmt.Sprintf(fakeMiseOutdatedTemplate, repoDir))

	var requests []recordedRequest
	srv := mockGitHub(t, &requests)
	defer srv.Close()

	// dry-run still needs read access (ReadFile, enrichment lookups), so the
	// mock's non-mutating endpoints must be reachable; only the mutating
	// ones (create-ref, put-file, create-pr) must never be hit.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sgash708/e2e-example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sha":     "blobsha",
			"content": base64.StdEncoding.EncodeToString([]byte(fixtureMiseToml)),
		})
	})
	dryRunSrv := httptest.NewServer(mux)
	defer dryRunSrv.Close()

	cmd := exec.Command(bin)
	cmd.Dir = repoDir
	summaryFile := filepath.Join(repoDir, "step-summary.md")
	cmd.Env = append(os.Environ(),
		"PATH="+fakeMiseDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_TOKEN=tok",
		"GITHUB_REPOSITORY=sgash708/e2e-example",
		"GITHUB_API_URL="+dryRunSrv.URL,
		"GITHUB_REF_NAME=main",
		"GITHUB_STEP_SUMMARY="+summaryFile,
		"INPUT_MISE_CONFIG_PATH=mise.toml",
		"INPUT_PR_STRATEGY=per-tool",
		"INPUT_DRY_RUN=true",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("binary exited with error: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "[dry-run] no pull requests were created") {
		t.Errorf("expected dry-run stderr message, got:\n%s", out)
	}

	summary, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("failed to read GITHUB_STEP_SUMMARY file: %v", err)
	}
	if !strings.Contains(string(summary), "2.13.2") {
		t.Errorf("expected job summary to preview the bumped version, got:\n%s", summary)
	}
}
