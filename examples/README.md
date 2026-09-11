# Examples

mise-bump-action の設定パターン集。`single-tool/` だけが実際に
[`.github/workflows/mise-bump-example.yml`](../.github/workflows/mise-bump-example.yml)
から実行されるライブデモで、他は設定例を示すためだけの非実行ファイル。

## single-tool/ — 基本形(ライブデモ)

`mise-config-path` に1ファイルを指定する最小構成。`pr-strategy: per-tool`(既定)
なので、outdatedなツールごとに別PRが開く。

```yaml
- uses: sgash708/mise-bump-action@v0.2.0
  with:
    mise-config-path: examples/single-tool/mise.toml
    pr-strategy: per-tool
```

## grouped-tools/ — `pr-strategy: single`

複数ツールが同時にoutdatedな場合、`pr-strategy: single` で1PRにまとめられる。
[`mise.toml`](grouped-tools/mise.toml) はgolangci-lint/actionlintの2つを意図的に
古いバージョンにピンしてある。

```yaml
- uses: sgash708/mise-bump-action@v0.2.0
  with:
    mise-config-path: examples/grouped-tools/mise.toml
    pr-strategy: single
```

## monorepo/ — 複数`mise-config-path`

[`backend/mise.toml`](monorepo/backend/mise.toml) と
[`frontend/mise.toml`](monorepo/frontend/mise.toml) のように、モノレポで
サブディレクトリごとに`mise.toml`が分かれているケース。`mise-config-path`は
改行区切りの複数行文字列で指定する。

```yaml
- uses: sgash708/mise-bump-action@v0.2.0
  with:
    mise-config-path: |
      examples/monorepo/backend/mise.toml
      examples/monorepo/frontend/mise.toml
    pr-strategy: per-tool
```

## 注意: miseの設定は親ディレクトリとマージされる

`mise-config-path`で指定したファイルを含むディレクトリより上の階層に別の
`mise.toml`(このリポジトリならルートの`mise.toml`)がある場合、miseはそれも
マージして解決する。`single-tool/`のライブデモ実行時にルートの`mise.toml`の
ツールも一緒に検出されるのはこのため([ADR未記載、plan文書のTask 18/19追補参照](../.agents/docs/plans/2026-09-11-mise-bump-action-implementation.md))。
