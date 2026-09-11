// Package githubapi implements runner.GitHub against the real GitHub REST
// API using only the standard library (ADR 0002: no third-party PR-creation
// action, no external Go HTTP client dependency).
package githubapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sgash708/mise-bump-action/internal/runner"
)

// statusError carries the HTTP status code from a failed GitHub API call so
// callers can distinguish "not found" (expected, e.g. checking whether a
// branch exists yet) from genuine errors.
type statusError struct {
	method, path string
	status       int
	body         string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("github api %s %s returned %d: %s", e.method, e.path, e.status, e.body)
}

func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.status == http.StatusNotFound
}

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

// do issues one GitHub API request, retrying exactly once if the response is
// a rate limit (403 or 429) that includes a Retry-After header — GitHub
// returns 403 (not just 429) for both the secondary rate limit and abuse
// detection mechanisms. A single retry is enough for the transient limits
// this action realistically hits (a handful of API calls per run); anything
// beyond that is treated as a genuine failure rather than retried
// indefinitely.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body for %s %s: %w", method, path, err)
		}
		bodyBytes = b
	}

	for attempt := 0; ; attempt++ {
		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
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

		if attempt == 0 && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) {
			if wait, ok := retryAfter(resp); ok {
				_ = resp.Body.Close()
				select {
				case <-time.After(wait):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return &statusError{method: method, path: path, status: resp.StatusCode, body: string(b)}
		}
		if out != nil {
			err := json.NewDecoder(resp.Body).Decode(out)
			_ = resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to decode response from %s %s: %w", method, path, err)
			}
			return nil
		}
		_ = resp.Body.Close()
		return nil
	}
}

// retryAfter reports how long to wait before retrying, based on the
// response's Retry-After header (seconds). ok is false if the header is
// absent or unparseable, meaning the caller should not retry.
func retryAfter(resp *http.Response) (wait time.Duration, ok bool) {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(v)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// escapePathSegments URL-escapes each "/"-separated segment of p
// individually, so a literal "/" in p keeps its meaning as a path separator
// while any other special character (spaces, "#", non-ASCII, etc.) within a
// segment is safely encoded.
func escapePathSegments(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// ReadFile fetches the current content and blob SHA of path at ref via the
// GitHub Contents API.
func (c *Client) ReadFile(ctx context.Context, path, ref string) ([]byte, string, error) {
	var out struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	reqPath := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", c.repo, escapePathSegments(path), url.QueryEscape(ref))
	if err := c.do(ctx, http.MethodGet, reqPath, nil, &out); err != nil {
		return nil, "", fmt.Errorf("failed to read file %s at ref %s: %w", path, ref, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode content of %s: %w", path, err)
	}
	return decoded, out.SHA, nil
}

// OpenBumpPR is idempotent with respect to in.BranchName:
//
//   - If a pull request is already open from in.BranchName into
//     in.BaseBranch, its number is returned immediately with no further
//     writes. This is what keeps a rerun against still-open PRs from
//     erroring with "422 Reference already exists" instead of silently
//     succeeding.
//   - Otherwise, if in.BranchName exists without an open PR (e.g. left over
//     from a run that failed after creating the branch but before opening
//     the PR), the stale branch is deleted and recreated from the current
//     in.BaseBranch so its content isn't stale.
//   - Then it creates the branch, commits in.FileContent to in.FilePath,
//     opens a pull request, and applies in.Labels.
func (c *Client) OpenBumpPR(ctx context.Context, in runner.BumpPRInput) (int, error) {
	if number, exists, err := c.findOpenPR(ctx, in.BaseBranch, in.BranchName); err != nil {
		return 0, err
	} else if exists {
		return number, nil
	}

	baseSHA, err := c.getRefSHA(ctx, in.BaseBranch)
	if err != nil {
		return 0, err
	}

	if _, exists, err := c.branchSHA(ctx, in.BranchName); err != nil {
		return 0, err
	} else if exists {
		if err := c.deleteRef(ctx, in.BranchName); err != nil {
			return 0, err
		}
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

// findOpenPR looks for an already-open pull request from branch into base.
func (c *Client) findOpenPR(ctx context.Context, base, branch string) (number int, exists bool, err error) {
	owner, _, _ := strings.Cut(c.repo, "/")
	var out []struct {
		Number int `json:"number"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=open&base=%s&head=%s:%s",
		c.repo, url.QueryEscape(base), url.QueryEscape(owner), url.QueryEscape(branch))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return 0, false, fmt.Errorf("failed to check for an existing pull request for branch %s: %w", branch, err)
	}
	if len(out) == 0 {
		return 0, false, nil
	}
	return out[0].Number, true, nil
}

// branchSHA returns the branch's current commit SHA, or exists=false if the
// branch doesn't exist (a 404 from GitHub, not an error here).
func (c *Client) branchSHA(ctx context.Context, branch string) (sha string, exists bool, err error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err = c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/ref/heads/%s", c.repo, escapePathSegments(branch)), nil, &out)
	if isNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to check branch %s: %w", branch, err)
	}
	return out.Object.SHA, true, nil
}

func (c *Client) deleteRef(ctx context.Context, branch string) error {
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/git/refs/heads/%s", c.repo, escapePathSegments(branch)), nil, nil); err != nil {
		return fmt.Errorf("failed to delete stale branch %s: %w", branch, err)
	}
	return nil
}

func (c *Client) getRefSHA(ctx context.Context, branch string) (string, error) {
	sha, exists, err := c.branchSHA(ctx, branch)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("base branch %s not found", branch)
	}
	return sha, nil
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
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/contents/%s", c.repo, escapePathSegments(path)), body, nil); err != nil {
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
