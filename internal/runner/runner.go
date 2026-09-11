// Package runner orchestrates turning outdated mise-managed tools into
// Dependabot-style pull requests: grouping, rendering PR text, rewriting
// mise.toml, and delegating the actual git/GitHub operations to a GitHub
// implementation (see internal/githubapi).
package runner

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/grouping"
	"github.com/sgash708/mise-bump-action/internal/misetoml"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/prtext"
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
}

// Run groups entries per cfg.PRStrategy and opens one pull request per group.
// It returns the created pull request numbers in the order groups were
// processed. On error, it returns the pull requests successfully opened so
// far alongside the error.
func Run(ctx context.Context, cfg config.Config, entries []outdated.Entry, gh GitHub) ([]int, error) {
	groups, err := grouping.Group(entries, grouping.Strategy(cfg.PRStrategy))
	if err != nil {
		return nil, fmt.Errorf("failed to group outdated entries: %w", err)
	}

	multiConfig := len(cfg.MiseConfigPaths) > 1
	var prNumbers []int

	for _, group := range groups {
		path := group.Entries[0].RelPath

		content, sha, err := gh.ReadFile(ctx, path, cfg.BaseBranch)
		if err != nil {
			return prNumbers, fmt.Errorf("failed to read %s: %w", path, err)
		}

		for _, e := range group.Entries {
			content, err = misetoml.Bump(content, e.Name, e.Requested, e.Latest)
			if err != nil {
				return prNumbers, fmt.Errorf("failed to bump %s in %s: %w", e.Name, path, err)
			}
		}

		text := prtext.Build(group.Entries, multiConfig)
		branch := branchName(group.Entries)

		number, err := gh.OpenBumpPR(ctx, BumpPRInput{
			BaseBranch:    cfg.BaseBranch,
			BranchName:    branch,
			FilePath:      path,
			FileContent:   content,
			FileSHA:       sha,
			CommitMessage: text.Commit,
			PRTitle:       text.Title,
			PRBody:        text.Body,
			Labels:        cfg.Labels,
		})
		if err != nil {
			return prNumbers, fmt.Errorf("failed to open pull request for branch %s: %w", branch, err)
		}
		prNumbers = append(prNumbers, number)
	}

	return prNumbers, nil
}

// branchName derives a deterministic branch name from a group's entries, so
// reruns against the same outdated versions target the same branch instead
// of piling up duplicate branches/PRs.
func branchName(entries []outdated.Entry) string {
	if len(entries) == 1 {
		e := entries[0]
		return fmt.Sprintf("mise-bump/%s-%s", sanitize(shortNameForBranch(e.Name)), sanitize(e.Latest))
	}

	h := fnv.New32a()
	for _, e := range entries {
		// hash.Hash.Write never returns an error; the check is unnecessary but
		// silences errcheck explicitly rather than ignoring it implicitly.
		_, _ = fmt.Fprintf(h, "%s@%s;", e.Name, e.Latest)
	}
	return fmt.Sprintf("mise-bump/batch-%x", h.Sum32())
}

func shortNameForBranch(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
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
