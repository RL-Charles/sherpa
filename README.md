# ⛰ Sherpa

**A terminal ticket system for you and your coding agents.**
Epics, stories and tickets live as markdown files in your repo. You browse and edit them in a TUI;
agents claim, log and submit work from the CLI. Every step lands in an audit trail, so you always
know what was done, by whom, and against which commit.

![Sherpa: ticket tree on the left, rendered markdown with a boxed table and the worklog on the right](docs/sherpa-preview.png)

## Features

- **Plain markdown in git.** One `.md` file per item under `.sherpa/`. Diff, review and merge it like code.
- **Epic → story → ticket.** A tree view with live status, progress and colour.
- **Rendered markdown.** Headings, checklists, code, text trees and fully boxed tables, right in the terminal.
- **Obsidian-style editing, with vim keys.** Press `e`: the block under your cursor turns into raw markdown and everything else stays rendered.
- **Built for agents.** Agents claim tickets, log typed entries and submit with how the work was verified. Only a human can mark work done.
- **A readable worklog.** Generated from the journal and grouped into work sessions, with commit ranges and line counts.
- **Live.** The TUI picks up agent activity every two seconds.

![Editing T-0008 in vim NORMAL mode: the paragraph under the cursor is raw, the rest stays rendered](docs/sherpa-edit.png)

## Install

Requires Go 1.27 or later.

```bash
go install github.com/RL-Charles/sherpa/cmd/shp@latest
```

Optionally, alias the full name: `ln -s "$(go env GOPATH)/bin/shp" ~/.local/bin/sherpa`.

## Quick start

```bash
cd your-project
shp init --key APP                                   # creates .sherpa/
shp new epic "Auth"
shp new story "Login" -p E-001
shp new ticket "JWT refresh" -p S-001 -m "Rotate tokens on expiry"
shp                                                  # open the TUI
```

### In the TUI

| Key | Action |
| --- | --- |
| `j` `k` / arrows | Move |
| `h` `l` / `enter` | Collapse, expand, toggle |
| `tab` | Switch between tree and preview |
| `e` | Edit the selected item (vim keys: `i` insert, `:wq` save & close, `:q!` discard) |
| `w` | Show every worklog session |
| `?` / `q` | Help / quit |

### From the CLI

```bash
shp ls                     # tree of everything
shp context                # what's active, in flight, ready and in review
shp show T-0001            # one item, rendered, with its worklog
shp start T-0001           # claim it and open a work session
shp log "Add refresh endpoint" -f api/auth.go
shp log -k decision "Rotate on use, not on a timer" -d "Simpler revocation"
shp submit -m "Refresh endpoint" --verified "go test ./auth"
shp done T-0001            # humans only
shp activity               # audit trail across all items
```

Every command accepts `--json`.

## Working with agents

Add this to your agent instructions (`CLAUDE.md`, `AGENTS.md`, …):

```markdown
## Work protocol
1. `shp context` to see what to work on.
2. `shp start <T-id>` before editing code. If no ticket fits, create one: `shp new ticket "…" -p S-xxx`.
3. Log as you go: `shp log "headline" -f file`, or `-k decision` / `-k verify` / `-k blocker`, with `-d` for details.
4. Finish with `shp submit -m "what changed" --verified "how it was checked"`.
5. Never mark work done; a human does that.
```

Sherpa enforces the important parts: agents can't log to a ticket they haven't claimed, can't
submit without saying how the work was verified, and can't mark anything done. Claude Code is
detected automatically (`CLAUDECODE=1`); for other agents, set `SHERPA_ACTOR=agent:<name>`.

## How it's stored

```text
.sherpa/
├─ config.yaml          # project key and name
├─ epics/E-001.md       # YAML frontmatter + markdown body
├─ stories/S-001.md     # parent: E-001
├─ tickets/T-0001.md    # parent: S-001
└─ sessions/*.jsonl     # append-only journal; the worklog is built from it
```

## Status

Early and moving fast. Sherpa tracks its own development: clone this repo and run `shp` inside it
to browse the roadmap, from Claude Code hooks and an MCP server to the kanban board and vim visual mode.

## Build from source

```bash
git clone https://github.com/RL-Charles/sherpa && cd sherpa
go build -o bin/shp ./cmd/shp
go test ./...
```
