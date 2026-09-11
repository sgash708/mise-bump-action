package outdated

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_SkipsToolsAlreadyUpToDate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "up_to_date.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	entries, err := Parse(data, "/repo")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 outdated entries (both tools already up to date once v-prefix is normalized), got %d: %+v", len(entries), entries)
	}
}

func TestParse_DetectsOutdatedAndNormalizesRelPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "with_outdated.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	entries, err := Parse(data, "/repo")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 outdated entry (only \"go\"), got %d: %+v", len(entries), entries)
	}
	got := entries[0]
	want := Entry{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}
	if got != want {
		t.Errorf("entry mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestParse_NormalizesVPrefixBeforeComparing(t *testing.T) {
	// go:github.com/matryer/moq has requested="v0.6.0" and latest="0.6.0" — these are
	// the same version once the "v" prefix is normalized, and must NOT be reported as outdated.
	data := []byte(`{
		"go:github.com/matryer/moq": {
			"name": "go:github.com/matryer/moq",
			"requested": "v0.6.0",
			"current": null,
			"bump": null,
			"latest": "0.6.0",
			"source": { "type": "mise.toml", "path": "/repo/mise.toml" }
		}
	}`)

	entries, err := Parse(data, "/repo")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected v0.6.0/0.6.0 to be treated as equal, got outdated entries: %+v", entries)
	}
}
