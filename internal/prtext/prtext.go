// Package prtext builds pull request titles, bodies, and commit messages
// that follow Dependabot's own conventions (ADR 0003), so reviewers see
// familiar output.
package prtext

import (
	"fmt"
	"strings"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

// Content is the text used to open a pull request: its title, its body, and
// the commit message applied to the branch (including the Dependabot-style
// updated-dependencies trailer).
type Content struct {
	Title  string
	Body   string
	Commit string
}

// Build renders Content for a group of outdated entries. multiConfig should
// be true when the action is configured with more than one mise-config-path,
// so single-entry titles disambiguate which file changed.
func Build(entries []outdated.Entry, multiConfig bool) Content {
	if len(entries) == 1 {
		return buildSingle(entries[0], multiConfig)
	}
	return buildGrouped(entries)
}

func buildSingle(e outdated.Entry, multiConfig bool) Content {
	title := fmt.Sprintf("chore(deps): bump %s from %s to %s", shortName(e.Name), e.Requested, e.Latest)
	if multiConfig {
		title += fmt.Sprintf(" in %s", e.RelPath)
	}

	body := fmt.Sprintf("Bumps `%s` from `%s` to `%s`.", shortName(e.Name), e.Requested, e.Latest)
	commit := title + "\n\n---\n" + buildTrailer([]outdated.Entry{e})

	return Content{Title: title, Body: body, Commit: commit}
}

func buildGrouped(entries []outdated.Entry) Content {
	title := fmt.Sprintf("chore(deps): bump %d mise-managed tools", len(entries))

	bodyLines := make([]string, len(entries))
	for i, e := range entries {
		bodyLines[i] = fmt.Sprintf("- Bumps `%s` from `%s` to `%s`.", shortName(e.Name), e.Requested, e.Latest)
	}
	body := strings.Join(bodyLines, "\n")
	commit := title + "\n\n---\n" + buildTrailer(entries)

	return Content{Title: title, Body: body, Commit: commit}
}

func buildTrailer(entries []outdated.Entry) string {
	var b strings.Builder
	b.WriteString("updated-dependencies:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- dependency-name: %s\n  dependency-version: %s\n  dependency-type: direct:production\n", shortName(e.Name), e.Latest)
	}
	b.WriteString("...")
	return b.String()
}

// shortName strips the mise backend prefix (e.g. "aqua:owner/repo" or
// "go:module/path") down to the trailing path segment, so PR text reads
// naturally (e.g. "golangci-lint" instead of "aqua:golangci/golangci-lint").
func shortName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		return name[idx+1:]
	}
	return name
}
