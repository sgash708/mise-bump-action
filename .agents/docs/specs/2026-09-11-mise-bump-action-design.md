# mise-bump-action 設計

## 背景・目的

`mise.toml`(または`.mise.toml`)で管理しているツール(terraform, go, node, aquaレジストリ経由のCLI, go install経由のツール等)は、Dependabotの`package-ecosystem`に対応するものが無いため自動追従できない。Renovateはmiseをネイティブサポートするが、ログインユーザーが限定される制約があり採用しない。

そこで、Dependabotが出すPRと同じ体裁で、mise管理下のツールのバージョンアップPRを自動で出すGitHub Actionを自作する。

## リポジトリ

- 名前: `mise-bump-action`
- 所有: `sgash708`(個人アカウント)配下。v1リリースとMarketplace公開に合わせてpublicにする(それまではprivateで開発)
- ライセンス: MIT(手元の他OSSリポジトリ`chromagic`/`errmagic`と同じ)
- 言語: Go

## アーキテクチャ概要

Goで書いた単一バイナリとして実装し、GitHub Actionsのcomposite actionとして配布する。

利用側リポジトリは以下のような短いワークフローを`.github/workflows/`に置くだけで使える(Dependabotの`dependabot.yml`が公式ecosystem以外のupdaterを受け付けないため、composite action呼び出し + cronという形が実質的な代替になる。GitHub Actionsの「reusable workflow」(`workflow_call`)とは別の仕組みで、通常のワークフローからcomposite actionを1ステップとして呼ぶだけ)。

```yaml
on:
  schedule:
    - cron: "0 3 * * 1"
  workflow_dispatch: {}

jobs:
  mise-bump:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v7
      - uses: jdx/mise-action@v4
      - uses: sgash708/mise-bump-action@v0
        with:
          mise-config-path: mise.toml
          pr-strategy: per-tool
```

## 処理フロー(バイナリ内部)

1. `mise outdated --json`相当を実行し、現在ピンと最新版の差分を取得する。
   - `[tools]`直下の短縮名、`"aqua:owner/repo"`、`"go:module/path"`など複数バックエンドの版解決は`mise`本体に任せる(自前実装しない)。
2. 差分があったツールについて、`pr-strategy`設定に従いPR単位をまとめる。
   - `per-tool`(既定): ツールごとに別PR。Dependabotの標準的な挙動に合わせる。
   - `single`: 全ツールの差分を1PRにまとめる。
3. 各PRについて、GitHub REST APIを直接呼び出し、以下を行う(サードパーティのPR作成action `peter-evans/create-pull-request` 等は使わず、依存を自バイナリのみに閉じてサプライチェーンリスクを下げる)。
   - branch作成
   - `.mise.toml`書き換えcommit
   - PR作成
4. 認証は各利用リポジトリの既定`GITHUB_TOKEN`(`contents: write`, `pull-requests: write`権限)で完結させる。PAT等の追加シークレットは不要。

## PRフォーマット(Dependabotの実際のPR形式を踏襲)

実際のDependabot PRを参考に、以下の規約を踏襲する。

- **PRタイトル**: mise管理下のツールは`go = "1.26.1"`のような厳密ピンであり、Terraformの`~>`のようなrange指定ではないため、Dependabotの`bump`系タイトル規約を採用する。
  - 例: `chore(deps): bump go from 1.26.1 to 1.27.0`
  - 複数ファイルを扱う場合は末尾に対象パスを付与する(例: `chore(deps): bump go from 1.26.1 to 1.27.0 in mise.toml`)。
- **commitメッセージ**: Dependabot公式の`updated-dependencies:` YAML trailerをそのまま踏襲する。

  ```
  chore(deps): bump go from 1.26.1 to 1.27.0

  ---
  updated-dependencies:
  - dependency-name: go
    dependency-version: 1.27.0
    dependency-type: direct:production
  ...
  ```

- **ラベル**: `labels` inputで指定したラベル(カンマ区切り、既定`dependencies`)をそのまま全PRに付与する。ツール別の自動ラベル付与は行わない。

## 設定インターフェース(action input)

| input | 説明 | 既定値 |
|---|---|---|
| `mise-config-path` | 対象の`mise.toml`パス。複数指定時は改行区切りの複数行文字列 | `mise.toml` |
| `pr-strategy` | `per-tool` / `single` | `per-tool` |
| `labels` | 付与するラベル(カンマ区切り) | `dependencies` |
| `base-branch` | PRのベースブランチ | リポジトリの既定ブランチ |

## エラーハンドリング

PRグループ(pr-strategyが`per-tool`ならツール1件、`single`なら同一ファイル内の全ツール)単位で失敗しても他のグループの処理は止めない。失敗したグループは`errors.Join`でまとめてエラーとして返し、成功したグループのPR番号は正常に返す。警告/エラーはstderrに出力する(`$GITHUB_STEP_SUMMARY`はdry-runプレビュー専用)。一時的なネットワークエラーやmise未対応のバックエンドを想定。

## v0スコープと配布

- v0はGitHub-hosted ubuntu runner(linux/amd64)のみを対象とする。
- ビルド済みバイナリをタグ付きでGitHub Releaseに添付し、composite actionが実行時にダウンロードする(`go install`ランタイムビルドは採用しない)。
- 将来GoReleaser等でOS/arch別ビルドに移行する場合も、利用側workflowのインターフェース(`uses: sgash708/mise-bump-action@vX`)は変えない。

## テスト方針

- `mise outdated`の出力をfixtureとして与えるユニットテストで、差分検出・PRグルーピングロジックを検証する。
- GitHub API呼び出し部分は`net/http/httptest`ベースの手書きmuxモックで検証し、実際のGitHub APIには実行時のみ触れる。

## v0スコープ外

- PRの自動マージ・自動rebase等、Dependabotの拡張コマンド(`@dependabot rebase`等)相当の機能は v0 スコープ外。
- `mise-config-path`に複数ファイル(monorepo)を指定するケースは実装・テスト済み(`examples/monorepo/`、`internal/grouping`の`groupByFile`)。
