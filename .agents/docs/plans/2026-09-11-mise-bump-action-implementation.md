# mise-bump-action Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** mise管理下のツールバージョンを検知し、Dependabot風のPRを自動で開くGitHub Action(composite action + Goバイナリ)のv0を実装する。

**Architecture:** Goの単一バイナリが `mise outdated --json` の出力を解析し、`pr-strategy` に応じてグルーピングした上で、GitHub REST APIを直接呼び出してbranch作成・`mise.toml`書き換えcommit・PR作成・ラベル付与までを行う。外部依存(サードパーティaction、Go外部モジュール)はゼロを目指す(標準ライブラリのみ)。

**Tech Stack:** Go(標準ライブラリのみ、外部moduleなし)、GitHub Actions composite action、`golangci-lint`、`moq`(モック生成)。

**Spec:** `.agents/docs/specs/2026-09-11-mise-bump-action-design.md`(ADR: `.agents/docs/adr/0001`〜`0004`)

## Global Constraints

- module path: `github.com/sgash708/mise-bump-action`
- go directive: `go 1.23`(このマシンの実行可能なGoツールチェーンに合わせる)
- 外部Goモジュール依存は追加しない(標準ライブラリのみ。GitHub API呼び出しも`net/http`で自前実装 — ADR 0002)
- バージョン解決は`mise outdated --json`に委譲し、自前で再実装しない(ADR 0001)
- PRタイトル/commit trailerはDependabotの`bump`規約に従う(ADR 0003): `chore(deps): bump <short-name> from <old> to <new>`、commit本文に`updated-dependencies:` YAML trailer
- v0はlinux/amd64のみ対象、composite actionはGitHub Releaseからビルド済みバイナリをダウンロードする(ADR 0004)
- コーディング規約(`hc-stock-api/.agents/docs/code-style.md`を参考に採用):
  - `gofmt`/`goimports`でフォーマット、`golangci-lint`の警告を解消する
  - モックは`moq`で生成する(手書き禁止)
  - パッケージ名: 小文字・短く・単数形
  - エラーは必ずチェックする。`fmt.Errorf("...: %w", err)`で文脈を追加。エラーメッセージは小文字始まり、末尾ピリオドなし
  - エクスポートする関数・型にはGoDocコメントを書く。コメントは「なぜ」を説明する
  - テストはtable-driven、`_test.go`を対象と同じディレクトリに配置、関数名は`Test<対象>_<シナリオ>`
- ただしhc-stock-apiのhandler/usecase/repository層別エラー変換ルール(`error-handling.md`)は、この小規模CLIには過剰なため採用しない。エラーは`fmt.Errorf`で素直にラップして上位に伝播させる

## 実データ調査で判明した前提(実装時に必ず踏まえること)

- `mise outdated --json`は**ツールキーをマップキーとするJSONオブジェクト**を返す(配列ではない)。実際に`hc-stock-api/mise.toml`に対して実行した結果:
  ```json
  {
    "aqua:golangci/golangci-lint": {
      "name": "aqua:golangci/golangci-lint",
      "requested": "2.9.0",
      "current": null,
      "bump": null,
      "latest": "2.9.0",
      "source": { "type": "mise.toml", "path": "/abs/path/to/mise.toml" }
    },
    "go:github.com/matryer/moq": {
      "name": "go:github.com/matryer/moq",
      "requested": "v0.6.0",
      "current": null,
      "bump": null,
      "latest": "0.6.0",
      "source": { "type": "mise.toml", "path": "/abs/path/to/mise.toml" }
    }
  }
  ```
- **`mise`は解決に失敗したツールをJSONから黙って除外する**(seiryuの`terraform`だけのmise.tomlに対して実行すると`{}`が返った)。exit codeは0のまま。つまり「特定ツールだけスキップしてジョブサマリに警告」は`mise`側で既に行われており、こちら側で個別のエラーハンドリングは不要(全体の実行が止まらないことだけ保証すればよい)。
- **`requested`と`latest`が文字列として同じであることは「アップデート不要」の判定条件にならない**(上記の2件はどちらも実質的には最新だが、`"requested": "v0.6.0"` vs `"latest": "0.6.0"`のように`go install`系バックエンドは`v`prefixの有無がずれる)。`v`prefixを正規化してから比較しないと、変化していないのに誤って「outdated」と判定してしまう(偽陽性)。
- `mise outdated`は標準エラー出力に警告(mise自体の新バージョン通知、ネットワークエラー等)を出すことがあるが、標準出力のJSONには影響しない。
- Composite actionは呼び出し元workflowから**自動では`INPUT_*`環境変数を受け取らない**(Docker/JSアクションと違い、composite actionは自分の`action.yml`内の`steps[].env`で明示的に設定する必要がある)。当action自身の`action.yml`でこれを行うため、env変数名は自由に選べる(ハイフンを気にする必要はなく`INPUT_MISE_CONFIG_PATH`のようにアンダースコアで統一する)。
- **`github.action_ref`はcomposite actionでは公式に保証された値ではない**(GitHub公式ドキュメントリポジトリでも既知の issue として扱われている)。ダウンロードするリリースタグをこれに依存させるのは壊れやすい。代わりに、リリースごとに`action.yml`内のダウンロードURLのバージョン文字列を直接書き換えてコミット・タグ付けする運用にする(`Makefile release`タスクで自動化)。

---

### Task 1: プロジェクト scaffold(Goモジュール・lint・Makefile)

**Files:**
- Create: `go.mod`
- Create: `.golangci.yaml`
- Create: `Makefile`
- Create: `mise.toml`(このプロジェクト自身の開発ツール管理。`golangci-lint`/`moq`/`actionlint`)
- Create: `cmd/mise-bump-action/main.go`(この時点では空実装のプレースホルダ)

**Interfaces:**
- Produces: `module github.com/sgash708/mise-bump-action`、`make build` / `make test` / `make lint` / `make fmt` コマンド

- [ ] **Step 1: go.mod を作成**

```
module github.com/sgash708/mise-bump-action

go 1.23
```

コマンド: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go mod init github.com/sgash708/mise-bump-action` を実行し、生成された`go.mod`の内容が上記と一致することを確認する(go versionは環境のものがそのまま入る。1.23系であることを確認)。

- [ ] **Step 2: .golangci.yaml を作成**

```yaml
version: "2"

linters:
  default: standard

formatters:
  enable:
    - gofmt
    - goimports

issues:
  max-issues-per-linter: 50
  max-same-issues: 10

output:
  formats:
    text:
      path: stdout
  sort-order:
    - linter
    - file
```

- [ ] **Step 3: Makefile を作成**

```makefile
.PHONY: build test lint fmt release

build:
	go build -o bin/mise-bump-action ./cmd/mise-bump-action

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .
	goimports -l -w .

release:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make release VERSION=v0.1.0" && exit 1)
	sed -i.bak -E 's#(download/)v[0-9]+\.[0-9]+\.[0-9]+(/mise-bump-action_linux_amd64)#\1$(VERSION)\2#' action.yml
	rm -f action.yml.bak
	git add action.yml
	git commit -m "chore: release $(VERSION)"
	git tag $(VERSION)
	@echo "Now run: git push && git push origin $(VERSION)"
```

- [ ] **Step 4: mise.toml を作成(このプロジェクト自身の開発ツール管理)**

```toml
[tools]
"aqua:golangci/golangci-lint" = "2.9.0"
"aqua:rhysd/actionlint" = "1.7.10"
"go:github.com/matryer/moq" = "v0.6.0"
```

- [ ] **Step 5: プレースホルダの main.go を作成**

```go
package main

func main() {}
```

- [ ] **Step 6: ビルドとlintの疎通確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go build ./... && golangci-lint run`
Expected: どちらもエラーなく終了する

- [ ] **Step 7: Commit**

```bash
git add go.mod .golangci.yaml Makefile mise.toml cmd/mise-bump-action/main.go
git commit -m "chore: Goモジュールとlint/build設定のscaffoldを追加"
```

---

### Task 2: `internal/outdated` — `mise outdated --json` の実行と解析

**Files:**
- Create: `internal/outdated/outdated.go`
- Create: `internal/outdated/outdated_test.go`
- Create: `internal/outdated/testdata/up_to_date.json`
- Create: `internal/outdated/testdata/with_outdated.json`

**Interfaces:**
- Produces:
  ```go
  package outdated

  type Entry struct {
      Name      string // mise上のツールキー。例: "go", "aqua:golangci/golangci-lint", "go:github.com/matryer/moq"
      Requested string // mise.tomlに書かれている現在のピン
      Latest    string // 更新先バージョン(go installバックエンドの"v"prefix有無はRequestedに合わせて正規化済み)
      RelPath   string // リポジトリルートからの相対パス。GitHub Contents APIに渡す
  }

  func Run(ctx context.Context, repoRoot, configDir string) ([]Entry, error)
  func Parse(jsonBytes []byte, repoRootAbs string) ([]Entry, error)
  ```

- [ ] **Step 1: 実データから作成したfixtureを配置する**

`internal/outdated/testdata/up_to_date.json`(hc-stock-apiに対する実行結果そのまま。両方とも実質最新):

```json
{
  "aqua:golangci/golangci-lint": {
    "name": "aqua:golangci/golangci-lint",
    "requested": "2.9.0",
    "current": null,
    "bump": null,
    "latest": "2.9.0",
    "source": { "type": "mise.toml", "path": "/repo/mise.toml" }
  },
  "go:github.com/matryer/moq": {
    "name": "go:github.com/matryer/moq",
    "requested": "v0.6.0",
    "current": null,
    "bump": null,
    "latest": "0.6.0",
    "source": { "type": "mise.toml", "path": "/repo/mise.toml" }
  }
}
```

`internal/outdated/testdata/with_outdated.json`(上記に加えて、コア`go`ツールが実際にoutdatedなケースを追加した合成fixture):

```json
{
  "go": {
    "name": "go",
    "requested": "1.26.1",
    "current": "1.26.1",
    "bump": "1.27.0",
    "latest": "1.27.0",
    "source": { "type": "mise.toml", "path": "/repo/mise.toml" }
  },
  "aqua:golangci/golangci-lint": {
    "name": "aqua:golangci/golangci-lint",
    "requested": "2.9.0",
    "current": null,
    "bump": null,
    "latest": "2.9.0",
    "source": { "type": "mise.toml", "path": "/repo/mise.toml" }
  },
  "go:github.com/matryer/moq": {
    "name": "go:github.com/matryer/moq",
    "requested": "v0.6.0",
    "current": null,
    "bump": null,
    "latest": "0.6.0",
    "source": { "type": "mise.toml", "path": "/repo/mise.toml" }
  }
}
```

- [ ] **Step 2: 失敗するテストを書く**

```go
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
```

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/outdated/...`
Expected: FAIL(`Parse` undefined)

- [ ] **Step 4: 実装を書く**

```go
// Package outdated runs `mise outdated --json` and reports tools whose pinned
// version differs from the latest version mise can resolve.
package outdated

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is a single mise-managed tool whose pinned version is behind the latest
// version mise resolved for it.
type Entry struct {
	Name      string
	Requested string
	Latest    string
	RelPath   string
}

type rawSource struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type rawEntry struct {
	Name      string    `json:"name"`
	Requested string    `json:"requested"`
	Latest    string    `json:"latest"`
	Source    rawSource `json:"source"`
}

// Run executes `mise outdated --json -C configDir` and parses its output. mise
// itself silently omits tools it cannot resolve (e.g. due to network errors),
// so a successful Run only reports tools mise could actually check.
func Run(ctx context.Context, repoRoot, configDir string) ([]Entry, error) {
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo root %q: %w", repoRoot, err)
	}

	cmd := exec.CommandContext(ctx, "mise", "outdated", "--json", "-C", configDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run mise outdated in %q (stderr: %s): %w", configDir, stderr.String(), err)
	}

	return Parse(stdout.Bytes(), repoRootAbs)
}

// Parse extracts outdated entries from the raw JSON produced by `mise outdated
// --json`. repoRootAbs must be an absolute path; each entry's RelPath is
// computed relative to it.
func Parse(jsonBytes []byte, repoRootAbs string) ([]Entry, error) {
	var raw map[string]rawEntry
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mise outdated json: %w", err)
	}

	entries := make([]Entry, 0, len(raw))
	for _, e := range raw {
		requestedNorm := normalizeVersion(e.Requested)
		latestNorm := normalizeVersion(e.Latest)
		if requestedNorm == latestNorm {
			continue
		}

		latest := e.Latest
		if strings.HasPrefix(e.Requested, "v") && !strings.HasPrefix(e.Latest, "v") {
			latest = "v" + e.Latest
		}

		relPath, err := filepath.Rel(repoRootAbs, e.Source.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to compute relative path for %q: %w", e.Name, err)
		}

		entries = append(entries, Entry{
			Name:      e.Name,
			Requested: e.Requested,
			Latest:    latest,
			RelPath:   relPath,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// normalizeVersion strips a leading "v" so that go-install-backend versions
// (which mise reports with an inconsistent "v" prefix between requested and
// latest) compare equal when they represent the same version.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
```

- [ ] **Step 5: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/outdated/... -v`
Expected: PASS(3件とも)

- [ ] **Step 6: Commit**

```bash
git add internal/outdated
git commit -m "feat: mise outdated --jsonの実行と解析を追加"
```

---

### Task 3: `internal/misetoml` — `mise.toml`のピン書き換え

**Files:**
- Create: `internal/misetoml/misetoml.go`
- Create: `internal/misetoml/misetoml_test.go`

**Interfaces:**
- Consumes: なし(独立パッケージ)
- Produces:
  ```go
  package misetoml

  func Bump(content []byte, toolKey, oldVersion, newVersion string) ([]byte, error)
  ```

- [ ] **Step 1: 失敗するテストを書く**

```go
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
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/misetoml/...`
Expected: FAIL(`Bump` undefined)

- [ ] **Step 3: 実装を書く**

```go
// Package misetoml applies targeted version bumps to mise.toml content
// without a full TOML round-trip, so comments and formatting elsewhere in the
// file are preserved byte-for-byte.
package misetoml

import (
	"fmt"
	"regexp"
	"strings"
)

// Bump replaces the pinned version for toolKey in content, matching both
// plain (`go = "1.26.1"`) and quoted/prefixed (`"aqua:owner/repo" = "1.0.0"`)
// key forms. It returns an error if toolKey is not found, or if its current
// value does not match oldVersion (guarding against a stale bump target).
func Bump(content []byte, toolKey, oldVersion, newVersion string) ([]byte, error) {
	pattern := regexp.MustCompile(
		`^(\s*"?` + regexp.QuoteMeta(toolKey) + `"?\s*=\s*")` +
			regexp.QuoteMeta(oldVersion) +
			`("\s*)$`,
	)

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if m := pattern.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + newVersion + m[2]
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("tool %q with version %q not found in mise.toml content", toolKey, oldVersion)
	}

	return []byte(strings.Join(lines, "\n")), nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/misetoml/... -v`
Expected: PASS(4件とも)

- [ ] **Step 5: Commit**

```bash
git add internal/misetoml
git commit -m "feat: mise.tomlのピン書き換えロジックを追加"
```

---

### Task 4: `internal/grouping` — PR単位のグルーピング

**Files:**
- Create: `internal/grouping/grouping.go`
- Create: `internal/grouping/grouping_test.go`

**Interfaces:**
- Consumes: `outdated.Entry`(Task 2)
- Produces:
  ```go
  package grouping

  type Strategy string

  const (
      PerTool Strategy = "per-tool"
      Single  Strategy = "single"
  )

  type Group struct {
      Entries []outdated.Entry
  }

  func Group(entries []outdated.Entry, strategy Strategy) ([]Group, error)
  ```

- [ ] **Step 1: 失敗するテストを書く**

```go
package grouping

import (
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func entries(names ...string) []outdated.Entry {
	es := make([]outdated.Entry, len(names))
	for i, n := range names {
		es[i] = outdated.Entry{Name: n}
	}
	return es
}

func TestGroup_PerToolCreatesOneGroupPerEntry(t *testing.T) {
	got, err := Group(entries("go", "node", "aqua:golangci/golangci-lint"), PerTool)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(got))
	}
	for i, g := range got {
		if len(g.Entries) != 1 {
			t.Errorf("group %d: expected 1 entry, got %d", i, len(g.Entries))
		}
	}
}

func TestGroup_SingleCreatesOneGroupWithAllEntries(t *testing.T) {
	got, err := Group(entries("go", "node", "aqua:golangci/golangci-lint"), Single)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 group, got %d", len(got))
	}
	if len(got[0].Entries) != 3 {
		t.Fatalf("expected 3 entries in the single group, got %d", len(got[0].Entries))
	}
}

func TestGroup_EmptyEntriesReturnsNoGroups(t *testing.T) {
	got, err := Group(nil, Single)
	if err != nil {
		t.Fatalf("Group returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 groups for empty input, got %d", len(got))
	}
}

func TestGroup_UnknownStrategyReturnsError(t *testing.T) {
	_, err := Group(entries("go"), Strategy("bogus"))
	if err == nil {
		t.Fatal("expected an error for an unknown strategy, got nil")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/grouping/...`
Expected: FAIL(`Group`/`PerTool`/`Single`undefined)

- [ ] **Step 3: 実装を書く**

```go
// Package grouping splits outdated tool entries into pull request groups
// according to the configured pr-strategy.
package grouping

import (
	"fmt"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

// Strategy controls how outdated entries are grouped into pull requests.
type Strategy string

const (
	// PerTool opens one pull request per outdated tool, matching Dependabot's
	// default behavior.
	PerTool Strategy = "per-tool"
	// Single bundles all outdated tools into a single pull request.
	Single Strategy = "single"
)

// Group is a set of outdated entries that will be bumped together in one
// pull request.
type Group struct {
	Entries []outdated.Entry
}

// Group splits entries into pull request groups according to strategy. An
// empty entries slice always yields zero groups.
func Group(entries []outdated.Entry, strategy Strategy) ([]Group, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	switch strategy {
	case PerTool, "":
		groups := make([]Group, len(entries))
		for i, e := range entries {
			groups[i] = Group{Entries: []outdated.Entry{e}}
		}
		return groups, nil
	case Single:
		return []Group{{Entries: entries}}, nil
	default:
		return nil, fmt.Errorf("unknown pr-strategy %q", strategy)
	}
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/grouping/... -v`
Expected: PASS(4件とも)

- [ ] **Step 5: Commit**

```bash
git add internal/grouping
git commit -m "feat: pr-strategyによるグルーピングを追加"
```

---

### Task 5: `internal/prtext` — Dependabot形式のPR本文生成

**Files:**
- Create: `internal/prtext/prtext.go`
- Create: `internal/prtext/prtext_test.go`

**Interfaces:**
- Consumes: `outdated.Entry`(Task 2)
- Produces:
  ```go
  package prtext

  type Content struct {
      Title  string
      Body   string
      Commit string
  }

  func Build(entries []outdated.Entry, multiConfig bool) Content
  ```

- [ ] **Step 1: 失敗するテストを書く**

```go
package prtext

import (
	"strings"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestBuild_SingleEntryTitleAndTrailer(t *testing.T) {
	entries := []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"}}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump go from 1.26.1 to 1.27.0"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
	if !strings.Contains(got.Commit, "updated-dependencies:") {
		t.Errorf("Commit missing updated-dependencies trailer: %q", got.Commit)
	}
	if !strings.Contains(got.Commit, "dependency-name: go") {
		t.Errorf("Commit missing dependency-name: %q", got.Commit)
	}
	if !strings.Contains(got.Commit, "dependency-version: 1.27.0") {
		t.Errorf("Commit missing dependency-version: %q", got.Commit)
	}
}

func TestBuild_SingleEntryWithMultiConfigAppendsPath(t *testing.T) {
	entries := []outdated.Entry{{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "backend/mise.toml"}}

	got := Build(entries, true)

	wantTitle := "chore(deps): bump go from 1.26.1 to 1.27.0 in backend/mise.toml"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
}

func TestBuild_StripsBackendPrefixFromDisplayName(t *testing.T) {
	entries := []outdated.Entry{{Name: "aqua:golangci/golangci-lint", Requested: "2.9.0", Latest: "2.10.0", RelPath: "mise.toml"}}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump golangci-lint from 2.9.0 to 2.10.0"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
}

func TestBuild_GroupedEntriesListsEachToolInTrailer(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}

	got := Build(entries, false)

	wantTitle := "chore(deps): bump 2 mise-managed tools"
	if got.Title != wantTitle {
		t.Errorf("Title = %q, want %q", got.Title, wantTitle)
	}
	for _, want := range []string{"dependency-name: go", "dependency-name: node"} {
		if !strings.Contains(got.Commit, want) {
			t.Errorf("Commit missing %q: %q", want, got.Commit)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/prtext/...`
Expected: FAIL(`Build`undefined)

- [ ] **Step 3: 実装を書く**

```go
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
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/prtext/... -v`
Expected: PASS(4件とも)

- [ ] **Step 5: Commit**

```bash
git add internal/prtext
git commit -m "feat: Dependabot形式のPRタイトル/commit trailer生成を追加"
```

---

### Task 6: `internal/config` — action inputのパース

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  ```go
  package config

  type Config struct {
      MiseConfigPaths []string
      PRStrategy      string
      Labels          []string
      BaseBranch      string
      GitHubToken     string
      Repository      string
      APIURL          string
  }

  func FromEnv(getenv func(string) string) (Config, error)
  ```

**設計メモ:** composite actionは`INPUT_*`を自動で受け取らないため、`action.yml`側で自前の`env:`キー名(`INPUT_MISE_CONFIG_PATH`等、アンダースコア区切り)を明示的に設定する(Task 9)。ここではそのキー名を前提にパースする。`GITHUB_REPOSITORY`/`GITHUB_API_URL`/`GITHUB_REF_NAME`はGitHub Actionsが全stepに自動で渡す標準env varなので、composite action側での追加設定は不要。

- [ ] **Step 1: 失敗するテストを書く**

```go
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
		"GITHUB_TOKEN":            "tok",
		"GITHUB_REPOSITORY":       "sgash708/example",
		"GITHUB_REF_NAME":         "main",
		"INPUT_MISE_CONFIG_PATH":  "mise.toml\nbackend/mise.toml",
		"INPUT_PR_STRATEGY":       "single",
		"INPUT_LABELS":            "dependencies,mise",
		"INPUT_BASE_BRANCH":       "develop",
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
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/config/...`
Expected: FAIL(`Config`/`FromEnv`undefined)

- [ ] **Step 3: 実装を書く**

```go
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
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/config/... -v`
Expected: PASS(3件とも)

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: action inputの環境変数パースを追加"
```

---

### Task 7: `internal/runner` — オーケストレーション

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/runner_test.go`
- Create: `internal/runner/mocks.go`(moq生成)

**Interfaces:**
- Consumes: `outdated.Entry`(Task 2)、`grouping.Group`/`grouping.Strategy`(Task 4)、`prtext.Build`(Task 5)、`config.Config`(Task 6)
- Produces:
  ```go
  package runner

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

  type GitHub interface {
      ReadFile(ctx context.Context, path, ref string) (content []byte, sha string, err error)
      OpenBumpPR(ctx context.Context, in BumpPRInput) (prNumber int, err error)
  }

  func Run(ctx context.Context, cfg config.Config, entries []outdated.Entry, gh GitHub) ([]int, error)
  ```

Task 8(`internal/githubapi`)はこの`GitHub`インターフェースを実装する。

- [ ] **Step 1: `GitHub`インターフェースと`BumpPRInput`を定義する**

`internal/runner/runner.go`:

```go
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
		fmt.Fprintf(h, "%s@%s;", e.Name, e.Latest)
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
```

- [ ] **Step 2: moqでモックを生成する**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go run github.com/matryer/moq@v0.6.0 -out internal/runner/mocks.go ./internal/runner GitHub`
Expected: `internal/runner/mocks.go`が生成され、`GitHubMock`構造体(各メソッドに対応する`XxxFunc`フィールドと呼び出し記録)が定義される

- [ ] **Step 3: 失敗するテストを書く**

```go
package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/outdated"
)

func TestRun_PerToolOpensOnePRPerEntry(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "per-tool", BaseBranch: "main", Labels: []string{"dependencies"}}

	var openedTitles []string
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			openedTitles = append(openedTitles, in.PRTitle)
			return len(openedTitles), nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 2 {
		t.Fatalf("expected 2 PR numbers, got %d: %+v", len(numbers), numbers)
	}
	wantTitles := []string{
		"chore(deps): bump go from 1.26.1 to 1.27.0",
		"chore(deps): bump node from 24.12.0 to 24.13.0",
	}
	for _, want := range wantTitles {
		found := false
		for _, got := range openedTitles {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a PR titled %q, got titles %+v", want, openedTitles)
		}
	}
}

func TestRun_SingleBundlesIntoOnePR(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "single", BaseBranch: "main"}

	callCount := 0
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			callCount++
			if !contains(in.FileContent, "1.27.0") || !contains(in.FileContent, "24.13.0") {
				t.Errorf("expected bundled file content to contain both bumped versions, got %q", in.FileContent)
			}
			return 1, nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(numbers) != 1 || callCount != 1 {
		t.Fatalf("expected exactly 1 PR to be opened, got %d (callCount=%d)", len(numbers), callCount)
	}
}

func TestRun_ReturnsPartialResultsOnError(t *testing.T) {
	entries := []outdated.Entry{
		{Name: "go", Requested: "1.26.1", Latest: "1.27.0", RelPath: "mise.toml"},
		{Name: "node", Requested: "24.12.0", Latest: "24.13.0", RelPath: "mise.toml"},
	}
	cfg := config.Config{PRStrategy: "per-tool", BaseBranch: "main"}

	calls := 0
	gh := &GitHubMock{
		ReadFileFunc: func(ctx context.Context, path, ref string) ([]byte, string, error) {
			return []byte("[tools]\ngo = \"1.26.1\"\nnode = \"24.12.0\"\n"), "blobsha", nil
		},
		OpenBumpPRFunc: func(ctx context.Context, in BumpPRInput) (int, error) {
			calls++
			if calls == 2 {
				return 0, errors.New("boom")
			}
			return calls, nil
		},
	}

	numbers, err := Run(context.Background(), cfg, entries, gh)
	if err == nil {
		t.Fatal("expected an error from the second PR creation, got nil")
	}
	if len(numbers) != 1 {
		t.Fatalf("expected the first successful PR number to be returned, got %+v", numbers)
	}
}

func contains(b []byte, s string) bool {
	return len(b) > 0 && string(b) != "" && (string(b) == s || indexOf(string(b), s) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/runner/...`
Expected: FAIL(`Run`が`internal/runner/runner.go`未作成のため、または`GitHubMock`未生成のため)

- [ ] **Step 5: Step 1のrunner.goとStep 2のmocks.gen.goが揃っていることを確認し、テストを再実行する**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/runner/... -v`
Expected: PASS(3件とも)

- [ ] **Step 6: Commit**

```bash
git add internal/runner
git commit -m "feat: PR作成オーケストレーションを追加"
```

---

### Task 8: `internal/githubapi` — GitHub REST APIクライアント

**Files:**
- Create: `internal/githubapi/client.go`
- Create: `internal/githubapi/client_test.go`

**Interfaces:**
- Consumes: `runner.GitHub`(Task 7、構造的に実装する。明示的な`var _ runner.GitHub = (*Client)(nil)`で保証する)、`runner.BumpPRInput`
- Produces:
  ```go
  package githubapi

  func NewClient(httpClient *http.Client, apiURL, token, repo string) *Client
  func (c *Client) ReadFile(ctx context.Context, path, ref string) ([]byte, string, error)
  func (c *Client) OpenBumpPR(ctx context.Context, in runner.BumpPRInput) (int, error)
  ```

- [ ] **Step 1: 失敗するテストを書く(httptestで偽のGitHub APIサーバを立てる)**

```go
package githubapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sgash708/mise-bump-action/internal/runner"
)

func TestReadFile_DecodesBase64Content(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/sgash708/example/contents/mise.toml" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sha":     "blobsha123",
			"content": base64.StdEncoding.EncodeToString([]byte("[tools]\ngo = \"1.26.1\"\n")),
		})
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

	content, sha, err := c.ReadFile(context.Background(), "mise.toml", "main")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "[tools]\ngo = \"1.26.1\"\n" {
		t.Errorf("content = %q", content)
	}
	if sha != "blobsha123" {
		t.Errorf("sha = %q, want blobsha123", sha)
	}
}

func TestOpenBumpPR_CallsBranchCommitPRAndLabelsInOrder(t *testing.T) {
	var calls []string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/sgash708/example/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "get-ref")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": map[string]string{"sha": "basesha"},
		})
	})
	mux.HandleFunc("/repos/sgash708/example/git/refs", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "create-ref")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("/repos/sgash708/example/contents/mise.toml", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "put-file")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	})
	mux.HandleFunc("/repos/sgash708/example/pulls", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "create-pr")
		_ = json.NewEncoder(w).Encode(map[string]int{"number": 42})
	})
	mux.HandleFunc("/repos/sgash708/example/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "add-labels")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.Client(), srv.URL, "tok", "sgash708/example")

	number, err := c.OpenBumpPR(context.Background(), runner.BumpPRInput{
		BaseBranch:    "main",
		BranchName:    "mise-bump/go-1.27.0",
		FilePath:      "mise.toml",
		FileContent:   []byte("[tools]\ngo = \"1.27.0\"\n"),
		FileSHA:       "blobsha123",
		CommitMessage: "chore(deps): bump go from 1.26.1 to 1.27.0",
		PRTitle:       "chore(deps): bump go from 1.26.1 to 1.27.0",
		PRBody:        "Bumps go.",
		Labels:        []string{"dependencies"},
	})
	if err != nil {
		t.Fatalf("OpenBumpPR returned error: %v", err)
	}
	if number != 42 {
		t.Errorf("number = %d, want 42", number)
	}
	wantCalls := []string{"get-ref", "create-ref", "put-file", "create-pr", "add-labels"}
	if len(calls) != len(wantCalls) {
		t.Fatalf("calls = %+v, want %+v", calls, wantCalls)
	}
	for i, want := range wantCalls {
		if calls[i] != want {
			t.Errorf("calls[%d] = %q, want %q", i, calls[i], want)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/githubapi/...`
Expected: FAIL(`NewClient`等未定義)

- [ ] **Step 3: 実装を書く**

```go
// Package githubapi implements runner.GitHub against the real GitHub REST
// API using only the standard library (ADR 0002: no third-party PR-creation
// action, no external Go HTTP client dependency).
package githubapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sgash708/mise-bump-action/internal/runner"
)

// Client implements runner.GitHub.
type Client struct {
	httpClient *http.Client
	apiURL     string
	token      string
	repo       string
}

var _ runner.GitHub = (*Client)(nil)

// NewClient builds a Client for repo (in "owner/repo" form) authenticated
// with token. apiURL is normally https://api.github.com; tests pass an
// httptest.Server URL instead.
func NewClient(httpClient *http.Client, apiURL, token, repo string) *Client {
	return &Client{
		httpClient: httpClient,
		apiURL:     strings.TrimSuffix(apiURL, "/"),
		token:      token,
		repo:       repo,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body for %s %s: %w", method, path, err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to build request for %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call github api %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api %s %s returned %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

// ReadFile fetches the current content and blob SHA of path at ref via the
// GitHub Contents API.
func (c *Client) ReadFile(ctx context.Context, path, ref string) ([]byte, string, error) {
	var out struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", c.repo, path, ref), nil, &out); err != nil {
		return nil, "", fmt.Errorf("failed to read file %s at ref %s: %w", path, ref, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode content of %s: %w", path, err)
	}
	return decoded, out.SHA, nil
}

// OpenBumpPR creates a branch from in.BaseBranch, commits in.FileContent to
// in.FilePath on that branch, opens a pull request, and applies in.Labels. It
// returns the created pull request number.
func (c *Client) OpenBumpPR(ctx context.Context, in runner.BumpPRInput) (int, error) {
	baseSHA, err := c.getRefSHA(ctx, in.BaseBranch)
	if err != nil {
		return 0, err
	}
	if err := c.createRef(ctx, in.BranchName, baseSHA); err != nil {
		return 0, err
	}
	if err := c.putFile(ctx, in.FilePath, in.CommitMessage, in.FileContent, in.FileSHA, in.BranchName); err != nil {
		return 0, err
	}
	number, err := c.createPullRequest(ctx, in.PRTitle, in.PRBody, in.BranchName, in.BaseBranch)
	if err != nil {
		return 0, err
	}
	if err := c.addLabels(ctx, number, in.Labels); err != nil {
		return 0, err
	}
	return number, nil
}

func (c *Client) getRefSHA(ctx context.Context, branch string) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/ref/heads/%s", c.repo, branch), nil, &out); err != nil {
		return "", fmt.Errorf("failed to get ref sha for branch %s: %w", branch, err)
	}
	return out.Object.SHA, nil
}

func (c *Client) createRef(ctx context.Context, branch, sha string) error {
	body := map[string]string{"ref": "refs/heads/" + branch, "sha": sha}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/git/refs", c.repo), body, nil); err != nil {
		return fmt.Errorf("failed to create ref for branch %s: %w", branch, err)
	}
	return nil
}

func (c *Client) putFile(ctx context.Context, path, message string, content []byte, sha, branch string) error {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"sha":     sha,
		"branch":  branch,
	}
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/contents/%s", c.repo, path), body, nil); err != nil {
		return fmt.Errorf("failed to update file %s on branch %s: %w", path, branch, err)
	}
	return nil
}

func (c *Client) createPullRequest(ctx context.Context, title, body, head, base string) (int, error) {
	reqBody := map[string]string{"title": title, "body": body, "head": head, "base": base}
	var out struct {
		Number int `json:"number"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls", c.repo), reqBody, &out); err != nil {
		return 0, fmt.Errorf("failed to create pull request for head %s: %w", head, err)
	}
	return out.Number, nil
}

func (c *Client) addLabels(ctx context.Context, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	body := map[string][]string{"labels": labels}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/labels", c.repo, number), body, nil); err != nil {
		return fmt.Errorf("failed to add labels to pull request #%d: %w", number, err)
	}
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./internal/githubapi/... -v`
Expected: PASS(2件とも)

- [ ] **Step 5: Commit**

```bash
git add internal/githubapi
git commit -m "feat: GitHub REST APIクライアントを追加"
```

---

### Task 9: `cmd/mise-bump-action/main.go` — 結線

**Files:**
- Modify: `cmd/mise-bump-action/main.go`(Task 1のプレースホルダを置き換える)
- Create: `cmd/mise-bump-action/main_test.go`

**Interfaces:**
- Consumes: `config.FromEnv`(Task 6)、`outdated.Run`(Task 2)、`runner.Run`(Task 7)、`githubapi.NewClient`(Task 8)

- [ ] **Step 1: 失敗するテストを書く(終了コードを返すrun関数を切り出してテスト可能にする)**

```go
package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRun_ReturnsErrorWhenConfigInvalid(t *testing.T) {
	var stderr bytes.Buffer
	err := run(context.Background(), func(string) string { return "" }, &stderr)
	if err == nil {
		t.Fatal("expected an error when required env vars are missing, got nil")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./cmd/mise-bump-action/...`
Expected: FAIL(`run`undefined)

- [ ] **Step 3: 実装を書く**

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sgash708/mise-bump-action/internal/config"
	"github.com/sgash708/mise-bump-action/internal/githubapi"
	"github.com/sgash708/mise-bump-action/internal/outdated"
	"github.com/sgash708/mise-bump-action/internal/runner"
)

func main() {
	if err := run(context.Background(), os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	var allEntries []outdated.Entry
	for _, path := range cfg.MiseConfigPaths {
		entries, err := outdated.Run(ctx, ".", filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("failed to check outdated tools for %s: %w", path, err)
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 {
		fmt.Fprintln(stderr, "no outdated mise-managed tools found")
		return nil
	}

	gh := githubapi.NewClient(http.DefaultClient, cfg.APIURL, cfg.GitHubToken, cfg.Repository)

	numbers, err := runner.Run(ctx, cfg, allEntries, gh)
	if err != nil {
		return fmt.Errorf("failed to bump outdated tools (opened %d pull requests before failing): %w", len(numbers), err)
	}

	fmt.Fprintf(stderr, "opened %d pull request(s): %v\n", len(numbers), numbers)
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go test ./cmd/mise-bump-action/... -v`
Expected: PASS

- [ ] **Step 5: 全パッケージのビルド・テスト・lintを通す**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && go build ./... && go test ./... && golangci-lint run`
Expected: すべて成功

- [ ] **Step 6: Commit**

```bash
git add cmd/mise-bump-action
git commit -m "feat: main.goでconfig/outdated/runner/githubapiを結線する"
```

---

### Task 10: `action.yml` とリリースワークフロー

**Files:**
- Create: `action.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `cmd/mise-bump-action`のビルド成果物(`GOOS=linux GOARCH=amd64 go build`)

**設計メモ:** `github.action_ref`はcomposite actionで保証された値ではない(GitHub公式リポジトリでも未解決のissueとして扱われている)。そのため、ダウンロードするリリースタグは`action.yml`にハードコードし、リリースのたびに`make release VERSION=vX.Y.Z`でこの文字列とタグを同時に進める運用にする(Task 1のMakefile `release`ターゲット)。

- [ ] **Step 1: action.yml を作成**

```yaml
name: "mise-bump-action"
description: "Bump mise-managed tool versions with Dependabot-style pull requests"

inputs:
  mise-config-path:
    description: "Path(s) to mise.toml, newline-separated for multiple files"
    required: false
    default: "mise.toml"
  pr-strategy:
    description: "per-tool (one PR per tool) or single (bundle all into one PR)"
    required: false
    default: "per-tool"
  labels:
    description: "Comma-separated labels to add to opened pull requests"
    required: false
    default: "dependencies"
  base-branch:
    description: "Base branch for pull requests (defaults to the current ref)"
    required: false
    default: ""

runs:
  using: "composite"
  steps:
    - name: Download mise-bump-action binary
      shell: bash
      run: |
        curl -sSL -o "${{ github.action_path }}/mise-bump-action" \
          "https://github.com/sgash708/mise-bump-action/releases/download/v0.1.0/mise-bump-action_linux_amd64"
        chmod +x "${{ github.action_path }}/mise-bump-action"

    - name: Run mise-bump-action
      shell: bash
      env:
        INPUT_MISE_CONFIG_PATH: ${{ inputs.mise-config-path }}
        INPUT_PR_STRATEGY: ${{ inputs.pr-strategy }}
        INPUT_LABELS: ${{ inputs.labels }}
        INPUT_BASE_BRANCH: ${{ inputs.base-branch }}
        GITHUB_TOKEN: ${{ github.token }}
      run: "${{ github.action_path }}/mise-bump-action"
```

`v0.1.0`はプレースホルダの初版バージョン文字列。実際のリリース時に`make release VERSION=vX.Y.Z`がこの行を書き換える(Task 1のMakefile参照)。

- [ ] **Step 2: リリースワークフローを作成**

`.github/workflows/release.yml`:

```yaml
name: release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  build-and-release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"

      - name: Build linux/amd64 binary
        run: GOOS=linux GOARCH=amd64 go build -o mise-bump-action_linux_amd64 ./cmd/mise-bump-action

      - name: Create GitHub Release
        env:
          GITHUB_TOKEN: ${{ github.token }}
        run: gh release create "${GITHUB_REF_NAME}" mise-bump-action_linux_amd64 --generate-notes
```

- [ ] **Step 3: actionlintで検証する**

Run: `cd /Users/suga.naoya/works/yamashita/source/mise-bump-action && actionlint .github/workflows/release.yml`
Expected: エラーなし(warning/error 0件)

- [ ] **Step 4: action.yml の構文を手動確認する**

`runs.using: composite`、`steps[].shell`が全stepに指定されていること、`inputs.*.default`が想定通りであることを目視で確認する(actionlintは`action.yml`のcomposite定義自体はカバーしないため)。

- [ ] **Step 5: Commit**

```bash
git add action.yml .github/workflows/release.yml
git commit -m "feat: composite action定義とリリースワークフローを追加"
```

---

## Self-Review 結果

- **Spec coverage:** 設計docの「処理フロー」「PRフォーマット」「設定インターフェース」「v0スコープと配布」は Task 2〜10 で実装対象になっている。「エラーハンドリング」は実データ調査の結果、mise自体が失敗ツールを黙って除外することが判明したため、Task 2のRunの説明とGlobal Constraintsに反映済み。「テスト方針」(fixtureベースのユニットテスト、GitHub APIはモック/フェイクサーバ)はTask 2・7・8で満たしている。
- **未実装として残る既知の項目(spec記載の「未確定・今後決める事項」通り、v0スコープ外):** 複数`mise-config-path`の実リポジトリでの動作検証、PRの自動rebase/自動マージ相当の機能。
- **Placeholder scan:** 各Taskのコードブロックはすべて実際に動くコード。「TBD」「後で実装」等の記述なし。
- **Type consistency:** `outdated.Entry`(Task 2)のフィールド名(`Name`/`Requested`/`Latest`/`RelPath`)は Task 4・5・7 で一貫して同じ名前で参照している。`runner.GitHub`/`runner.BumpPRInput`(Task 7)は Task 8 の`githubapi.Client`が構造的に実装し、`var _ runner.GitHub = (*Client)(nil)`で静的に保証する。
