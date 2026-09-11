// Package githubapi implements runner.GitHub against the real GitHub REST
// API using only the standard library (ADR 0002: no third-party PR-creation
// action, no external Go HTTP client dependency).
package githubapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sgash708/mise-bump-action/internal/runner"
)

// Client implements runner.GitHub.
type Client struct {
	httpClient *http.Client
	apiURL     string
	token      string
	repo       string
}

var _ runner.GitHub = (*Client)(nil)

// NewClient builds a Client for repo (in "owner/repo" form) authenticated
// with token. apiURL is normally https://api.github.com; tests pass an
// httptest.Server URL instead.
func NewClient(httpClient *http.Client, apiURL, token, repo string) *Client {
	return &Client{
		httpClient: httpClient,
		apiURL:     strings.TrimSuffix(apiURL, "/"),
		token:      token,
		repo:       repo,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body for %s %s: %w", method, path, err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to build request for %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call github api %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api %s %s returned %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

// ReadFile fetches the current content and blob SHA of path at ref via the
// GitHub Contents API.
func (c *Client) ReadFile(ctx context.Context, path, ref string) ([]byte, string, error) {
	var out struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", c.repo, path, ref), nil, &out); err != nil {
		return nil, "", fmt.Errorf("failed to read file %s at ref %s: %w", path, ref, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode content of %s: %w", path, err)
	}
	return decoded, out.SHA, nil
}

// OpenBumpPR creates a branch from in.BaseBranch, commits in.FileContent to
// in.FilePath on that branch, opens a pull request, and applies in.Labels. It
// returns the created pull request number.
func (c *Client) OpenBumpPR(ctx context.Context, in runner.BumpPRInput) (int, error) {
	baseSHA, err := c.getRefSHA(ctx, in.BaseBranch)
	if err != nil {
		return 0, err
	}
	if err := c.createRef(ctx, in.BranchName, baseSHA); err != nil {
		return 0, err
	}
	if err := c.putFile(ctx, in.FilePath, in.CommitMessage, in.FileContent, in.FileSHA, in.BranchName); err != nil {
		return 0, err
	}
	number, err := c.createPullRequest(ctx, in.PRTitle, in.PRBody, in.BranchName, in.BaseBranch)
	if err != nil {
		return 0, err
	}
	if err := c.addLabels(ctx, number, in.Labels); err != nil {
		return 0, err
	}
	return number, nil
}

func (c *Client) getRefSHA(ctx context.Context, branch string) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/ref/heads/%s", c.repo, branch), nil, &out); err != nil {
		return "", fmt.Errorf("failed to get ref sha for branch %s: %w", branch, err)
	}
	return out.Object.SHA, nil
}

func (c *Client) createRef(ctx context.Context, branch, sha string) error {
	body := map[string]string{"ref": "refs/heads/" + branch, "sha": sha}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/git/refs", c.repo), body, nil); err != nil {
		return fmt.Errorf("failed to create ref for branch %s: %w", branch, err)
	}
	return nil
}

func (c *Client) putFile(ctx context.Context, path, message string, content []byte, sha, branch string) error {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"sha":     sha,
		"branch":  branch,
	}
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/contents/%s", c.repo, path), body, nil); err != nil {
		return fmt.Errorf("failed to update file %s on branch %s: %w", path, branch, err)
	}
	return nil
}

func (c *Client) createPullRequest(ctx context.Context, title, body, head, base string) (int, error) {
	reqBody := map[string]string{"title": title, "body": body, "head": head, "base": base}
	var out struct {
		Number int `json:"number"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls", c.repo), reqBody, &out); err != nil {
		return 0, fmt.Errorf("failed to create pull request for head %s: %w", head, err)
	}
	return out.Number, nil
}

func (c *Client) addLabels(ctx context.Context, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	body := map[string][]string{"labels": labels}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/labels", c.repo, number), body, nil); err != nil {
		return fmt.Errorf("failed to add labels to pull request #%d: %w", number, err)
	}
	return nil
}
