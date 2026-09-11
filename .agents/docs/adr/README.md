# アーキテクチャ決定記録 (ADR)

mise-bump-action における**確定済みの判断**を1ファイル1決定で記録する場所。

設計全体の背景は [`.agents/docs/specs/2026-09-11-mise-bump-action-design.md`](../specs/2026-09-11-mise-bump-action-design.md) を参照。本ディレクトリは「なぜそうしたか」を残す。

## 一覧

| # | タイトル | 関連領域 |
|---|---|---|
| [0001](0001-delegate-version-resolution-to-mise-cli.md) | ツールのバージョン解決はmise CLIに委譲し、複数バックエンドを自前実装しない | アーキテクチャ |
| [0002](0002-pr-creation-via-direct-github-api.md) | PR作成はGitHub API直呼び出しで自前実装し、サードパーティactionに依存しない | セキュリティ |
| [0003](0003-follow-dependabot-official-pr-format.md) | PRフォーマットはDependabot公式PRの規約(bump系タイトル + updated-dependenciesトレーラー)を踏襲する | UX |
| [0004](0004-v0-single-platform-prebuilt-binary.md) | v0はlinux向け事前ビルドバイナリのみで配布し、将来のマルチプラットフォーム移行に備えてインターフェースを固定する(amd64/arm64両対応) | 配布 |
| [0005](0005-conventional-commits-release-notes.md) | リリースノートはgit logからConventional Commitsのprefixで自作生成する | リリース |
| [0006](0006-outdated-bump-field-over-latest.md) | `mise outdated --json`は`latest`ではなく`bump`フィールドを優先して使う | 正確性 |
| [0007](0007-sanitize-upstream-release-body.md) | アップストリームrelease本文の@メンション/#issue参照をサニタイズしてから埋め込む | セキュリティ/UX |
| [0008](0008-single-retry-on-rate-limit.md) | GitHub API呼び出しは403/429を`Retry-After`に従って一度だけリトライする | 信頼性 |
| [0009](0009-line-based-mise-toml-rewrite.md) | `mise.toml`の書き換えはTOMLライブラリでの完全パースではなく正規表現ベースの行単位置換にする | アーキテクチャ |
| [0010](0010-pr-lifecycle-respects-closed-prs-and-closes-superseded.md) | closeされたPRは再生成せず、古いバージョンのPRはsupersededとして自動closeする | UX/信頼性 |
| [0011](0011-build-provenance-attestation.md) | バイナリの真正性はGitHub Artifact Attestation(Sigstore)で検証する | セキュリティ |
| [0012](0012-ignore-and-max-open-prs-inputs.md) | `ignore`(ツール除外)と`max-open-prs`(PR数上限)を入力として追加する | UX |
| [0013](0013-per-entry-skip-for-unsupported-value-forms.md) | 未対応の値形式(inline table/array)は個別エントリだけスキップし、グループ全体を失敗させない | 信頼性 |
| [0014](0014-pr-body-truncation.md) | PR本文はGitHubの65,536文字上限に合わせて切り詰める | 信頼性 |
| [0015](0015-pr-numbers-opened-count-outputs.md) | `pr-numbers`/`opened-count`をaction outputとして公開する | UX |
| [0016](0016-fix-branch-collision-deadlock-label-and-ref-bugs.md) | ブランチプレフィックス衝突・max-open-prsデッドロック・ラベル衝突・`/head`誤検知の4件を修正する | 信頼性 |
| [0017](0017-branch-name-migration-pagination-opened-count-fix.md) | ブランチ名の無告知破壊的変更・ページネーション欠如・opened-countの二重計上を修正する | 信頼性/互換性 |
| [0018](0018-legacy-branch-title-guard-and-scope-closure.md) | legacy branch名の衝突をPRタイトルで防ぎ、据え置き項目をLimitationsとして確定する | 信頼性/スコープ |

## 書き方

- ファイル名: `NNNN-kebab-case-title.md` (NNNN は4桁ゼロ埋め)
- 見出し構成: `Status` / `Date` / `Context` / `Decision` / `Rules` / `Consequences` / `References`
- 文体: 日本語、である調
- 連番は採用順。`Superseded` になっても番号は欠番にせずADR自体は残す
- 相互参照は `[ADR NNNN](NNNN-...md)` 形式
