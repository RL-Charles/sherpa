# ⛰ Sherpa

[![CI](https://github.com/RL-Charles/sherpa/actions/workflows/ci.yml/badge.svg)](https://github.com/RL-Charles/sherpa/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/RL-Charles/sherpa.svg)](https://pkg.go.dev/github.com/RL-Charles/sherpa)

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

**Binaries:** download the archive for your platform from
[Releases](https://github.com/RL-Charles/sherpa/releases), unpack it and put `shp` and `sherpa`
on your `PATH`. Linux, macOS and Windows builds are available for amd64 and arm64.

**With Go** (1.27 or later):

```bash
go install github.com/RL-Charles/sherpa/cmd/shp@latest github.com/RL-Charles/sherpa/cmd/sherpa@latest
```

This installs the same program under two names: `sherpa` for people, and `shp`, the short form
agents, hooks and docs use. Use whichever you like.

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
| `s` | Set the selected item's status (←/→ + enter, or `1`–`6`) |
| `b` | Switch between the tree and the board (tickets grouped by status) |
| `w` | Show every worklog session |
| `?` / `q` | Help / quit |

The mouse works too. Click a row to select it; click its arrow, or click it again, to expand or collapse it.
Click text in the preview to start editing with the cursor there, and click a worklog session header to show every session.

With hooks installed (`shp hooks install`), a spinner replaces a ticket's status glyph while an agent is working on it,
and the header shows who (`⠹ claude on T-0009`). It stops when the agent's turn ends or it submits.

### From the CLI

```bash
shp ls                     # tree of everything
shp context                # what's active, in flight, ready and in review
shp show T-0001            # one item, rendered, with its worklog
shp start T-0001           # claim it and open a work session
shp log "Add refresh endpoint" -f api/auth.go
shp log -k decision "Rotate on use, not on a timer" -d "Simpler revocation"
shp submit -m "Refresh endpoint" --verified "go test ./auth"   # agents → agent review
shp review T-0001 --pass -m "Checked diff and tests"           # agent review → review (--reject: back to in progress)
shp done T-0001            # humans; agents add --approved-by <human>
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
4. Finish with `shp submit -m "what changed" --verified "how it was checked"`. It lands in agent review.
5. When asked to review agent work: `shp review <id> --pass -m "…"` (on to human review) or `--reject -m "…"`.
6. Only mark work done when a human explicitly tells you to: `shp done <id> --approved-by <name>`.
```

Agent work passes two gates: **agent review**, where a human or another agent checks it and
passes it on or sends it back, then **review**, where a human signs it off. A human can also close
work straight from agent review.

Sherpa enforces the important parts: agents can't log to a ticket they haven't claimed, can't
submit without saying how the work was verified, and can only close reviewed work with a named
human approver, which the audit log records alongside the agent. Claude Code is
detected automatically (`CLAUDECODE=1`); for other agents, set `SHERPA_ACTOR=agent:<name>`.

Agents working in git worktrees (`git worktree add`, Claude Code's `.claude/worktrees/`) all
read and write the main checkout's `.sherpa/`, so the board shows their progress live; set
`SHERPA_DIR` to point somewhere else.

## Readable tickets

Agents keep structured data current with commands, so ticket prose stays human-written and
every ticket reads the same way:

```text
T-0030 · Add OAuth login
● done · ⇡ PR #35 merged · +412 −37 · checks 2/2 ✓ · needs T-0019 ●, T-0029 ●
Now: …one line on where things stand…
Goal / Work …                     ← human prose
Visual                            ← stories and epics: generated from children until you draw one
Measurables                       ← boxed table: measure · target · current ✓/✗ · evidence
Work done                         ← PR, branch, size, commits (linked automatically)
Checks · 2/2                      ← always last, each with its evidence
Worklog                           ← highlights; routine progress folds away (w shows all)
```

| Command | Does |
| --- | --- |
| `shp now "…"` | One-line current state at the top |
| `shp check <n> -e <sha\|PR\|path>` · `shp check add "…"` · `shp checks` | Acceptance checks with evidence (`--waive`, `--undo`) |
| `shp measure "<name>" <value> --target <goal> -e <ref>` | Rows of the Measurables table; ✓ when the target is met |
| `shp visual <S-id> --stdin` · `--auto` | A story/epic diagram; `--auto` prints the generated one |
| `shp depends <id> <dep…>` | Dependencies, shown as badges and in Visuals |
| `shp sync` | Link PRs (by branch or title, e.g. `app-30-…`, `T-0030`) and `Ticket:` commits; refresh PR state |

Agents open PRs with `gh pr create`; the hooks catch the URL and link it to the active ticket.
`shp submit` looks up the branch's PR and **warns** about a missing PR, open checks, unmet
measures or a missing Now line. Tickets in review show a review card with the summary,
verification, follow-ups and those warnings.

## Hooks: enforcement, not just instructions

`shp hooks install` wires Sherpa into Claude Code, Cursor, Codex and OpenCode (plus a git hook),
so agents can't quietly work untracked:

| When | What happens |
| --- | --- |
| Session starts | The agent is told what's active, in progress and ready |
| Before an edit | Blocked until a ticket is claimed (`shp start`), with instructions to fix it |
| After an edit | The file is recorded on the ticket ("files touched", diff stats) |
| Agent stops | Asked once to `shp log` or `shp submit` if files changed since the last entry |
| `git commit` | `Ticket: T-xxxx` added from the claimed ticket; agent commits without one are refused |

| Agent | Installed into | Notes |
| --- | --- | --- |
| Claude Code | `.claude/settings.json` | SessionStart, PreToolUse, PostToolUse, Stop |
| Cursor (IDE and `cursor-agent`) | `.cursor/hooks.json` | sessionStart, preToolUse, afterFileEdit, stop |
| Codex CLI ≥ 0.124 | `.codex/hooks.json` | Trust the project and approve the hooks in `/hooks` |
| OpenCode | `.opencode/plugins/sherpa.ts` | Plugin calls `shp hook … --agent opencode` |

Hooks fail open: if `shp` isn't installed or a repo has no `.sherpa/`, nothing is blocked.
`shp hooks status` shows what's installed; `shp hooks uninstall` removes only Sherpa's entries.
The active ticket is tracked per checkout, so run one agent per checkout (use worktrees for parallel agents).

## Importing from Agency

`shp import agency --mapping layout.yaml` converts `.agency/issues/` into epics, stories and
tickets. The mapping places every issue in a story; ticket numbers match issue numbers, comments
become worklog entries, and the originals are archived in `.sherpa/imports/agency/`.
Run `shp import agency --help` for the mapping format.

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
go build -o bin/ ./cmd/...      # bin/shp and bin/sherpa
go test ./...
```

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup and checks,
and the [Code of Conduct](CODE_OF_CONDUCT.md). Report security problems privately as described in
[SECURITY.md](SECURITY.md). Changes are listed in [CHANGELOG.md](CHANGELOG.md).

Agent hooks run as POSIX shell commands; on Windows use Git Bash or WSL for them.

## License

Sherpa is released under the [MIT License](LICENSE).
