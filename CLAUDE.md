# Sherpa

TUI + CLI ticket system (epics → stories → tickets) for humans and agents. Go, Charm libs.
This repo tracks its own work in `.sherpa/` using the `shp` binary.

## Build & test

```bash
mise exec -- go build -o bin/ ./cmd/...   # bin/shp and bin/sherpa (same program)
mise exec -- go test ./...
```

## Layout

```
cmd/shp/            entrypoint (short name; agents and hooks use shp)
cmd/sherpa/         same program under the product name
internal/model/     Item, Kind, Status, IDs
internal/store/     markdown+frontmatter persistence under .sherpa/
internal/workflow/  actor detection, transition rules
internal/journal/   per-session JSONL audit log (source of every worklog)
internal/worklog/   journal → sessions → timeline rendering
internal/gitinfo/   HEAD sha + diff stats for log entries
internal/render/    lipgloss tree, Glamour markdown
internal/cli/       cobra commands (bare `shp` launches the TUI)
internal/tui/       Bubble Tea app: tree, live-preview editor (blocks.go, editor.go, vim.go), auto-reload
```

## Work protocol (mandatory for agents)

1. `./bin/shp context` — see active, ready and in-review work.
2. `./bin/shp start <T-id>` before editing any code. This opens a work session. No ticket fits? Create one under the right story with `./bin/shp new ticket "…" -p S-xxx`.
3. Log as you go — one-line headline, facts in `-d`, files in `-f`:
   - progress: `./bin/shp log "Add boxed table renderer" -f internal/tui/table.go`
   - decision: `./bin/shp log -k decision "Line splitter, not goldmark" -d "goldmark lists have no positions"`
   - verify:   `./bin/shp log -k verify "go test ./... passes"`
   - blocker:  `./bin/shp log -k blocker "Need API key for MCP smoke test"`
4. Keep tickets readable: `./bin/shp now "one-line state"`, `./bin/shp check <n> -e <evidence>`, `./bin/shp measure "<name>" <value> --target <goal>`; stories/epics get a Visual (`./bin/shp visual <id> --stdin`).
5. Open PRs with `gh pr create` (Sherpa links them; `./bin/shp sync` for others).
6. Finish with a PR-style submit: `./bin/shp submit -m "what changed" --verified "how it was checked" [--followup "gap"]` (warns about missing PR/checks). Agent submits land in **agent review**.
   - Asked to review agent work? `./bin/shp review <T-id> --pass -m "what you checked"` sends it to human review; `--reject -m "what to fix"` sends it back to in progress.
7. Never mark work done on your own. Only when the human explicitly tells you to close specific in-review tickets: `./bin/shp done <T-id> --approved-by <their name> -m "why"`.
8. Update a description with `./bin/shp edit <id> --stdin`. Never write a worklog into ticket markdown; it is generated from `.sherpa/sessions/`.
9. Commit messages include a `Ticket: T-xxxx` line.
