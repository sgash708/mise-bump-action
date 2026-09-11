# ADR 0006: `mise outdated --json`は`latest`ではなく`bump`フィールドを優先して使う

## Status

Accepted

## Date

2026-09-11

## Context

`mise outdated --json --bump`の各エントリには`latest`(制約を無視した絶対的な最新版)と
`bump`(そのツールのピン形式が持つ制約を尊重した次バージョン)の2つのフィールドがある。
`internal/outdated.Parse`は当初`latest`をそのまま採用していたが、これは誤りだった。

たとえば`node = "20"`のようなfuzzy pin(メジャーバージョンのみ指定)の場合、`latest`は
`22.x`のような無関係に新しいメジャーバージョンまで含んでしまう。`bump`はfuzzy pinの意図
(メジャー20系列内での更新)を尊重した値を返す。`latest`をそのまま使うと、意図しないメジャー
バージョンへ強制的にジャンプするPRを開いてしまう。

## Decision

`internal/outdated.Parse`は各エントリについて`bump`フィールドを優先し、`bump`が空の場合のみ
`latest`にフォールバックする。

```go
target := e.Bump
if target == "" {
    target = e.Latest
}
```

## Rules

- `mise outdated`の出力を扱うコードは、`latest`ではなく`bump`を版解決の基準にする。
- `bump`が空になるケース(mise側が算出できなかった場合)のみ`latest`を許容する。

## Consequences

- 厳密ピン(`go = "1.26.1"`のような完全指定)では`bump`と`latest`は基本的に同じ値になるため、
  この変更は挙動に影響しない。
- fuzzy pin(`node = "20"`等)を使うリポジトリでも、意図しないメジャーバージョンへの
  ジャンプを防げる。

## References

- `internal/outdated/outdated.go`
- `.agents/docs/plans/2026-09-11-mise-bump-action-implementation.md` Task 18追補(`--bump`フラグ自体の必要性の経緯)
