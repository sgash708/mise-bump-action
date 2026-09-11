# mise-bump-action

[English](README.md) | **日本語**

[![Marketplace](https://img.shields.io/badge/marketplace-mise--bump--action-blue?logo=github)](https://github.com/marketplace/actions/mise-bump-action)
[![CI](https://github.com/sgash708/mise-bump-action/actions/workflows/ci.yml/badge.svg)](https://github.com/sgash708/mise-bump-action/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[mise](https://mise.jdx.dev/) (`mise.toml`) で管理しているツールのバージョンを、
[Dependabot](https://docs.github.com/en/code-security/dependabot) と同じPR体裁で自動追従させるGitHub Action。

Dependabotの`dependabot.yml`は公式`package-ecosystem`しか受け付けず、`mise.toml`を検知できない。
本actionは`mise`本体(`mise outdated`)にバージョン解決を委譲し、差分があったツールについて
Dependabot風のPRを作成する。

設計判断の背景は [.agents/docs/adr/](.agents/docs/adr/README.md)、詳細設計は
[.agents/docs/specs/2026-09-11-mise-bump-action-design.md](.agents/docs/specs/2026-09-11-mise-bump-action-design.md) を参照。

## なぜRenovateではないのか

Renovateは`mise.toml`をネイティブサポートしており、既にRenovateを使える状況なら
そちらの方が高機能(依存関係のグルーピング、semver範囲でのignore、柔軟なスケジュール、
圧倒的に大きいユーザーベース)。本actionが存在する理由は機能面ではなく、Renovate
「自体」が導入の壁になるケースへの対応:

- ホスト版のMend Renovate Appはサードパーティ製GitHub Appのインストールが必要で、
  組織によってはリポジトリに触れる前に個別のセキュリティ審査が必要になる。
- self-hosted Renovateは別サービスの運用・保守に加え、「ピンが古ければPRを開く」
  だけのために広大な設定サーフェスを学ぶコストがかかる。

mise-bump-actionはRenovateの制御機能と引き換えに、導入の軽さを取る: GitHub App
のインストールも別サービスも不要で、リポジトリ自身の`GITHUB_TOKEN`だけで動く
workflowステップ1つで完結する。その代償は実際にあるので、チーム展開の前に
下記の[制限事項](#制限事項)を確認してほしい。

## 使い方

利用側リポジトリの`.github/workflows/`に、以下のようなワークフローを置く。

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
      - uses: sgash708/mise-bump-action@v1
        with:
          mise-config-path: mise.toml
          pr-strategy: per-tool
```

### 初回だけ必要なリポジトリ設定

GitHubは既定で「Actionsがpull requestを作成できる」設定を無効にしている。これが
無効なままだと、workflowの`permissions:`ブロックに関わらず、本actionのPR作成ステップが
403(`GitHub Actions is not permitted to create or approve pull requests`)で失敗する。
リポジトリごとに一度だけ有効化する。

- **UI**: Settings → Actions → General → Workflow permissions →
  「Allow GitHub Actions to create and approve pull requests」にチェック。
- **CLI**:
  ```bash
  gh api -X PUT repos/{owner}/{repo}/actions/permissions/workflow \
    -f default_workflow_permissions=write \
    -F can_approve_pull_request_reviews=true
  ```

## Examples

そのままコピーして`.github/workflows/mise-bump.yml`に置ける例:

| ファイル | 用途 |
|---|---|
| [`examples/single-tool/mise-bump.yml`](examples/single-tool/mise-bump.yml) | 基本形(1ファイル・`pr-strategy: per-tool`) |
| [`examples/grouped-tools/mise-bump.yml`](examples/grouped-tools/mise-bump.yml) | 複数ツールを1PRにまとめる(`pr-strategy: single`) |
| [`examples/monorepo/mise-bump.yml`](examples/monorepo/mise-bump.yml) | モノレポで複数`mise-config-path`を指定 |

各パターンの詳細は [examples/](examples/README.md) を参照。

> **注意:** GitHubの仕様上、`GITHUB_TOKEN`で作成されたPR(同一リポジトリ内・fork
> でない場合も含む)に対する最初のworkflow runは、リポジトリの管理者が手動で承認
> しないと実行されない。このactionが開いたPRのCIチェックにも当てはまる。PRの
> checksタブから一度承認する(または`gh api -X POST
> repos/{owner}/{repo}/actions/runs/{run_id}/approve`)と、以降そのブランチでは
> 再度聞かれない。

## Inputs

| input | 説明 | 既定値 |
|---|---|---|
| `mise-config-path` | 対象の`mise.toml`パス。複数指定時は改行区切りの複数行文字列 | `mise.toml` |
| `pr-strategy` | `per-tool`(ツールごとに別PR) / `single`(1PRにまとめる) | `per-tool` |
| `labels` | 付与するラベル(カンマ区切り) | `dependencies` |
| `base-branch` | PRのベースブランチ | 実行をトリガーしたref(`GITHUB_REF_NAME`) |
| `dry-run` | `true`にするとブランチ/PRを作成せず、意図したPRのタイトル・本文・diffをjob summaryに出力する | `false` |
| `ignore` | 恒久的に除外するツール名パターン(カンマ区切り)。完全一致、または末尾`*`の前方一致(例: `terraform,aqua:foo/*`) | (なし) |
| `max-open-prs` | 本actionが同時に開いていてよいbump PRの上限数(ラベルではなくブランチ名で識別)。既存のPRも数に含む。同じツールの古いPRを置き換えるbumpは正味の増加として数えない。`0`は無制限 | `0` |

## Outputs

| output | 説明 |
|---|---|
| `pr-numbers` | 今回開いたPR番号(カンマ区切り、無ければ空) |
| `opened-count` | 今回開いたPRの数 |

```yaml
- uses: sgash708/mise-bump-action@v1
  id: bump
- run: echo "Opened ${{ steps.bump.outputs.opened-count }} PR(s): ${{ steps.bump.outputs.pr-numbers }}"
```

## PRのライフサイクル

- PRをmergeせずにcloseした場合、そのバージョンは再提案されない(次回実行で再生成されない)。新しいバージョンが出れば通常通り提案される。
- `pr-strategy: per-tool`の場合、同じツールの新しいPRを開くと、そのツールの古いopen PRを自動的にcloseし、新しいPRへのリンクをコメントする。

## アップグレード時の注意

v1.4.0までは`mise-bump/<tool>-<version>`、v1.5.0では一時的に
`mise-bump/<tool>_<version>`というブランチ名形式を使っていた。本バージョンでは
両方とも恒久的に衝突しない形式へ修正した(ADR 0017)。PRのopen/close判定は
旧いずれの形式で開かれたPRも引き継いで認識する(アップグレードしても、
既にcloseしたbumpが復活したり、既にopenなPRが重複作成されたりはしない)。
ただし、本バージョンより前に開かれたstaleなPRは、同じツールの新しいbumpが
開かれてもsuperseded(古いPRの自動close)の対象にはならない — 2つの形式を
prefix一致だけで安全に区別できないため。v1.6.0より前に開かれたbump PRが
残っている場合は、アップグレード後に一度だけ手動でcloseしてほしい。
以降に開かれるPRはすべて現行形式で一貫するため、この対応は一度きりで済む。

旧いずれかのブランチ名形式でのPR認識は、ツール名 + 対象バージョンの一致も
必須にした。fingerprintを持たない旧形式では、異なるツール名が同じブランチ名に
衝突しうるため、別ツールのPRを誤って「既存」と判定しないようにするための措置
(ADR 0018, 0019)。当初はPRタイトルの完全一致を要求していたが、タイトルには
bump当時の「from」バージョンや`mise-config-path`の本数に応じたサフィックスが
含まれるため、手動でバージョンを一部上げた後や設定変更後は文字列として一致
しなくなり、閉じたはずのbumpが復活する副作用があった(ADR 0019で修正)。
この互換レイヤー(`LegacyBranchNames`)は移行期のためだけの足場であり、
恒久機能ではない。次のメジャーバージョンでコードごと撤去し、以降は現行形式の
ブランチ名のみを認識する。

## 制限事項

導入の軽さをRenovate/Dependabotの機能の一部と引き換えにしている。

- **メジャーバージョンだけの抑止はできない。** `ignore`はツールを丸ごと除外する仕組みで、
  「パッチは提案するがメジャーは提案しない」というモードは無い(mise管理下のツールは
  任意のCLIや言語ランタイムを含み、厳密なsemverに従わないものが多く、この種のルールの
  信頼性が低くなるため)。
- **`mise.toml`のみ対応。** `.tool-versions`は対象外。`mise-config-path`は
  mise-bump-actionがその場で書き換えられるTOMLファイルを指す必要がある。
- **その場で書き換えられない値形式**(inline tableや配列、例: `python = { version = "3.11" }`)
  は、そのエントリだけをスキップしjob summaryに記録する(実行全体は失敗させない)が、
  そのツールは永久にbumpされない。同じ通知を繰り返したくない場合は`ignore`で除外する。
- **半自動である。** 呼び出し元自身の`GITHUB_TOKEN`で認証する設計(ADR 0002)は追加PAT
  を不要にするが、代わりに本actionが開いたPRの最初のCI実行は手動承認が1回必要になる
  ([Examples](#examples)の注意書き参照)。Dependabotのような検証済み公式Appとは
  扱いが異なるため。この最初の承認をPAT/App tokenで自動化できるような
  `github-token`入力は存在しない。個人・小規模利用を前提にした意図的な
  スコープ判断であり、見落としではない(ADR 0018)。
- **全体タイムアウト・secondary rate limit対応はない。** `internal/githubapi.Client`は
  403/429を`Retry-After`に従って一度だけリトライする(ADR 0008)が、それ以上の
  バックオフや実行全体の時間上限は設けていない。対象ツール数が多い、あるいは
  APIが不調な場合に実行が長引いたり、secondary rate limitに抵触したりしうる。
  本actionが想定する個人・単一リポジトリ規模では許容範囲だが、ツール数の多い
  共有monorepoでの利用にはこのまま推奨しない(必要ならworkflow側の
  `timeout-minutes`等で補うこと)。
- **staleブランチの置き換えはブランチ名一致のみで判断する。** `OpenBumpPR`は
  新規ブランチ作成前に同名refが存在すればまず削除する(ADR 0009のトラブル
  シューティング参照)が、それが本当に本action自身が残した古いブランチかは
  検証していない。本actionの`mise-bump/*`名前空間には他に書き込むものが
  ない前提のため、理論上のリスクであり実害の報告はない。
- **E2Eテスト(`e2e/`)はモック化したGitHub APIと偽の`mise`バイナリに対して実行する**。
  実リポジトリや実際の`mise outdated`は経由しない。action自体のロジック
  (グルーピング・PR文面・output書き込み)はend-to-endで検証するが、`mise`本体の
  バージョン解決挙動や実際のGitHub APIの癖までは検証しない。

## 技術スタック

- Go
- 配布形態: composite action(linux/amd64・linux/arm64向け事前ビルドバイナリをGitHub Releaseで配布。macOS/Windowsランナーは非対応)
- 認証: 利用側リポジトリの既定`GITHUB_TOKEN`のみ(追加のPAT不要)

## ディレクトリ構成

```
.
├── AGENTS.md              # プロジェクト概要(エージェント向け)
├── CLAUDE.md              # AGENTS.md へのシンボリックリンク
├── README.md              # 英語版(公式)
├── README.ja.md           # このファイル
├── examples/              # 設定パターン集(single-tool/はライブデモ、他は非実行)
└── .agents/
    ├── docs/
    │   ├── README.md      # ドキュメント索引
    │   ├── adr/           # アーキテクチャ決定記録
    │   ├── specs/         # 設計ドキュメント
    │   └── plans/         # 実装計画
    └── agents/            # サブエージェント定義(.claude/agents/からシンボリックリンク)
```

## ライセンス

MIT
