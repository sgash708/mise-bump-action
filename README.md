# mise-bump-action

**English** | [日本語](README.ja.md)

[![Marketplace](https://img.shields.io/badge/marketplace-mise--bump--action-blue?logo=github)](https://github.com/marketplace/actions/mise-bump-action)
[![CI](https://github.com/sgash708/mise-bump-action/actions/workflows/ci.yml/badge.svg)](https://github.com/sgash708/mise-bump-action/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A GitHub Action that keeps tools managed by [mise](https://mise.jdx.dev/) (`mise.toml`) up to date, opening pull requests in the same style as [Dependabot](https://docs.github.com/en/code-security/dependabot).

Dependabot's `dependabot.yml` only understands its own built-in `package-ecosystem` list and can't see `mise.toml`. This action delegates version resolution to `mise` itself (`mise outdated`) and opens a Dependabot-style pull request for every tool that's behind.

Design rationale lives in [.agents/docs/adr/](.agents/docs/adr/README.md); the full design doc is at [.agents/docs/specs/2026-09-11-mise-bump-action-design.md](.agents/docs/specs/2026-09-11-mise-bump-action-design.md).

## Why not Renovate?

Renovate does natively support `mise.toml` — if it's already usable in your setup, it's the more capable option (dependency grouping, semver-range ignore rules, custom schedules, a much larger user base). This action exists for the case where Renovate itself is the obstacle, not the features:

- The hosted Mend Renovate App requires installing a third-party GitHub App, which in many organizations means a separate security review before it can touch any repository.
- Self-hosting Renovate means running and maintaining another service, plus learning its (large) configuration surface just to reproduce "open a PR when a pin is behind."

mise-bump-action trades Renovate's control surface for a lighter footprint: no GitHub App install, no separate service, just a workflow step authenticated with the repository's own `GITHUB_TOKEN`. The real cost of that trade is fewer safety nets — see [Limitations](#limitations) below before you rely on it for a team-wide rollout.

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
      - uses: actions/checkout@v7
      - uses: jdx/mise-action@v4
      - uses: sgash708/mise-bump-action@v1
        with:
          mise-config-path: mise.toml
          pr-strategy: per-tool
```

### One-time repo setup

GitHub disables "Actions can create pull requests" by default. Without it, this action's PR-creation step fails with a 403 (`GitHub Actions is not permitted to create or approve pull requests`), regardless of the `permissions:` block in the workflow above. Enable it once per repository:

- **UI**: Settings → Actions → General → Workflow permissions → check "Allow GitHub Actions to create and approve pull requests".
- **CLI**:
  ```bash
  gh api -X PUT repos/{owner}/{repo}/actions/permissions/workflow \
    -f default_workflow_permissions=write \
    -F can_approve_pull_request_reviews=true
  ```

## Examples

Ready-to-copy workflows for `.github/workflows/mise-bump.yml`:

| File | Use case |
|---|---|
| [`examples/single-tool/mise-bump.yml`](examples/single-tool/mise-bump.yml) | Basic single-file setup (`pr-strategy: per-tool`) |
| [`examples/grouped-tools/mise-bump.yml`](examples/grouped-tools/mise-bump.yml) | Bundle multiple tools into one PR (`pr-strategy: single`) |
| [`examples/monorepo/mise-bump.yml`](examples/monorepo/mise-bump.yml) | Monorepo with multiple `mise-config-path` values |

See [examples/](examples/README.md) for details on each pattern.

> **Note:** GitHub requires a maintainer to manually approve the first workflow
> run triggered on a pull request opened via `GITHUB_TOKEN` (even for
> same-repo, non-fork PRs) — this applies to the CI checks on PRs this action
> opens. Approve once from the PR's checks tab (or `gh api -X POST
> repos/{owner}/{repo}/actions/runs/{run_id}/approve`) and it won't ask again
> for that branch.

## Inputs

| input | description | default |
|---|---|---|
| `mise-config-path` | Path to the target `mise.toml`. For multiple files, pass a newline-separated multi-line string | `mise.toml` |
| `pr-strategy` | `per-tool` (one PR per tool) or `single` (bundle everything into one PR) | `per-tool` |
| `labels` | Labels to apply, comma-separated | `dependencies` |
| `base-branch` | Base branch for pull requests | the ref that triggered the run (`GITHUB_REF_NAME`) |
| `dry-run` | If `true`, print the intended pull request title/body/diff to the job summary without creating any branch or pull request | `false` |
| `ignore` | Comma-separated tool-name patterns to never bump: an exact name, or a prefix ending in `*` (e.g. `terraform,aqua:foo/*`) | `` (none) |
| `max-open-prs` | Cap on how many bump pull requests this action may have open at once (identified by branch name, not labels); existing ones count toward it. Replacing a stale PR for the same tool never counts as a net increase. `0` means unlimited | `0` |

## Outputs

| output | description |
|---|---|
| `pr-numbers` | Comma-separated pull request numbers opened this run (empty if none) |
| `opened-count` | How many pull requests were opened this run |

```yaml
- uses: sgash708/mise-bump-action@v1
  id: bump
- run: echo "Opened ${{ steps.bump.outputs.opened-count }} PR(s): ${{ steps.bump.outputs.pr-numbers }}"
```

## Pull request lifecycle

- Closing a pull request without merging it means "don't reopen this exact version" — the next run won't recreate it. A newer version is still proposed normally.
- With `pr-strategy: per-tool`, opening a new pull request for a tool automatically closes any older still-open pull request for that same tool, with a comment pointing at the new one.

## Upgrading

Versions through v1.4.0 used `mise-bump/<tool>-<version>` as the branch-name
format; v1.5.0 briefly changed it to `mise-bump/<tool>_<version>`; this
version fixes both to be collision-free going forward (ADR 0017). Opening or
closing a pull request under the current format still recognizes a pull
request opened under either older format (so upgrading won't reopen a bump
you already closed, or duplicate one that's still open) — but a stale pull
request opened before this version won't be auto-closed as superseded when
a newer bump for the same tool opens, since the two formats can't be told
apart safely by prefix alone. If you have open bump pull requests from
before v1.6.0, close them once after upgrading; every pull request opened
from now on uses the current format consistently.

Recognizing a pull request under an older branch-name format also requires
its title to identify the same tool and target version, to reject a
same-named-but-different tool that happens to collide under the old,
fingerprint-less format (ADR 0018, 0019). This was originally an exact
title match, but a title also encodes the "from" version at bump time and
an optional `mise-config-path`-count suffix — either of which changes (a
manual bump applied since, or a `mise-config-path` count change) made an
exact match miss a real match and resurrect a bump that had already been
closed; ADR 0019 fixed this by matching on tool name + target version
instead. This compatibility path (`LegacyBranchNames`) is scaffolding for
the migration above, not a permanent feature: it will be removed entirely
in the next major version, at which point only the current branch-name
format is recognized.

## Limitations

Trade-offs from favoring a light setup over Renovate/Dependabot's full feature set:

- **No major-version-only suppression.** `ignore` excludes a tool entirely; there's no "propose patches but not majors" mode (many mise-managed tools — arbitrary CLIs, language runtimes — don't follow strict semver closely enough for that rule to be reliable).
- **Only `mise.toml` is supported**, not `.tool-versions`. `mise-config-path` must point at a TOML file mise-bump-action can parse and rewrite in place.
- **Values it can't rewrite in place** (inline tables/arrays, e.g. `python = { version = "3.11" }`) are skipped per-entry with a note in the job summary, not treated as a fatal error — but they also never get bumped. Use `ignore` to silence the repeated notice.
- **Semi-automatic by design.** Authenticating with the caller's own `GITHUB_TOKEN` (ADR 0002) avoids needing an extra PAT, but it also means the first CI run on a PR this action opens needs a one-time manual approval (see the note in [Examples](#examples)) — unlike Dependabot, which runs as a verified first-party App with different treatment. There's no `github-token` input to supply a PAT/App token instead, which would let that first-run approval be automated away; this is a deliberate scope choice for personal/small-scale use, not an oversight (ADR 0018).
- **No overall run timeout, and no secondary (abuse-detection) rate limit handling.** `internal/githubapi.Client` retries a single 403/429 once (ADR 0008), but doesn't back off further or bound the whole run's wall-clock time. A pathological run (many outdated tools, a misbehaving API) could run long or trip GitHub's secondary rate limits. Acceptable for the personal/individual-repository scale this action targets; not recommended for a shared, high-tool-count monorepo without adding this yourself upstream (e.g. a workflow-level `timeout-minutes`).
- **Stale-branch replacement is name-based, not history-based.** Before creating a branch, `OpenBumpPR` deletes any existing ref at the same name (see ADR 0009's troubleshooting note) rather than verifying it's actually this action's own abandoned branch. In practice only this action ever writes to its own `mise-bump/*` namespace, so this is a theoretical rather than observed risk.
- **The end-to-end test (`e2e/`) runs against a mocked GitHub API and a fake `mise` binary**, not a real repository or a real `mise outdated`. It verifies the action's own logic (grouping, PR text, output writing) end-to-end, but not `mise`'s own version-resolution behavior or genuine GitHub API quirks.

## Tech stack

- Go
- Distribution: composite action (pre-built linux/amd64 and linux/arm64 binaries via GitHub Releases; macOS/Windows runners are not supported)
- Auth: the calling repository's default `GITHUB_TOKEN` only (no extra PAT required)

## Directory structure

```
.
├── AGENTS.md              # Project overview (for agents)
├── CLAUDE.md              # Symlink to AGENTS.md
├── README.md              # This file
├── examples/              # Configuration patterns (single-tool/ is a live demo, others are illustrative)
└── .agents/
    ├── docs/
    │   ├── README.md      # Documentation index
    │   ├── adr/           # Architecture decision records
    │   ├── specs/         # Design docs
    │   └── plans/         # Implementation plans
    └── agents/            # Subagent definitions (symlinked from .claude/agents/)
```

## License

MIT
