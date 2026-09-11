// Package runner orchestrates turning outdated mise-managed tools into
// Dependabot-style pull requests: grouping, rendering PR text, rewriting
// mise.toml, and delegating the actual git/GitHub operations to a GitHub
// implementation (see internal/githubapi).
package runner

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"strings"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/grouping"
	"github.com/sgash708/mise-bump-action/internal/misetoml"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/prtext"
	"github.com/sgash708/mise-bump-action/internal/reponame"
)

// BumpPRInput is everything needed to write one commit to a new branch and
// open a pull request from it.
type BumpPRInput struct {
	BaseBranch    string
	BranchName    string
	FilePath      string
	FileContent   []byte
	FileSHA       string
	CommitMessage string
	PRTitle       string
	PRBody        string
	Labels        []string
}

// GitHub is the set of GitHub operations runner.Run needs. internal/githubapi
// provides the real implementation; tests use a moq-generated mock.
//
//go:generate moq -out mocks.go . GitHub
type GitHub interface {
	ReadFile(ctx context.Context, path, ref string) (content []byte, sha string, err error)
	OpenBumpPR(ctx context.Context, in BumpPRInput) (prNumber int, err error)
	// ReleaseNotesHTML and CommitsHTML enrich a bump's PR body with the
	// target tool's own release notes/commit history, matching Dependabot's
	// format. Implementations return ok=false when enrichment isn't
	// available (e.g. the tool isn't backed by a single GitHub repo, or the
	// GitHub API call fails); callers must treat that as "omit," never as a
	// fatal error.
	ReleaseNotesHTML(ctx context.Context, repo, fromVersion, toVersion string) (html string, ok bool)
	CommitsHTML(ctx context.Context, repo, fromVersion, toVersion string) (html string, ok bool)
}

// Run groups entries per cfg.PRStrategy and opens one pull request per
// group. Every group is attempted even if an earlier one fails (e.g. a
// transient GitHub API error), so one bad group can't prevent unrelated
// tools from being bumped. It returns the pull request numbers successfully
// opened, in the order their groups were processed, alongside a combined
// error (via errors.Join) for any groups that failed. The returned error is
// nil only if every group succeeded.
//
// When cfg.DryRun is set, no branch/pull request is created for any group;
// instead each group's intended title, body, and file diff are written to
// out, and the returned PR numbers slice is always empty.
func Run(ctx context.Context, cfg config.Config, entries []outdated.Entry, gh GitHub, out io.Writer) ([]int, error) {
	groups, err := grouping.Group(entries, cfg.PRStrategy)
	if err != nil {
		return nil, fmt.Errorf("failed to group outdated entries: %w", err)
	}

	multiConfig := len(cfg.MiseConfigPaths) > 1
	var prNumbers []int
	var errs []error

	for _, group := range groups {
		number, err := bumpGroup(ctx, cfg, group, multiConfig, gh, out)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !cfg.DryRun {
			prNumbers = append(prNumbers, number)
		}
	}

	if len(errs) > 0 {
		return prNumbers, errors.Join(errs...)
	}
	return prNumbers, nil
}

// bumpGroup reads the group's shared mise.toml, applies every entry's bump,
// renders PR text (with best-effort enrichment), and either opens the pull
// request or, in dry-run mode, writes a preview of it to out.
func bumpGroup(ctx context.Context, cfg config.Config, group grouping.PRGroup, multiConfig bool, gh GitHub, out io.Writer) (int, error) {
	path := group.Entries[0].RelPath

	before, sha, err := gh.ReadFile(ctx, path, cfg.BaseBranch)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s: %w", path, err)
	}

	after := before
	for _, e := range group.Entries {
		after, err = misetoml.Bump(after, e.Name, e.Requested, e.Latest)
		if err != nil {
			return 0, fmt.Errorf("failed to bump %s in %s: %w", e.Name, path, err)
		}
	}

	enrichment := buildEnrichment(ctx, gh, group.Entries)
	text := prtext.Build(group.Entries, multiConfig, enrichment)
	branch := branchName(group.Entries)

	if cfg.DryRun {
		writeDryRunPreview(out, path, branch, text, before, after)
		return 0, nil
	}

	number, err := gh.OpenBumpPR(ctx, BumpPRInput{
		BaseBranch:    cfg.BaseBranch,
		BranchName:    branch,
		FilePath:      path,
		FileContent:   after,
		FileSHA:       sha,
		CommitMessage: text.Commit,
		PRTitle:       text.Title,
		PRBody:        text.Body,
		Labels:        cfg.Labels,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to open pull request for branch %s: %w", branch, err)
	}
	return number, nil
}

// writeDryRunPreview renders what bumpGroup would have opened as a pull
// request, formatted for $GITHUB_STEP_SUMMARY (rendered as Markdown in the
// job summary UI).
func writeDryRunPreview(out io.Writer, path, branch string, text prtext.Content, before, after []byte) {
	_, _ = fmt.Fprintf(out, "## [dry-run] %s\n\n", path)
	_, _ = fmt.Fprintf(out, "**Branch:** `%s`\n\n", branch)
	_, _ = fmt.Fprintf(out, "**Title:** %s\n\n", text.Title)
	_, _ = fmt.Fprintf(out, "%s\n\n", text.Body)
	_, _ = fmt.Fprintf(out, "```diff\n%s```\n\n", lineDiff(before, after))
}

// lineDiff renders a minimal diff between before and after. misetoml.Bump
// only ever replaces a version substring within an existing line — it never
// inserts or removes lines — so before and after always have the same line
// count, and an index-aligned comparison is a correct diff (not just an
// approximation) for this specific domain.
func lineDiff(before, after []byte) string {
	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")

	var b strings.Builder
	for i, line := range beforeLines {
		if i < len(afterLines) && line != afterLines[i] {
			fmt.Fprintf(&b, "-%s\n+%s\n", line, afterLines[i])
		}
	}
	return b.String()
}

// buildEnrichment fetches release notes/commits for each entry backed by a
// resolvable GitHub repo. Entries with no resolvable repo, or for which
// enrichment fetching fails, are simply absent from the result — enrichment
// is best-effort and never blocks the bump.
func buildEnrichment(ctx context.Context, gh GitHub, entries []outdated.Entry) map[string]prtext.Enrichment {
	enrichment := make(map[string]prtext.Enrichment, len(entries))
	for _, e := range entries {
		repo, ok := reponame.FromToolName(e.Name)
		if !ok {
			continue
		}
		enr := prtext.Enrichment{RepoURL: "https://github.com/" + repo}
		if html, ok := gh.ReleaseNotesHTML(ctx, repo, e.Requested, e.Latest); ok {
			enr.ReleaseNotesHTML = html
		}
		if html, ok := gh.CommitsHTML(ctx, repo, e.Requested, e.Latest); ok {
			enr.CommitsHTML = html
		}
		enrichment[e.Name] = enr
	}
	return enrichment
}

// branchName derives a deterministic branch name from a group's entries, so
// reruns against the same outdated versions target the same branch instead
// of piling up duplicate branches/PRs.
func branchName(entries []outdated.Entry) string {
	if len(entries) == 1 {
		e := entries[0]
		// The full tool name (not just its trailing path segment) is used so
		// that different backends sharing a segment — e.g. "aqua:foo/cli" and
		// "go:github.com/bar/cli" both end in "/cli" — don't collide into the
		// same branch name.
		return fmt.Sprintf("mise-bump/%s-%s", sanitize(e.Name), sanitize(e.Latest))
	}

	h := fnv.New32a()
	for _, e := range entries {
		// hash.Hash.Write never returns an error; the check is unnecessary but
		// silences errcheck explicitly rather than ignoring it implicitly.
		_, _ = fmt.Fprintf(h, "%s@%s;", e.Name, e.Latest)
	}
	return fmt.Sprintf("mise-bump/batch-%x", h.Sum32())
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
