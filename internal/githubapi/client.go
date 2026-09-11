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

// do issues one GitHub API request, retrying exactly once on a rate-limited
// (403 or 429) response that includes a Retry-After header (ADR 0008).
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

// OpenBumpPR is idempotent with respect to in.BranchName (see
// troubleshooting.md, "422 Reference already exists"): an existing open PR
// under in.BranchName or any of in.LegacyBranchNames short-circuits with no
// writes (created=false), a previously-closed-without-merging PR under any
// of those names returns runner.ErrClosedPreviously with no writes, a stale
// branch is deleted and recreated, and otherwise it creates the branch,
// commits, opens the PR, and applies in.Labels (created=true). When
// in.BranchPrefix is set, any other open PR sharing that prefix (an older
// version of the same tool) is closed with a comment pointing at the new PR.
//
// A match under one of in.LegacyBranchNames additionally requires the found
// PR's title to equal in.PRTitle. Legacy branch names are built from
// sanitize(name) alone, without the fingerprint the current scheme mixes in
// (see runner.legacyBranchNames) — two different tool names can sanitize to
// the identical legacy branch name, so a name-only match there can point at
// a different tool's PR entirely. The title (unchanged in format since
// v1.0.0) is the same string runner regenerates for this exact tool and
// version, so requiring it to match rejects that false positive (ADR 0018).
// This is a mitigation, not a guarantee: two different tools sharing both a
// mise.jdx.dev shortName and a target version could still collide. Legacy
// branch name support is planned for removal in the next major version
// (README "Upgrading"), at which point this whole path goes away.
func (c *Client) OpenBumpPR(ctx context.Context, in runner.BumpPRInput) (int, bool, error) {
	if number, _, exists, err := c.findOpenPR(ctx, in.BaseBranch, in.BranchName); err != nil {
		return 0, false, err
	} else if exists {
		return number, false, nil
	}
	for _, branch := range in.LegacyBranchNames {
		number, title, exists, err := c.findOpenPR(ctx, in.BaseBranch, branch)
		if err != nil {
			return 0, false, err
		}
		if exists && title == in.PRTitle {
			return number, false, nil
		}
	}

	if _, closed, err := c.findClosedUnmergedPR(ctx, in.BaseBranch, in.BranchName); err != nil {
		return 0, false, err
	} else if closed {
		return 0, false, runner.ErrClosedPreviously
	}
	for _, branch := range in.LegacyBranchNames {
		title, closed, err := c.findClosedUnmergedPR(ctx, in.BaseBranch, branch)
		if err != nil {
			return 0, false, err
		}
		if closed && title == in.PRTitle {
			return 0, false, runner.ErrClosedPreviously
		}
	}

	baseSHA, err := c.getRefSHA(ctx, in.BaseBranch)
	if err != nil {
		return 0, false, err
	}

	if _, exists, err := c.branchSHA(ctx, in.BranchName); err != nil {
		return 0, false, err
	} else if exists {
		if err := c.deleteRef(ctx, in.BranchName); err != nil {
			return 0, false, err
		}
	}

	if err := c.createRef(ctx, in.BranchName, baseSHA); err != nil {
		return 0, false, err
	}
	if err := c.putFile(ctx, in.FilePath, in.CommitMessage, in.FileContent, in.FileSHA, in.BranchName); err != nil {
		return 0, false, err
	}
	number, err := c.createPullRequest(ctx, in.PRTitle, in.PRBody, in.BranchName, in.BaseBranch)
	if err != nil {
		return 0, false, err
	}
	if err := c.addLabels(ctx, number, in.Labels); err != nil {
		return 0, false, err
	}
	if in.BranchPrefix != "" {
		c.closeSupersededPRs(ctx, in.BaseBranch, in.BranchPrefix, in.BranchName, number)
	}
	return number, true, nil
}

// findOpenPR looks for an already-open pull request from branch into base.
// title is that PR's title, used by callers matching a legacy branch name to
// reject a name collision with a different tool (see OpenBumpPR).
func (c *Client) findOpenPR(ctx context.Context, base, branch string) (number int, title string, exists bool, err error) {
	owner, _, _ := strings.Cut(c.repo, "/")
	var out []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=open&base=%s&head=%s:%s",
		c.repo, url.QueryEscape(base), url.QueryEscape(owner), url.QueryEscape(branch))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return 0, "", false, fmt.Errorf("failed to check for an existing pull request for branch %s: %w", branch, err)
	}
	if len(out) == 0 {
		return 0, "", false, nil
	}
	return out[0].Number, out[0].Title, true, nil
}

// findClosedUnmergedPR reports whether a pull request from branch into base
// was previously closed without being merged (runner.ErrClosedPreviously).
// title is that PR's title; see findOpenPR.
func (c *Client) findClosedUnmergedPR(ctx context.Context, base, branch string) (title string, closed bool, err error) {
	owner, _, _ := strings.Cut(c.repo, "/")
	var out []struct {
		Title    string  `json:"title"`
		MergedAt *string `json:"merged_at"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=closed&base=%s&head=%s:%s",
		c.repo, url.QueryEscape(base), url.QueryEscape(owner), url.QueryEscape(branch))
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", false, fmt.Errorf("failed to check for a previously closed pull request for branch %s: %w", branch, err)
	}
	for _, pr := range out {
		if pr.MergedAt == nil {
			return pr.Title, true, nil
		}
	}
	return "", false, nil
}

// closeSupersededPRs closes every other open PR into base whose branch
// starts with prefix (an older version of the same tool as excludeBranch),
// commenting that it was superseded by newNumber. Best-effort: the new PR
// (already open at this point) is not affected by any failure here.
func (c *Client) closeSupersededPRs(ctx context.Context, base, prefix, excludeBranch string, newNumber int) {
	out, err := c.listOpenPRRefs(ctx, base)
	if err != nil {
		return
	}
	for _, pr := range out {
		if pr.Head.Ref == excludeBranch || !strings.HasPrefix(pr.Head.Ref, prefix) {
			continue
		}
		_ = c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", c.repo, pr.Number), map[string]string{"state": "closed"}, nil)
		comment := map[string]string{"body": fmt.Sprintf("Superseded by #%d.", newNumber)}
		_ = c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", c.repo, pr.Number), comment, nil)
	}
}

// CountOpenBumpPRs reports how many open pull requests into base this action
// opened (branch name starting with runner.BranchNamespace). Matching by
// branch name rather than by label avoids miscounting another tool's PRs
// that happen to carry the same default label — e.g. Dependabot also
// defaults to "dependencies" (ADR 0016).
func (c *Client) CountOpenBumpPRs(ctx context.Context, base string) (int, error) {
	prs, err := c.listOpenPRRefs(ctx, base)
	if err != nil {
		return 0, fmt.Errorf("failed to count open pull requests: %w", err)
	}
	count := 0
	for _, pr := range prs {
		if strings.HasPrefix(pr.Head.Ref, runner.BranchNamespace) {
			count++
		}
	}
	return count, nil
}

// HasOpenPRWithPrefix reports whether an open pull request into base has a
// branch starting with prefix.
func (c *Client) HasOpenPRWithPrefix(ctx context.Context, base, prefix string) (bool, error) {
	prs, err := c.listOpenPRRefs(ctx, base)
	if err != nil {
		return false, fmt.Errorf("failed to check for an existing pull request with prefix %s: %w", prefix, err)
	}
	for _, pr := range prs {
		if strings.HasPrefix(pr.Head.Ref, prefix) {
			return true, nil
		}
	}
	return false, nil
}

type openPRRef struct {
	Number int       `json:"number"`
	Head   prHeadRef `json:"head"`
}

// prHeadRef is named (rather than an inline anonymous struct) purely so test
// code can construct an openPRRef literal without repeating the anonymous
// struct's shape at every call site.
type prHeadRef struct {
	Ref string `json:"ref"`
}

// maxOpenPRPages bounds listOpenPRRefs's pagination loop (100 PRs/page, so
// 1000 pages is 100,000 open PRs — far beyond any real repository). It exists
// only so a malfunctioning API/mock can't spin the loop forever.
const maxOpenPRPages = 1000

// listOpenPRRefs lists every open pull request into base along with its
// head branch name, shared by CountOpenBumpPRs, HasOpenPRWithPrefix, and
// closeSupersededPRs. Paginates through every page GitHub returns (a repo
// with more than 100 open PRs into base would otherwise silently see only
// the first 100, undercounting max-open-prs and missing superseded PRs to
// close, ADR 0017).
func (c *Client) listOpenPRRefs(ctx context.Context, base string) ([]openPRRef, error) {
	var all []openPRRef
	for page := 1; page <= maxOpenPRPages; page++ {
		var out []openPRRef
		path := fmt.Sprintf("/repos/%s/pulls?state=open&base=%s&per_page=100&page=%d", c.repo, url.QueryEscape(base), page)
		if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out...)
		if len(out) < 100 {
			break
		}
	}
	return all, nil
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
