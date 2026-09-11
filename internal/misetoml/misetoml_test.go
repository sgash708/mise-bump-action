package misetoml

import "testing"

func TestBump_PlainKey(t *testing.T) {
	content := []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n")
	got, err := Bump(content, "go", "1.26.1", "1.27.0")
	if err != nil {
		t.Fatalf("Bump returned error: %v", err)
	}
	want := "[tools]\ngo = \"1.27.0\"\nnode = \"24.12.0\"\n"
	if string(got) != want {
		t.Errorf("content mismatch:\n got  %q\n want %q", got, want)
	}
}

func TestBump_PrefixedQuotedKeyPreservesComments(t *testing.T) {
	content := []byte("[tools]\ngo = \"1.26.1\"\n\n# aqua registry 経由のツール\n\"aqua:golangci/golangci-lint\" = \"2.9.0\"\n")
	got, err := Bump(content, "aqua:golangci/golangci-lint", "2.9.0", "2.10.0")
	if err != nil {
		t.Fatalf("Bump returned error: %v", err)
	}
	want := "[tools]\ngo = \"1.26.1\"\n\n# aqua registry 経由のツール\n\"aqua:golangci/golangci-lint\" = \"2.10.0\"\n"
	if string(got) != want {
		t.Errorf("content mismatch:\n got  %q\n want %q", got, want)
	}
}

func TestBump_ToolNotFound(t *testing.T) {
	content := []byte("[tools]\ngo = \"1.26.1\"\n")
	_, err := Bump(content, "node", "24.12.0", "24.13.0")
	if err == nil {
		t.Fatal("expected an error when the tool key is not found, got nil")
	}
}

func TestBump_VersionMismatch(t *testing.T) {
	content := []byte("[tools]\ngo = \"1.26.1\"\n")
	_, err := Bump(content, "go", "1.25.0", "1.27.0")
	if err == nil {
		t.Fatal("expected an error when oldVersion does not match the file content, got nil")
	}
}
