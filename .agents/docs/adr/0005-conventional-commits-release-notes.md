# ADR 0005: リリースノートはgit logからConventional Commitsのprefixで自作生成する

## Status

Accepted

## Date

2026-09-11

## Context

`gh release create --generate-notes`はマージされたPull Requestを起点に変更点を集計する。
本リポジトリの運用は基本的にmainへの直push(mise-bump-actionが開くbump PRのマージを除く)であり、
`--generate-notes`は実態を反映しなかった。実際に発生した問題:

- `v0.1.2`: コミットが全て直pushだったため、生成されたリリースノート本文が空になった。
- `v0.2.0`: デモ用のbot PRだけを拾い、`New Contributors: @github-actions[bot]`という無意味な内容になった。

## Decision

`--generate-notes`をやめ、`.github/workflows/release.yml`内でgit logベースのリリースノートを自作する。

- `actions/checkout`に`fetch-depth: 0`を指定し、tag履歴を含む全コミット履歴を取得する。
- `git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "${GITHUB_REF_NAME}^"`で直前のsemverタグを検出する
  (floating majorタグ`v0`/`v1`はこのパターンにマッチしないため誤検出しない)。
- 直前タグから現タグまでの`git log --no-merges --pretty=format:"- %s (%h)"`をConventional Commitsのprefix
  (`feat`/`fix`/`perf`/`docs`/`test`/`refactor`/`build`/`ci`/`revert`/`chore`)ごとに`--grep`で分類し、
  カテゴリ見出し付きのMarkdownを組み立てる。
- `--grep`パターンは`^${prefix}(\(.*\))?!?:`とし、スコープ付き(`fix(scope):`)やbreaking change
  マーカー付き(`feat!:`)のコミットも正しく分類する。
- 初回リリース(直前タグが存在しない)の場合は全履歴を対象にする。
- 組み立てたMarkdownを`gh release create --notes-file`で渡す。

## Rules

- コミットメッセージは常にConventional Commitsのprefixで始める(このリポジトリのグローバル規約と一致)。
- release.ymlの分類カテゴリを増やす場合は、対応するprefixをコミット規約にも追加する。

## Consequences

- mainへの直push中心の運用でも、実際の変更内容を反映したリリースノートが生成される。
- `--generate-notes`が持つ「PRへのリンク」「New Contributors」等の付加情報は失われる
  (本リポジトリの運用では実質的にノイズだったため許容)。

## References

- `.github/workflows/release.yml`
- `.agents/docs/plans/2026-09-11-mise-bump-action-implementation.md` Task 24追補
