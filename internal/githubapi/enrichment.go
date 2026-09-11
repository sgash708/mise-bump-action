package githubapi

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
)

type releaseEntry struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// ReleaseNotesHTML renders a Dependabot-style "Release notes" <details>
// block for repo (any public "owner/repo"), covering releases strictly
// after fromVersion up to and including toVersion. It returns ok=false if
// repo has no releases, toVersion can't be found among the first page of
// releases, or the request fails — release-notes enrichment is best-effort
// and must never fail the overall bump. It never panics: GitHub's /releases
// list is ordered by creation time, not by version, so a backport can put
// fromVersion at a lower index than toVersion; that case degrades to "from
// not usefully found" rather than panicking on an inverted slice range.
func (c *Client) ReleaseNotesHTML(ctx context.Context, repo, fromVersion, toVersion string) (result string, ok bool) {
	defer func() {
		if recover() != nil {
			result, ok = "", false
		}
	}()

	var raw []releaseEntry
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/releases?per_page=100", repo), nil, &raw); err != nil {
		return "", false
	}

	releases := make([]releaseEntry, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		releases = append(releases, r)
	}

	toIdx := indexByTag(releases, toVersion)
	if toIdx == -1 {
		return "", false
	}
	fromIdx := indexByTag(releases, fromVersion)
	end := len(releases)
	if fromIdx != -1 && fromIdx > toIdx {
		end = fromIdx
	}
	inRange := releases[toIdx:end]
	if len(inRange) == 0 {
		return "", false
	}

	var b strings.Builder
	b.WriteString("<details>\n<summary>Release notes</summary>\n")
	fmt.Fprintf(&b, "<p><em>Sourced from <a href=\"https://github.com/%s/releases\">%s's releases</a>.</em></p>\n", repo, repo)
	b.WriteString("<blockquote>\n")
	for _, r := range inRange {
		if r.Prerelease {
			continue
		}
		fmt.Fprintf(&b, "<h2>%s</h2>\n%s\n", html.EscapeString(r.TagName), sanitizeReleaseBody(r.Body, repo))
	}
	b.WriteString("</blockquote>\n</details>")
	return b.String(), true
}

func indexByTag(releases []releaseEntry, version string) int {
	for i, r := range releases {
		if normalizeTag(r.TagName) == normalizeTag(version) {
			return i
		}
	}
	return -1
}

func normalizeTag(v string) string {
	return strings.TrimPrefix(v, "v")
}

var (
	mentionPattern  = regexp.MustCompile(`@([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?)`)
	issueRefPattern = regexp.MustCompile(`(^|[^A-Za-z0-9/])#(\d+)`)
)

// sanitizeReleaseBody neutralizes patterns in an upstream release body that
// GitHub would otherwise interpret against *this* repo when the body is
// embedded in a PR here: bare "@user" would notify that user as if they were
// mentioned in this PR, and bare "#123" would cross-link to this repo's
// issue/PR #123 instead of the upstream one. Both are rewritten the same way
// Dependabot does: a zero-width space breaks the mention, and issue
// references become explicit links to the upstream repo via
// redirect.github.com (which does not trigger a cross-reference notification
// on the target issue).
func sanitizeReleaseBody(body, repo string) string {
	body = mentionPattern.ReplaceAllString(body, "@\u200b$1")
	body = issueRefPattern.ReplaceAllString(body, fmt.Sprintf("$1[#$2](https://redirect.github.com/%s/issues/$2)", repo))
	return body
}

// CommitsHTML renders a Dependabot-style "Commits" <details> block for the
// range between fromVersion and toVersion in repo, using the GitHub compare
// API. Since repos disagree on whether tags are prefixed with "v", it tries
// both forms for each side before giving up. It returns ok=false if no
// combination resolves — enrichment is best-effort.
func (c *Client) CommitsHTML(ctx context.Context, repo, fromVersion, toVersion string) (string, bool) {
	const maxShown = 10

	for _, base := range candidateRefs(fromVersion) {
		for _, head := range candidateRefs(toVersion) {
			var out struct {
				HTMLURL string `json:"html_url"`
				Commits []struct {
					SHA     string `json:"sha"`
					HTMLURL string `json:"html_url"`
					Commit  struct {
						Message string `json:"message"`
					} `json:"commit"`
				} `json:"commits"`
			}
			if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/compare/%s...%s", repo, base, head), nil, &out); err != nil {
				continue
			}
			if len(out.Commits) == 0 {
				continue
			}

			var b strings.Builder
			b.WriteString("<details>\n<summary>Commits</summary>\n<ul>\n")
			shown := out.Commits
			if len(shown) > maxShown {
				shown = shown[len(shown)-maxShown:]
			}
			for _, cm := range shown {
				subject := cm.Commit.Message
				if idx := strings.IndexByte(subject, '\n'); idx != -1 {
					subject = subject[:idx]
				}
				shortSHA := cm.SHA
				if len(shortSHA) > 7 {
					shortSHA = shortSHA[:7]
				}
				fmt.Fprintf(&b, "<li><a href=\"%s\"><code>%s</code></a> %s</li>\n", cm.HTMLURL, shortSHA, html.EscapeString(sanitizeReleaseBody(subject, repo)))
			}
			fmt.Fprintf(&b, "<li>Additional commits viewable in <a href=\"%s\">compare view</a></li>\n", out.HTMLURL)
			b.WriteString("</ul>\n</details>")
			return b.String(), true
		}
	}
	return "", false
}

// candidateRefs returns v and its "v"-prefix-toggled counterpart, since
// tagging conventions vary by repo (e.g. "v0.6.0" vs "0.6.0").
func candidateRefs(v string) []string {
	if strings.HasPrefix(v, "v") {
		return []string{v, strings.TrimPrefix(v, "v")}
	}
	return []string{v, "v" + v}
}
