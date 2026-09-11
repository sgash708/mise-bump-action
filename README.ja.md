# mise-bump-action

[English](README.md) | **日本語**

[mise](https://mise.jdx.dev/) (`mise.toml`) で管理しているツールのバージョンを、
[Dependabot](https://docs.github.com/en/code-security/dependabot) と同じPR体裁で自動追従させるGitHub Action。

Dependabotの`dependabot.yml`は公式`package-ecosystem`しか受け付けず、`mise.toml`を検知できない。
本actionは`mise`本体(`mise outdated`)にバージョン解決を委譲し、差分があったツールについて
Dependabot風のPRを作成する。

設計判断の背景は [.agents/docs/adr/](.agents/docs/adr/README.md)、詳細設計は
[.agents/docs/specs/2026-09-11-mise-bump-action-design.md](.agents/docs/specs/2026-09-11-mise-bump-action-design.md) を参照。

## 使い方

利用側リポジトリの`.github/workflows/`に、以下のようなreusable workflowを置く。

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
      - uses: sgash708/mise-bump-action@v0.2.4
        with:
          mise-config-path: mise.toml
          pr-strategy: per-tool
```

## Examples

そのままコピーして`.github/workflows/mise-bump.yml`に置ける例:

| ファイル | 用途 |
|---|---|
| [`examples/single-tool/mise-bump.yml`](examples/single-tool/mise-bump.yml) | 基本形(1ファイル・`pr-strategy: per-tool`) |
| [`examples/grouped-tools/mise-bump.yml`](examples/grouped-tools/mise-bump.yml) | 複数ツールを1PRにまとめる(`pr-strategy: single`) |
| [`examples/monorepo/mise-bump.yml`](examples/monorepo/mise-bump.yml) | モノレポで複数`mise-config-path`を指定 |

各パターンの詳細は [examples/](examples/README.md) を参照。

## Inputs

| input | 説明 | 既定値 |
|---|---|---|
| `mise-config-path` | 対象の`mise.toml`パス。複数指定時は改行区切りの複数行文字列 | `mise.toml` |
| `pr-strategy` | `per-tool`(ツールごとに別PR) / `single`(1PRにまとめる) | `per-tool` |
| `labels` | 付与するラベル(カンマ区切り) | `dependencies` |
| `base-branch` | PRのベースブランチ | リポジトリの既定ブランチ |
| `dry-run` | `true`にするとブランチ/PRを作成せず、意図したPRのタイトル・本文・diffをjob summaryに出力する | `false` |

## 技術スタック

- Go
- 配布形態: composite action(v0はlinux/amd64向け事前ビルドバイナリをGitHub Releaseで配布)
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
    └── docs/
        ├── README.md      # ドキュメント索引
        ├── adr/           # アーキテクチャ決定記録
        ├── specs/         # 設計ドキュメント
        └── plans/         # 実装計画
```

## ライセンス

MIT
