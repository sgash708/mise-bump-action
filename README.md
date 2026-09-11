# mise-bump-action

**English** | [日本語](README.ja.md)

A GitHub Action that keeps tools managed by [mise](https://mise.jdx.dev/) (`mise.toml`) up to date, opening pull requests in the same style as [Dependabot](https://docs.github.com/en/code-security/dependabot).

Dependabot's `dependabot.yml` only understands its own built-in `package-ecosystem` list and can't see `mise.toml`. This action delegates version resolution to `mise` itself (`mise outdated`) and opens a Dependabot-style pull request for every tool that's behind.

Design rationale lives in [.agents/docs/adr/](.agents/docs/adr/README.md); the full design doc is at [.agents/docs/specs/2026-09-11-mise-bump-action-design.md](.agents/docs/specs/2026-09-11-mise-bump-action-design.md).

## Usage

Add a workflow like this to `.github/workflows/` in the repository that uses it:

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
      - uses: actions/checkout@v4
      - uses: jdx/mise-action@v2
      - uses: sgash708/mise-bump-action@v0.2.1
        with:
          mise-config-path: mise.toml
          pr-strategy: per-tool
```

## Examples

Ready-to-copy workflows for `.github/workflows/mise-bump.yml`:

| File | Use case |
|---|---|
| [`examples/single-tool/mise-bump.yml`](examples/single-tool/mise-bump.yml) | Basic single-file setup (`pr-strategy: per-tool`) |
| [`examples/grouped-tools/mise-bump.yml`](examples/grouped-tools/mise-bump.yml) | Bundle multiple tools into one PR (`pr-strategy: single`) |
| [`examples/monorepo/mise-bump.yml`](examples/monorepo/mise-bump.yml) | Monorepo with multiple `mise-config-path` values |

See [examples/](examples/README.md) for details on each pattern.

## Inputs

| input | description | default |
|---|---|---|
| `mise-config-path` | Path to the target `mise.toml`. For multiple files, pass a newline-separated multi-line string | `mise.toml` |
| `pr-strategy` | `per-tool` (one PR per tool) or `single` (bundle everything into one PR) | `per-tool` |
| `labels` | Labels to apply, comma-separated | `dependencies` |
| `base-branch` | Base branch for pull requests | the repository's default branch |

## Tech stack

- Go
- Distribution: composite action (v0 ships a pre-built linux/amd64 binary via GitHub Releases)
- Auth: the calling repository's default `GITHUB_TOKEN` only (no extra PAT required)

## Directory structure

```
.
├── AGENTS.md              # Project overview (for agents)
├── CLAUDE.md              # Symlink to AGENTS.md
├── README.md              # This file
├── examples/              # Configuration patterns (single-tool/ is a live demo, others are illustrative)
└── .agents/
    └── docs/
        ├── README.md      # Documentation index
        ├── adr/           # Architecture decision records
        ├── specs/         # Design docs
        └── plans/         # Implementation plans
```

## License

MIT
