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
internal/model/     Item, Kind, Status, IDs, deliverables, code map, templates
internal/store/     markdown+frontmatter persistence under .sherpa/
internal/workflow/  actor detection, transition rules
internal/journal/   per-session JSONL audit log (source of every worklog)
internal/worklog/   journal → sessions → timeline rendering
internal/gitinfo/   HEAD sha + diff stats for log entries
internal/render/    lipgloss tree, Glamour markdown, deliverables + code map
internal/cli/       cobra commands (deliverable, where, lint, brief structure)
internal/tui/       Bubble Tea app: tree, live-preview editor (blocks.go, editor.go, vim.go), auto-reload
```

## Work protocol (mandatory for agents)

1. `./bin/shp context` — see active, ready and in-review work.
2. `./bin/shp start <T-id>` before editing any code. This opens a work session. No ticket fits? Create one under the right story with `./bin/shp new ticket "…" -p S-xxx --template feature` (also `bug`, `spike`, `chore`).
3. Run `./bin/shp brief <T-id>` for the outcome, deliverables, next check, code map and authored context. Then pin the brief before the diff:
   - `./bin/shp deliverable add "outcome the ticket promises"` (repeat for each outcome)
   - `./bin/shp where add path/to/file.go --symbols Type,Func --deliverable 1` (repeat for each file)
   - `./bin/shp check add --for 1 "unit: go test ./pkg -run TestX"`
   - `./bin/shp design <T-id> --stdin` for the shape, types/enums, pattern, invariants, contracts, edge cases and alternatives
4. Log as you go — one-line headline, facts in `-d`, files in `-f`:
   - progress: `./bin/shp log "Add boxed table renderer" -f internal/tui/table.go`
   - decision: `./bin/shp log -k decision "Line splitter, not goldmark" -d "goldmark lists have no positions"`
   - verify:   `./bin/shp log -k verify "go test ./... passes"`
   - blocker:  `./bin/shp log -k blocker "Need API key for MCP smoke test"`
5. Keep tickets readable: `./bin/shp now "one-line state"`, `./bin/shp check <n> -e <evidence>` (what you can't verify yourself: `./bin/shp check add --manual "…" --how "steps"`, humans tick those), `./bin/shp measure "<name>" <value> --target <goal>`; epics need ≥2 Goal bullets (`./bin/shp new epic "…" -g "…" -g "…"`); stories/epics get a Visual (`./bin/shp visual <id> --stdin`).
6. Run `./bin/shp lint <T-id>`; fix missing Why/Context/Design, deliverables without checks, ticked checks without evidence and missing code-map entries.
7. Open PRs with `gh pr create` (Sherpa links them; `./bin/shp sync` for others).
8. Finish with a PR-style submit: `./bin/shp submit -m "what changed" --verified "how it was checked" [--followup "gap"]` (warns about missing PR/checks/deliverables/code map). Agent submits land in **agent review**.
   - Asked to review agent work? `./bin/shp review <T-id> --pass -m "what you checked"` sends it to human review; `--reject -m "what to fix"` sends it back to in progress.
9. Close a ticket when every deliverable and check is resolved: `./bin/shp done <T-id>` (no human approval needed; `--approved-by <name>` is optional audit). Stories and epics are closed only by a human after review; agents cannot close them.
10. Update a description with `./bin/shp edit <id> --stdin`. Never write a worklog into ticket markdown; it is generated from `.sherpa/sessions/`.
11. Commit messages include a `Ticket: T-xxxx` line.
