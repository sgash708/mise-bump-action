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
	// BranchPrefix, when non-empty, identifies this bump's tool independent
	// of version (e.g. "mise-bump/go-a1b2c3d4_"). Implementations use it to
	// find and close other open PRs for the same tool at an older version (a
	// newer bump supersedes them). Empty for grouped bumps, where no single
	// branch-name prefix identifies "this tool".
	BranchPrefix string
	// LegacyBranchNames lists branch names this exact bump (same tool, same
	// target version) would have used under naming schemes this action has
	// since replaced (see legacyBranchNames). Implementations must treat an
	// open or previously-closed PR under any of these exactly as they would
	// under BranchName, so renaming the scheme doesn't forget PRs opened
	// under the old one (ADR 0017).
	LegacyBranchNames []string
	// LegacyMatchName and LegacyMatchVersion identify this exact bump (short
	// tool name + target version) independent of PRTitle's exact wording.
	// Implementations use them, not a raw PRTitle comparison, to decide
	// whether a PR found under a LegacyBranchNames entry is this bump: the
	// title of a PR opened weeks ago also encodes the "from" version at the
	// time, which changes if the pin was bumped manually since, and gains or
	// loses an " in <path>" suffix if mise-config-path's entry count changed
	// — either would make an exact-title comparison miss a match that is
	// otherwise this exact tool at this exact target version, resurrecting a
	// bump that was already closed (ADR 0019). Empty for grouped bumps, same
	// as LegacyBranchNames.
	LegacyMatchName    string
	LegacyMatchVersion string
}

// ErrClosedPreviously is returned by GitHub.OpenBumpPR when a pull request
// for this exact bump (same branch name, i.e. same tool and target version)
// was previously closed without being merged. Matches Dependabot: closing a
// PR without merging means "don't reopen this exact version."
var ErrClosedPreviously = errors.New("a pull request for this bump was previously closed without merging; not reopening it")

// BranchNamespace prefixes every branch this action creates (see branchName),
// regardless of pr-strategy. githubapi uses it to recognize "our" pull
// requests when counting against config.Config.MaxOpenPRs (ADR 0016) —
// counting by config.Config.Labels instead would misattribute PRs from any
// other tool that happens to default to the same label (e.g. Dependabot's
// own default "dependencies" label).
const BranchNamespace = "mise-bump/"

// GitHub is the set of GitHub operations runner.Run needs. internal/githubapi
// provides the real implementation; tests use a moq-generated mock.
//
//go:generate moq -out mocks.go . GitHub
type GitHub interface {
	ReadFile(ctx context.Context, path, ref string) (content []byte, sha string, err error)
	// OpenBumpPR reports created=false (with no error) when a pull request
	// for in.BranchName (or one of in.LegacyBranchNames) was already open and
	// nothing new was written — callers must not count that toward
	// opened-count/pr-numbers, since nothing was newly opened this run
	// (ADR 0017).
	OpenBumpPR(ctx context.Context, in BumpPRInput) (prNumber int, created bool, err error)
	// CountOpenBumpPRs reports how many open pull requests into base were
	// opened by this action (branch name starting with BranchNamespace), for
	// enforcing config.Config.MaxOpenPRs.
	CountOpenBumpPRs(ctx context.Context, base string) (int, error)
	// HasOpenPRWithPrefix reports whether an open pull request into base
	// already exists whose branch starts with prefix (an older version of
	// the same tool). Run uses this so a bump that will replace a stale PR
	// (net zero open-PR count) doesn't get blocked by max-open-prs, which
	// would otherwise deadlock: the stale PR can only be closed by the
	// replacement bump succeeding, but max-open-prs would never let that
	// bump through while the stale PR still occupies a slot (ADR 0016).
	HasOpenPRWithPrefix(ctx context.Context, base, prefix string) (bool, error)
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
// group, attempting every group even if an earlier one fails. It returns
// the PR numbers opened, in processing order, and a combined error
// (errors.Join) for any groups that failed.
//
// Entries matching cfg.Ignore are dropped before grouping. When cfg.MaxOpenPRs
// is set and non-zero, groups are skipped once that many bump PRs (carrying
// cfg.Labels) are already open — existing open PRs count toward the cap,
// checked once at the start of the run.
//
// When cfg.DryRun is set, no branch/PR is created; each group's preview is
// written to out instead, and the returned PR numbers slice is empty.
func Run(ctx context.Context, cfg config.Config, entries []outdated.Entry, gh GitHub, out io.Writer) ([]int, error) {
	entries = filterIgnored(entries, cfg.Ignore, out)

	groups, err := grouping.Group(entries, cfg.PRStrategy)
	if err != nil {
		return nil, fmt.Errorf("failed to group outdated entries: %w", err)
	}

	multiConfig := len(cfg.MiseConfigPaths) > 1
	var prNumbers []int
	var errs []error

	openCount := 0
	if cfg.MaxOpenPRs > 0 && !cfg.DryRun {
		n, err := gh.CountOpenBumpPRs(ctx, cfg.BaseBranch)
		if err != nil {
			_, _ = fmt.Fprintf(out, "warning: failed to count open pull requests for max-open-prs (%v); not enforcing the limit this run\n", err)
		} else {
			openCount = n
		}
	}

	for _, group := range groups {
		// A bump that will replace an existing open PR for the same tool
		// (an older version) is net zero against max-open-prs: closing the
		// stale PR frees the slot the new one takes. It must bypass the gate
		// below rather than be counted against it, or a saturated cap would
		// permanently block the only thing that could free a slot.
		replacesExisting := false
		if prefix := branchPrefix(group.Entries); prefix != "" && cfg.MaxOpenPRs > 0 && !cfg.DryRun {
			has, err := gh.HasOpenPRWithPrefix(ctx, cfg.BaseBranch, prefix)
			if err != nil {
				_, _ = fmt.Fprintf(out, "warning: failed to check for an existing pull request to replace for %s (%v)\n", prefix, err)
			} else {
				replacesExisting = has
			}
		}

		if cfg.MaxOpenPRs > 0 && !cfg.DryRun && openCount >= cfg.MaxOpenPRs && !replacesExisting {
			_, _ = fmt.Fprintf(out, "## [skipped] %s\n\nmax-open-prs (%d) already reached; not opening more pull requests this run.\n\n", group.Entries[0].RelPath, cfg.MaxOpenPRs)
			continue
		}

		number, skipped, err := bumpGroup(ctx, cfg, group, multiConfig, gh, out)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if skipped || cfg.DryRun {
			continue
		}
		prNumbers = append(prNumbers, number)
		if !replacesExisting {
			openCount++
		}
	}

	if len(errs) > 0 {
		return prNumbers, errors.Join(errs...)
	}
	return prNumbers, nil
}

// filterIgnored removes entries whose Name matches any of ignore's patterns
// (an exact tool name, or a prefix ending in "*"), noting each exclusion in
// out.
func filterIgnored(entries []outdated.Entry, ignore []string, out io.Writer) []outdated.Entry {
	if len(ignore) == 0 {
		return entries
	}
	kept := make([]outdated.Entry, 0, len(entries))
	for _, e := range entries {
		if pattern, matched := matchesAny(e.Name, ignore); matched {
			_, _ = fmt.Fprintf(out, "## [ignored] %s\n\n`%s` matches the `ignore` input pattern `%s`; skipping.\n\n", e.RelPath, e.Name, pattern)
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

func matchesAny(name string, patterns []string) (pattern string, matched bool) {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(name, prefix) {
				return p, true
			}
		} else if name == p {
			return p, true
		}
	}
	return "", false
}

// bumpGroup reads the group's shared mise.toml, applies every entry's bump,
// renders PR text (with best-effort enrichment), and either opens the pull
// request or, in dry-run mode, writes a preview of it to out. skipped is
// true when there is nothing new to report this run: GitHub reports the
// exact bump was previously closed without merging (ErrClosedPreviously), a
// pull request for it was already open (OpenBumpPR's created=false), or
// every entry in the group had an unsupported value form.
func bumpGroup(ctx context.Context, cfg config.Config, group grouping.PRGroup, multiConfig bool, gh GitHub, out io.Writer) (number int, skipped bool, err error) {
	path := group.Entries[0].RelPath

	before, sha, err := gh.ReadFile(ctx, path, cfg.BaseBranch)
	if err != nil {
		return 0, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	after := before
	bumped := make([]outdated.Entry, 0, len(group.Entries))
	for _, e := range group.Entries {
		next, bumpErr := misetoml.Bump(after, e.Name, e.Requested, e.Latest)
		if errors.Is(bumpErr, misetoml.ErrUnsupportedValueForm) {
			_, _ = fmt.Fprintf(out, "## [skipped] %s\n\n`%s` uses a value format mise-bump-action can't rewrite in place (inline table or array); skipping. Use the `ignore` input to silence this.\n\n", path, e.Name)
			continue
		}
		if bumpErr != nil {
			return 0, false, fmt.Errorf("failed to bump %s in %s: %w", e.Name, path, bumpErr)
		}
		after = next
		bumped = append(bumped, e)
	}
	if len(bumped) == 0 {
		return 0, true, nil
	}

	fetchFullDetails := len(bumped) == 1
	enrichment := buildEnrichment(ctx, gh, bumped, fetchFullDetails)
	text := prtext.Build(bumped, multiConfig, enrichment)
	branch := branchName(bumped)

	if cfg.DryRun {
		writeDryRunPreview(out, path, branch, text, before, after)
		return 0, false, nil
	}

	number, created, err := gh.OpenBumpPR(ctx, BumpPRInput{
		BaseBranch:         cfg.BaseBranch,
		BranchName:         branch,
		BranchPrefix:       branchPrefix(bumped),
		LegacyBranchNames:  legacyBranchNames(bumped),
		LegacyMatchName:    legacyMatchName(bumped),
		LegacyMatchVersion: legacyMatchVersion(bumped),
		FilePath:           path,
		FileContent:        after,
		FileSHA:            sha,
		CommitMessage:      text.Commit,
		PRTitle:            text.Title,
		PRBody:             text.Body,
		Labels:             cfg.Labels,
	})
	if errors.Is(err, ErrClosedPreviously) {
		_, _ = fmt.Fprintf(out, "## [skipped] %s\n\nA pull request for **%s** was previously closed without merging; not reopening it.\n\n", path, text.Title)
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to open pull request for branch %s: %w", branch, err)
	}
	if !created {
		_, _ = fmt.Fprintf(out, "## [skipped] %s\n\nA pull request for **%s** is already open (#%d); nothing new to report.\n\n", path, text.Title, number)
		return 0, true, nil
	}
	return number, false, nil
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

// buildEnrichment fetches release notes/commits for entries backed by a
// resolvable GitHub repo; unresolvable or failed lookups are simply
// omitted. fetchFullDetails=false skips those API calls entirely,
// populating only RepoURL (see bumpGroup).
func buildEnrichment(ctx context.Context, gh GitHub, entries []outdated.Entry, fetchFullDetails bool) map[string]prtext.Enrichment {
	enrichment := make(map[string]prtext.Enrichment, len(entries))
	for _, e := range entries {
		repo, ok := reponame.FromToolName(e.Name)
		if !ok {
			continue
		}
		enr := prtext.Enrichment{RepoURL: "https://github.com/" + repo}
		if fetchFullDetails {
			if html, ok := gh.ReleaseNotesHTML(ctx, repo, e.Requested, e.Latest); ok {
				enr.ReleaseNotesHTML = html
			}
			if html, ok := gh.CommitsHTML(ctx, repo, e.Requested, e.Latest); ok {
				enr.CommitsHTML = html
			}
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
		// same branch name. nameFingerprint (see there) is what actually
		// guarantees no collision, since sanitize() alone is a lossy
		// many-to-one mapping.
		return fmt.Sprintf("%s%s-%s_%s", BranchNamespace, sanitize(e.Name), nameFingerprint(e.Name), sanitize(e.Latest))
	}

	h := fnv.New32a()
	for _, e := range entries {
		// hash.Hash.Write never returns an error; the check is unnecessary but
		// silences errcheck explicitly rather than ignoring it implicitly.
		_, _ = fmt.Fprintf(h, "%s@%s;", e.Name, e.Latest)
	}
	return fmt.Sprintf("%sbatch-%x", BranchNamespace, h.Sum32())
}

// branchPrefix returns the version-independent prefix of branchName's
// single-entry form (e.g. "mise-bump/go-a1b2c3d4_"), or "" for a grouped
// bump, where no single prefix identifies "this tool" across versions.
func branchPrefix(entries []outdated.Entry) string {
	if len(entries) != 1 {
		return ""
	}
	return fmt.Sprintf("%s%s-%s_", BranchNamespace, sanitize(entries[0].Name), nameFingerprint(entries[0].Name))
}

// legacyBranchNames returns the branch name this exact bump (same tool, same
// target version) would have used under naming schemes this action has
// since replaced, so a pull request opened or closed under an old scheme is
// still recognized after the scheme changes (ADR 0017) — without this,
// renaming the scheme would silently forget every PR opened under the old
// one, both resurrecting bumps ADR 0010 already closed and duplicating PRs
// that are still open. Empty for grouped bumps: their hash-based name has
// never changed.
func legacyBranchNames(entries []outdated.Entry) []string {
	if len(entries) != 1 {
		return nil
	}
	e := entries[0]
	return []string{
		// v1.0.0-v1.4.0
		fmt.Sprintf("%s%s-%s", BranchNamespace, sanitize(e.Name), sanitize(e.Latest)),
		// v1.5.0
		fmt.Sprintf("%s%s_%s", BranchNamespace, sanitize(e.Name), sanitize(e.Latest)),
	}
}

// legacyMatchName and legacyMatchVersion give BumpPRInput.LegacyMatchName/
// LegacyMatchVersion for a single-entry group; both empty for a grouped
// bump, since LegacyBranchNames is already empty there too (see
// legacyBranchNames).
func legacyMatchName(entries []outdated.Entry) string {
	if len(entries) != 1 {
		return ""
	}
	return prtext.ShortName(entries[0].Name)
}

func legacyMatchVersion(entries []outdated.Entry) string {
	if len(entries) != 1 {
		return ""
	}
	return entries[0].Latest
}

// nameFingerprint returns an 8-hex-character fingerprint of name, mixed into
// branchName/branchPrefix so they stay collision-free even though sanitize
// is a lossy many-to-one mapping — e.g. "go:github.com/foo/bar" and
// "go:github.com/foo-bar" both sanitize to "go-github.com-foo-bar". Two
// different tool names getting the same fingerprint is astronomically
// unlikely for the number of tools any single mise.toml realistically has
// (same trust model as branchName's batch hash above).
func nameFingerprint(name string) string {
	h := fnv.New32a()
	// hash.Hash.Write never returns an error; see the identical note above.
	_, _ = io.WriteString(h, name)
	return fmt.Sprintf("%08x", h.Sum32())
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
