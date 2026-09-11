# ADR 0019: legacy PRの一致判定をタイトル完全一致からツール名+対象バージョンに変更する

## Status

Accepted

## Date

2026-09-12

## Context

Fableによる5回目の敵対的レビューで、ADR 0018が導入したタイトル完全一致
ガード自体が新たなリグレッションであると指摘された。

`OpenBumpPR`はlegacy branch名で見つかったPRを「このbumpと同一」と
認めるために、そのPRのtitleが`in.PRTitle`(今回`prtext.Build`が
生成したtitle)と完全一致することを要求していた(ADR 0018)。しかし
`in.PRTitle`は`chore(deps): bump <shortName> from <Requested> to
<Latest>`という形式で、`Requested`(現在のピン値)を含む。以下のいずれ
かが起きるだけで、同一のbump(同じツール・同じ対象バージョン)なのに
文字列としては一致しなくなる。

1. 過去にこのPRを閉じた後、ユーザーが手動で一部だけバージョンを
   上げた(`Requested`が変わる)。
2. `mise-config-path`の設定を1本から複数本に増減した(タイトルに
   ` in <path>`が付く/消える)。
3. PRタイトルを人が編集した(GitHub上でよくある操作)。

`findClosedUnmergedPR`は最初に見つかったunmerged PRのtitleだけを見て
ループを抜けていたため、同一branch名に複数のclosed PRの履歴がある
場合、先頭のPRのtitleがたまたま不一致だと、後続に本当に一致する
PRがあっても見逃していた。

結果として、v1.6.0(branch名の一致のみで判定していたバージョン)では
正しく「再オープンしない」と判定できていたケースが、ADR 0018の
title完全一致要求によって「一致しない」と誤判定され、一度閉じた
bumpが復活する方向にリグレッションしていた。ADR 0016→0017→0018と
続いてきた「修正Aが修正Bを部分的に無効化する」パターンの5回目。

## Decision

### 1. 一致判定のキーをPRタイトル全体からツール名+対象バージョンに変更する

`BumpPRInput`に`LegacyMatchName`(`prtext.ShortName`が生成する短縮
ツール名)と`LegacyMatchVersion`(対象バージョン、`outdated.Entry.Latest`)
を追加する。`runner`はsingle-entryグループについてのみこれを設定し
(`legacyBranchNames`が空になるgroupedバンプでは空文字のまま)、
`githubapi.Client`はlegacy branch名で見つかったPRのtitleに対して、
`in.PRTitle`との完全一致ではなく、`bumpTitleMatches`で判定する。

`bumpTitleMatches`は当初`\b<toolName>\b`(単語境界)でツール名を
探す実装にしたが、これはレビューで却下された: Goの`regexp`では
`-`が非単語文字のため、`\bbar\b`は`foo-bar`という文字列にも一致
する。まさにlegacy branch名が衝突する典型例(`go:github.com/foo/bar`
→ ShortName `bar` と `go:github.com/foo-bar` → ShortName `foo-bar`)
が、ShortNameどうし「一方が他方の中に単語として現れる」関係になる
ため、単語境界だけでは衝突ガードとして機能しない。最終的に
`bump <toolName> from `と` to <targetVersion>(?: in |$)`という、
タイトルの固定フォーマット(`prtext.Build`が生成する文字列そのもの)
に沿ったリテラル文字列一致にした。これによりツール名は
「`bump `と` from `に挟まれた区間全体」として一致が要求され、
`bar`が`foo-bar`にも一致するような部分一致は起きない。

これにより、`Requested`の変化・` in <path>`サフィックスの有無・
その他のタイトル編集は一致判定に影響しなくなり、ツール名と対象
バージョンさえ一致すれば「同一bump」と認識される。

### 2. `findClosedUnmergedPR`はマッチ述語をループ内で評価する

`findClosedUnmergedPR`のシグネチャを`(title string, closed bool, err
error)`から`(closed bool, err error)`に変更し、`matches func(title
string) bool`を引数に取るようにした。closed unmerged PRを列挙する
ループの内側で`matches`を評価し、最初に一致したものが見つかった時点で
`true`を返す。`BranchName`自体の検索では`matchAnyTitle`(常に`true`)
を渡し、`LegacyBranchNames`の各候補では`bumpTitleMatches`を渡す。

これにより、同一branch名の履歴に複数のclosed PRがあり、先頭のPRの
titleがこのbumpと無関係でも、後続に一致するPRがあれば正しく検出
される。

## Rules

- 「同一のドメインオブジェクト(このbump)」を識別するキーは、生成物
  そのもの(PRタイトル全体)ではなく、その生成物が本質的に依存する
  最小限の属性(ツール名・対象バージョン)に絞る。生成物全体の完全
  一致で識別すると、その生成物の非本質的な部分(前回値・オプション
  サフィックス・人の手による編集)が変わるたびに識別が壊れる。
- 複数件を返しうるAPI応答に対して「マッチする1件を探す」ロジックを
  書くときは、マッチ述語をループの内側で評価する。先頭要素だけを見て
  返す実装は、複数件が返る現実のケース(同一branchへの複数回の
  open/close)で静かに間違った判定をする。

## Consequences

- 過去に閉じたPRについて、`Requested`が変わった・`mise-config-path`
  の本数が変わった、のいずれが起きても「同一bump」として正しく
  検出され、黙らせたはずのbumpが復活しなくなった。タイトルが
  人の手で編集され`bump <name> from `/` to <version>`という
  固定フォーマット自体が崩れた場合は一致しない(この形を保ったまま
  でなければ、`bar`と`foo-bar`のような衝突ペアをリテラル一致で
  弾けなくなるため、意図的なトレードオフ)。
- `findClosedUnmergedPR`が複数のclosed PR履歴を正しく全件チェック
  するようになり、先頭のPRがたまたま無関係でも後続の一致を見逃さ
  なくなった。
- `bumpTitleMatches`をタイトルの固定フォーマットに沿ったリテラル
  一致にしたことで、ADR 0018の元々の完全一致が防いでいた
  「ShortNameどうしが部分文字列関係にある衝突ペア」(`bar`と
  `foo-bar`等)も引き続き正しく拒否される。残存リスクはADR 0018と
  同一で、「同じShortNameかつ同じ対象バージョンへ、legacy形式時代
  に別のバックエンドがたまたま同時に存在した場合」に限定される。
  これはADR 0018の対応方針どおり、legacy branch名対応自体を
  v2.0.0で撤去することで解消する。

## References

- `internal/githubapi/client.go`(`OpenBumpPR`, `findClosedUnmergedPR`, `bumpTitleMatches`)
- `internal/runner/runner.go`(`BumpPRInput.LegacyMatchName/LegacyMatchVersion`, `legacyMatchName`, `legacyMatchVersion`)
- `internal/prtext/prtext.go`(`ShortName`、runnerからも参照できるようexport)
- `cmd/mise-bump-action/main.go`(`config.FromEnv`失敗パスでも`opened-count`/`pr-numbers`を設定するよう、`$GITHUB_OUTPUT`のオープンを設定読み込みより前に移動)
- [ADR 0018](0018-legacy-branch-title-guard-and-scope-closure.md)
- [ADR 0015](0015-pr-numbers-opened-count-outputs.md)（`opened-count`/`pr-numbers`を常に設定する方針の初出）
