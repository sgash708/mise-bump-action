# ADR 0018: legacy branch名の衝突をPRタイトルで防ぎ、据え置き項目をLimitationsとして確定する

## Status

Accepted

## Date

2026-09-12

## Context

Fableによる4回目の敵対的レビューで、ADR 0017自体が新たに持ち込んだ問題と、
3回以上据え置かれてきた項目の扱いが指摘された。

1. **legacy branch名検索がsanitizeの衝突をもう一段深いところで復活させる**:
   `legacyBranchNames`はv1.0.0〜v1.5.0形式を再現するために`sanitize(name)`
   のみを使い、`nameFingerprint`を混ぜない(混ぜてしまうと過去に実在した
   branch名と一致しなくなり、そもそも移行の意味がなくなるため)。しかし
   これは、`go:github.com/foo/bar`と`go:github.com/foo-bar`のように
   異なるツール名が同一のsanitize結果に潰れる場合、legacy branch名も
   完全に一致してしまうことを意味する。`OpenBumpPR`がbranch名の一致だけで
   「既存のPR」と判定していたため、片方のツールの過去PRがもう片方の
   判定に誤って使われ、恒久的なskipや誤ったPRへの言及が起きうる状態
   だった。ADR 0017自身が導入した仕組みが、ADR 0017が直したはずの衝突を
   legacy経路で再導入していた。
2. **legacy branch名検索に撤去条件がない**: ツール数 × 3リクエスト
   (現行 + 旧2形式)が今後も恒久的なAPIコストとして積み上がる。
3. **3回以上据え置かれてきた5項目**(token入力なし、全体タイムアウトなし、
   secondary rate limit未対応、ブランチ削除が名前一致のみ、E2Eが実mise
   非経由)が、直すとも直さないとも意思決定されないまま繰り返し「今回は
   スコープ外」として先送りされていた。

## Decision

### 1. legacy branch名の一致は、PRタイトルの完全一致も要求する

`OpenBumpPR`は、`BranchName`(現行形式)の一致はそのまま「既存」と扱うが、
`LegacyBranchNames`のいずれかの一致は、見つかったPRのtitleが
`in.PRTitle`(このbumpに対して`prtext.Build`が生成する、v1.0.0から
フォーマットが変わっていないtitle)と完全一致する場合のみ「既存」と
扱う。一致しなければ、そのPRは無関係な別ツールのものとみなし、次の
候補を試すか、通常どおり新規PRを作成する。

これは緩和策であってdegre保証ではない: 同じツールshortName(パス末尾
セグメント)かつ同じバージョンへ、legacy形式時代に別のバックエンド
(例: `aqua:x/bar`と`go:y/bar`)がたまたま同時に存在した場合はtitleも
一致してしまいうる。現行形式(fingerprint付き)ではこの残存リスクは
存在しない。legacy branch名対応そのものを次のメジャーバージョンで
撤去する(下記2.)ことで、この残存リスクの寿命を区切る。

### 2. legacy branch名対応は次のメジャーバージョンで撤去する

`LegacyBranchNames`・`legacyBranchNames`関数・上記のtitle突合ロジックは、
恒久機能ではなく移行期のみの互換レイヤーとして扱う。次のメジャー
バージョン(v2.0.0)でコードごと削除する予定とし、README「Upgrading」に
明記する。撤去後にv1.6.0より前のPRが残っている場合の扱いは、v2.0.0への
移行時に改めてUpgradingへ追記する。これにより、ツール数に比例して
増え続けるAPIコスト(現行+旧2形式で3倍)と、残存衝突リスクの両方に
期限を設ける。

### 3. 据え置き5項目は「直さない」と明示してLimitationsに閉じる

token入力、全体タイムアウト、secondary rate limit対応、ブランチ削除の
安全化、実mise経由のE2Eの5項目は、今回のリリースでも実装しない。
これを4回目の「言及なき先送り」にはせず、README/README.ja.mdの
Limitationsに個人・小規模利用を前提としたスコープ上の意図的な選択と
して明記し、今後の要望が来るまではこの姿勢を既定とする。実装するか
どうかの判断そのものをドキュメント化することで、レビューのたびに
同じ5項目が「未解消」として再指摘される状態を解消する。

## Rules

- ブランチ名でPRを同定する仕組みに互換レイヤー(legacy検索)を足す場合は、
  同定に使うキー(branch名)がその後衝突しうることを疑い、branch名以外の
  独立した属性(タイトル・ラベル等)でも一致を要求する。
- 互換レイヤーには撤去条件(次のメジャーバージョンで削除、等)を必ず
  決めてドキュメント化する。「いつか消す」ではなく「vX.0.0で消す」と
  書く。
- スコープ外にする項目は、指摘されるたびに黙って見送るのではなく、
  Limitationsのような一次ドキュメントに理由込みで明記し、意思決定
  そのものを完了させる。

## Consequences

- `go:github.com/foo/bar`と`go:github.com/foo-bar`のように異なる
  ツール名がlegacy branch名で衝突しても、タイトルの不一致で誤判定
  されなくなった。
- legacy branch名対応がv2.0.0で撤去される予定であることが明文化され、
  「直すか閉じるか」が「次のメジャーで閉じる」という形で確定した。
- token入力・全体タイムアウト・secondary rate limit・ブランチ削除の
  安全化・実mise E2Eの5項目は、「据え置き」ではなく「個人・小規模
  利用のスコープでは対応しない」という明示的な判断として
  Limitationsに記録された。

## References

- `internal/githubapi/client.go`(`OpenBumpPR`, `findOpenPR`, `findClosedUnmergedPR`)
- `internal/githubapi/client_test.go`(`openPRStub`, `closedPRStub`)
- README「Upgrading」「Limitations」節
- [ADR 0017](0017-branch-name-migration-pagination-opened-count-fix.md)
