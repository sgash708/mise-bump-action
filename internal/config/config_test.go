package config

import (
	"reflect"
	"testing"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestFromEnv_DefaultsWhenInputsEmpty(t *testing.T) {
	got, err := FromEnv(fakeEnv(map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	}))
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}

	want := Config{
		MiseConfigPaths: []string{"mise.toml"},
		PRStrategy:      "per-tool",
		Labels:          []string{"dependencies"},
		BaseBranch:      "main",
		GitHubToken:     "tok",
		Repository:      "sgash708/example",
		APIURL:          "https://api.github.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Config mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestFromEnv_ParsesMultilinePathsAndCustomValues(t *testing.T) {
	got, err := FromEnv(fakeEnv(map[string]string{
		"GITHUB_TOKEN":           "tok",
		"GITHUB_REPOSITORY":      "sgash708/example",
		"GITHUB_REF_NAME":        "main",
		"INPUT_MISE_CONFIG_PATH": "mise.toml\nbackend/mise.toml",
		"INPUT_PR_STRATEGY":      "single",
		"INPUT_LABELS":           "dependencies,mise",
		"INPUT_BASE_BRANCH":      "develop",
	}))
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}

	if !reflect.DeepEqual(got.MiseConfigPaths, []string{"mise.toml", "backend/mise.toml"}) {
		t.Errorf("MiseConfigPaths = %+v", got.MiseConfigPaths)
	}
	if got.PRStrategy != "single" {
		t.Errorf("PRStrategy = %q, want single", got.PRStrategy)
	}
	if !reflect.DeepEqual(got.Labels, []string{"dependencies", "mise"}) {
		t.Errorf("Labels = %+v", got.Labels)
	}
	if got.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want develop", got.BaseBranch)
	}
}

func TestFromEnv_MissingTokenReturnsError(t *testing.T) {
	_, err := FromEnv(fakeEnv(map[string]string{
		"GITHUB_REPOSITORY": "sgash708/example",
		"GITHUB_REF_NAME":   "main",
	}))
	if err == nil {
		t.Fatal("expected an error when GITHUB_TOKEN is missing, got nil")
	}
}
