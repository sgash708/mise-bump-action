// Package prtext builds pull request titles, bodies, and commit messages
// that follow Dependabot's own conventions (ADR 0003), so reviewers see
// familiar output.
package prtext

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

// maxBodyBytes is GitHub's own limit on issue/pull-request body length.
// Enrichment (upstream release notes/commit lists) can occasionally exceed
// it for a tool with a very long changelog, which would otherwise turn a
// successful bump into a 422 from the GitHub API.
const maxBodyBytes = 65536

const truncationNotice = "\n\n...(truncated — see the tool's own releases page for the full changelog)"

// Content is the text used to open a pull request: its title, its body, and
// the commit message applied to the branch (including the Dependabot-style
// updated-dependencies trailer).
type Content struct {
	Title  string
	Body   string
	Commit string
}

// Enrichment holds pre-fetched supplementary content for a single outdated
// entry, keyed by outdated.Entry.Name in the map passed to Build. Any zero
// field is simply omitted from the rendered body — enrichment is optional
// and best-effort.
type Enrichment struct {
	RepoURL          string
	ReleaseNotesHTML string
	CommitsHTML      string
}

// Build renders Content for a group of outdated entries. multiConfig should
// be true when the action is configured with more than one mise-config-path,
// so single-entry titles disambiguate which file changed. enrichment may be
// nil; entries with no corresponding map entry fall back to a plain
// backtick-quoted bumps line.
func Build(entries []outdated.Entry, multiConfig bool, enrichment map[string]Enrichment) Content {
	var content Content
	if len(entries) == 1 {
		content = buildSingle(entries[0], multiConfig, enrichment)
	} else {
		content = buildGrouped(entries, enrichment)
	}
	content.Body = truncateBody(content.Body)
	return content
}

// truncateBody keeps body under maxBodyBytes, cutting at a rune boundary and
// appending truncationNotice so it's clear content was cut, not corrupted.
func truncateBody(body string) string {
	if len(body) <= maxBodyBytes {
		return body
	}
	keep := maxBodyBytes - len(truncationNotice)
	if keep < 0 {
		keep = 0
	}
	for keep > 0 && !utf8.RuneStart(body[keep]) {
		keep--
	}
	return body[:keep] + truncationNotice
}

func buildSingle(e outdated.Entry, multiConfig bool, enrichment map[string]Enrichment) Content {
	title := fmt.Sprintf("chore(deps): bump %s from %s to %s", ShortName(e.Name), e.Requested, e.Latest)
	if multiConfig {
		title += fmt.Sprintf(" in %s", e.RelPath)
	}

	var b strings.Builder
	b.WriteString(bumpsLine(e, enrichment[e.Name]))
	if enr, ok := enrichment[e.Name]; ok {
		if enr.ReleaseNotesHTML != "" {
			b.WriteString("\n")
			b.WriteString(enr.ReleaseNotesHTML)
		}
		if enr.CommitsHTML != "" {
			b.WriteString("\n")
			b.WriteString(enr.CommitsHTML)
		}
	}
	body := b.String()

	commit := title + "\n\n---\n" + buildTrailer([]outdated.Entry{e})

	return Content{Title: title, Body: body, Commit: commit}
}

func buildGrouped(entries []outdated.Entry, enrichment map[string]Enrichment) Content {
	title := fmt.Sprintf("chore(deps): bump %d mise-managed tools", len(entries))

	bodyLines := make([]string, len(entries))
	for i, e := range entries {
		bodyLines[i] = "- " + bumpsLine(e, enrichment[e.Name])
	}
	body := strings.Join(bodyLines, "\n")
	commit := title + "\n\n---\n" + buildTrailer(entries)

	return Content{Title: title, Body: body, Commit: commit}
}

// bumpsLine renders the "Bumps X from A to B." line: a markdown link to the
// tool's repo when enr.RepoURL is available (matching Dependabot's own
// format), otherwise a plain backtick-quoted fallback.
func bumpsLine(e outdated.Entry, enr Enrichment) string {
	if enr.RepoURL == "" {
		return fmt.Sprintf("Bumps `%s` from `%s` to `%s`.", ShortName(e.Name), e.Requested, e.Latest)
	}
	return fmt.Sprintf("Bumps [%s](%s) from %s to %s.", ShortName(e.Name), enr.RepoURL, e.Requested, e.Latest)
}

func buildTrailer(entries []outdated.Entry) string {
	var b strings.Builder
	b.WriteString("updated-dependencies:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- dependency-name: %s\n  dependency-version: %s\n  dependency-type: direct:production\n", ShortName(e.Name), e.Latest)
	}
	b.WriteString("...")
	return b.String()
}

// ShortName strips the mise backend prefix (e.g. "aqua:owner/repo" or
// "go:module/path") down to the trailing path segment, so PR text reads
// naturally (e.g. "golangci-lint" instead of "aqua:golangci/golangci-lint").
// Exported so runner can derive the same identifier for legacy-PR title
// matching (see runner.legacyMatchName).
func ShortName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		return name[idx+1:]
	}
	return name
}
