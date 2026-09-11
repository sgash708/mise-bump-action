# Contributing

## Dev commands

Tasks are defined in `mise.toml` (run `mise tasks` to list them):

```
mise run build    # go build
mise run test     # go test ./...
mise run lint     # golangci-lint run
mise run fmt      # gofmt + goimports
VERSION=vX.Y.Z mise run release   # bump action.yml, tag vX.Y.Z, move the floating vN tag
```

## Windows: enable symlink support before cloning

This repo tracks a few paths as real git symlinks (mode `120000`):

- `CLAUDE.md` -> `AGENTS.md`
- `.claude/docs` -> `../.agents/docs`
- `.claude/skills` -> `../.agents/skills`

On Windows, git only creates real filesystem symlinks for these if
`core.symlinks` is enabled *before* the clone, and Developer Mode (or an
elevated shell) is available. Otherwise each of the paths above checks out
as a plain text file containing the link target string instead of the
linked file/directory.

To clone correctly on Windows:

```
git config --global core.symlinks true
git clone https://github.com/sgash708/mise-bump-action.git
```

If you already cloned without this set, re-checkout after enabling it:

```
git config core.symlinks true
git checkout -- CLAUDE.md .claude/docs .claude/skills
```

None of this affects using the action itself (`uses:
sgash708/mise-bump-action@vX`) — it only matters if you're editing this
repository's own source.
