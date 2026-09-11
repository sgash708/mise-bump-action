package outdated

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newFakeRepoRoot returns a fresh temp dir to use as a test's repo root,
// before the fake mise script content (which needs to embed this path) is
// generated.
func newFakeRepoRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// installFakeMise writes script as an executable file named "mise" in a
// fresh temp dir and prepends that dir to PATH for the duration of the test.
// This exercises Run's real exec.CommandContext invocation (argument order,
// stdout/stderr separation, exit code handling) without depending on the
// real mise CLI being installed or trusted.
func installFakeMise(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake mise script requires a POSIX shell")
	}

	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "mise")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake mise script: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

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

func TestRun_InvokesMiseAndParsesItsStdout(t *testing.T) {
	repoRoot := newFakeRepoRoot(t)
	installFakeMise(t, fmt.Sprintf(`#!/bin/sh
if [ "$1" != "outdated" ] || [ "$2" != "--json" ] || [ "$3" != "-C" ]; then
  echo "unexpected args: $@" >&2
  exit 1
fi
cat <<'EOF'
{
  "go": {
    "name": "go",
    "requested": "1.26.1",
    "current": "1.26.1",
    "bump": "1.27.0",
    "latest": "1.27.0",
    "source": { "type": "mise.toml", "path": "%s/mise.toml" }
  }
}
EOF
`, repoRoot))

	entries, err := Run(context.Background(), repoRoot, ".")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 outdated entry, got %d: %+v", len(entries), entries)
	}
	want := Entry{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}
	if entries[0] != want {
		t.Errorf("entry mismatch:\n got  %+v\n want %+v", entries[0], want)
	}
}

func TestRun_WrapsCommandFailureWithStderr(t *testing.T) {
	repoRoot := newFakeRepoRoot(t)
	installFakeMise(t, `#!/bin/sh
echo "mise: network unreachable" >&2
exit 1
`)

	_, err := Run(context.Background(), repoRoot, ".")
	if err == nil {
		t.Fatal("expected an error when the mise command fails, got nil")
	}
	if !strings.Contains(err.Error(), "network unreachable") {
		t.Errorf("expected error to include the command's stderr output, got: %v", err)
	}
}
