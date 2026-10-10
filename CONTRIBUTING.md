# Contributing to Sherpa

Thanks for helping. Bug reports, ideas and pull requests are all welcome.

## Ground rules

- Be kind; this project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
- Report security problems privately, as described in [SECURITY.md](SECURITY.md), not in issues.
- For anything bigger than a small fix, open an issue first so we can agree on the approach.

## Development setup

You need Go 1.27 or later (a `mise.toml` pins the version if you use [mise](https://mise.jdx.dev)).

```bash
git clone https://github.com/RL-Charles/sherpa && cd sherpa
go build -o bin/ ./cmd/...     # bin/shp and bin/sherpa (the same program)
go test ./...
./bin/shp                      # open the TUI on Sherpa's own tickets
```

Before sending a pull request:

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./...
```

CI runs the same checks on every pull request.

## Sherpa tracks its own work

This repository dogfoods Sherpa: tickets live in `.sherpa/`, and the agent protocol is in
[CLAUDE.md](CLAUDE.md). You don't have to use it to contribute, but if you do:

```bash
./bin/shp context                       # what's ready
./bin/shp start T-0042                  # claim a ticket
./bin/shp log "what you did" -f path    # record progress
./bin/shp submit -m "summary" --verified "how you tested it"
```

Never edit files in `.sherpa/` by hand; use `shp` so the journal stays consistent.

## Pull requests

- Keep each PR focused on one change, with tests for new behaviour.
- Write commit messages in the imperative ("Add board view"), and name the ticket if there is one
  (`Ticket: T-0042`).
- Update `README.md` and `CHANGELOG.md` (under *Unreleased*) when behaviour changes.
- By submitting a contribution you agree it is licensed under the project's [MIT License](LICENSE).

## Project layout

```text
cmd/shp, cmd/sherpa   entry points (same program, two names)
internal/model        items, statuses, checks, measures, links
internal/store        markdown + frontmatter files under .sherpa/
internal/journal      append-only JSONL audit log
internal/worklog      journal → sessions → timeline
internal/cli          cobra commands
internal/tui          Bubble Tea app: tree, live-preview editor (vim keys), board
internal/hooks        agent hooks (Claude Code, Cursor, Codex, OpenCode) and git commit-msg
internal/links        GitHub PR and commit linking (via gh)
internal/importer     Agency importer
```
