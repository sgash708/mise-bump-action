# アーキテクチャ決定記録 (ADR)

mise-bump-action における**確定済みの判断**を1ファイル1決定で記録する場所。

設計全体の背景は [`.agents/docs/specs/2026-09-11-mise-bump-action-design.md`](../specs/2026-09-11-mise-bump-action-design.md) を参照。本ディレクトリは「なぜそうしたか」を残す。

## 一覧

| # | タイトル | 関連領域 |
|---|---|---|
| [0001](0001-delegate-version-resolution-to-mise-cli.md) | ツールのバージョン解決はmise CLIに委譲し、複数バックエンドを自前実装しない | アーキテクチャ |
| [0002](0002-pr-creation-via-direct-github-api.md) | PR作成はGitHub API直呼び出しで自前実装し、サードパーティactionに依存しない | セキュリティ |
| [0003](0003-follow-dependabot-official-pr-format.md) | PRフォーマットはDependabot公式PRの規約(bump系タイトル + updated-dependenciesトレーラー)を踏襲する | UX |
| [0004](0004-v0-single-platform-prebuilt-binary.md) | v0はlinux/amd64向け事前ビルドバイナリのみで配布し、将来のマルチプラットフォーム移行に備えてインターフェースを固定する | 配布 |
| [0005](0005-conventional-commits-release-notes.md) | リリースノートはgit logからConventional Commitsのprefixで自作生成する | リリース |
| [0006](0006-outdated-bump-field-over-latest.md) | `mise outdated --json`は`latest`ではなく`bump`フィールドを優先して使う | 正確性 |

## 書き方

- ファイル名: `NNNN-kebab-case-title.md` (NNNN は4桁ゼロ埋め)
- 見出し構成: `Status` / `Date` / `Context` / `Decision` / `Rules` / `Consequences` / `References`
- 文体: 日本語、である調
- 連番は採用順。`Superseded` になっても番号は欠番にせずADR自体は残す
- 相互参照は `[ADR NNNN](NNNN-...md)` 形式
