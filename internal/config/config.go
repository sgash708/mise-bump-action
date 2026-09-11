// Package config parses this action's inputs from environment variables.
package config

import (
	"fmt"
	"strings"
)

// Config holds all inputs needed to run one invocation of mise-bump-action.
type Config struct {
	MiseConfigPaths []string
	PRStrategy      string
	Labels          []string
	BaseBranch      string
	GitHubToken     string
	Repository      string
	APIURL          string
}

const defaultAPIURL = "https://api.github.com"

// FromEnv builds a Config from environment variables. getenv is injected so
// tests do not depend on process-global environment state.
func FromEnv(getenv func(string) string) (Config, error) {
	token := getenv("GITHUB_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("GITHUB_TOKEN is required")
	}

	repository := getenv("GITHUB_REPOSITORY")
	if repository == "" {
		return Config{}, fmt.Errorf("GITHUB_REPOSITORY is required")
	}

	paths := splitNonEmpty(getenv("INPUT_MISE_CONFIG_PATH"), "\n")
	if len(paths) == 0 {
		paths = []string{"mise.toml"}
	}

	strategy := getenv("INPUT_PR_STRATEGY")
	if strategy == "" {
		strategy = "per-tool"
	}

	labels := splitNonEmpty(getenv("INPUT_LABELS"), ",")
	if len(labels) == 0 {
		labels = []string{"dependencies"}
	}

	baseBranch := getenv("INPUT_BASE_BRANCH")
	if baseBranch == "" {
		baseBranch = getenv("GITHUB_REF_NAME")
	}

	apiURL := getenv("GITHUB_API_URL")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}

	return Config{
		MiseConfigPaths: paths,
		PRStrategy:      strategy,
		Labels:          labels,
		BaseBranch:      baseBranch,
		GitHubToken:     token,
		Repository:      repository,
		APIURL:          apiURL,
	}, nil
}

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, sep) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
