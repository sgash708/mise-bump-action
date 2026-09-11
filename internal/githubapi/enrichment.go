package githubapi

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
)

// ReleaseNotesHTML renders a Dependabot-style "Release notes" <details>
// block for repo (any public "owner/repo"), covering releases strictly
// after fromVersion up to and including toVersion. It returns ok=false if
// repo has no releases, toVersion can't be found among the first page of
// releases, or the request fails — release-notes enrichment is best-effort
// and must never fail the overall bump.
func (c *Client) ReleaseNotesHTML(ctx context.Context, repo, fromVersion, toVersion string) (string, bool) {
	var releases []struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/releases?per_page=100", repo), nil, &releases); err != nil {
		return "", false
	}

	toIdx := indexByTag(releases, toVersion)
	if toIdx == -1 {
		return "", false
	}
	fromIdx := indexByTag(releases, fromVersion)
	end := len(releases)
	if fromIdx != -1 {
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
		fmt.Fprintf(&b, "<h2>%s</h2>\n%s\n", html.EscapeString(r.TagName), r.Body)
	}
	b.WriteString("</blockquote>\n</details>")
	return b.String(), true
}

func indexByTag(releases []struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}, version string,
) int {
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
				fmt.Fprintf(&b, "<li><a href=\"%s\"><code>%s</code></a> %s</li>\n", cm.HTMLURL, shortSHA, html.EscapeString(subject))
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
