# Changelog

All notable changes to Sherpa are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Ticket briefs: `shp deliverable` (outcomes), `shp where` (code map: files, symbols,
  change type and the deliverable each serves), and `shp check add --for <n>` to link a check
  to a deliverable. Rendering nests checks under the outcome they prove and prints Where as a
  directory-grouped map.
- Ticket templates: `shp new ticket --template feature|bug|spike|chore` scaffolds the right
  sections, including a Design section for types/enums, patterns, invariants, contracts, edge
  cases and alternatives. `--deliverable` adds outcomes at creation time.
- `shp design [id] --stdin` reads or replaces just the Design section, so an agent can add
  types/enums and invariants without rewriting the rest of the ticket.
- `shp lint [id]`: template-aware, advisory checks for missing Why/Context/Design, deliverables
  without checks, ticked checks without evidence, vague checks, unfilled template placeholders
  and missing code maps (`--strict` for CI; active tickets by default, `--all` for everything).
- `shp submit` now warns about deliverables without checks, invalid deliverable links and a
  missing code map.
- `shp brief [id]`: the compact handoff — outcome, deliverables, next open check, code map,
  authored Context/Design/Out of scope, and remaining gaps. The review prompt now starts with it.
- `shp migrate brief [id…]`: preview-first upgrade for existing tickets. It recovers a deliverable
  from the title, links unlinked checks, adds code-map entries from files already recorded by
  hooks, detects the template, sets `Now` from the latest log and can move `Description` to `Why`.
  It never invents Why/Context/Design/Out of scope; preview is the default and `--write` applies
  only the mechanical fields. The hook session context now names deliverables and the code map.
- Review flow: manual (✋) checks only humans tick (`shp check add --manual --how`); an
  independent reviewer agent launched on agent submit (`reviewer` in `.sherpa/config.yaml`,
  `shp review --spawn`), and a session can't review its own work; `.sherpa/policy.yaml` lets a
  passing agent review close low-risk work (`shp policy`); TUI needs-you inbox (`n`, `a`, `x`).
- Epic goals are a bullet list of at least two deliverables: `shp new epic -g "…" -g "…"`;
  `shp edit` refuses fewer, and the TUI warns on save.
- The agent-working spinner also animates the ticket's story and epic, even when collapsed.
- Fuzzy search: `/` in the TUI (live preview, enter jumps to the item) and `shp search`, matching
  IDs, titles, labels and assignees, then ticket text; `is:<status|kind>` and `@assignee` filters.
- Epics, stories and tickets stored as markdown with YAML frontmatter in `.sherpa/`, with an
  append-only JSONL journal.
- `shp`/`sherpa` CLI: `init`, `new`, `ls`, `show`, `start`, `log`, `submit`, `done`, `context`,
  `activity`, `edit`, `--json` everywhere.
- TUI: item tree, Glamour-rendered preview with fully boxed tables, Obsidian-style live-preview
  editor with vim keybindings, board and status views, mouse support, live reload.
- Readable tickets: Now line, acceptance checks with evidence, measurables, dependencies,
  generated Visuals for stories and epics, Work done with linked PRs and commits, review cards,
  worklog highlights.
- Agent hooks for Claude Code, Cursor, Codex and OpenCode, plus a git `commit-msg` hook
  (`shp hooks install`): session context, edits blocked until a ticket is claimed, touched files
  recorded, a nudge to log before stopping, `Ticket:` trailers, automatic PR linking.
- Shared board for agents working in git worktrees.
- `shp sync` to link pull requests and commits; `shp import agency` and `shp backfill`.
- MIT license, contributing guide, code of conduct, security policy, CI and release builds.

### Changed
- TUI readability pass: title-first rows with right-aligned checks, age and PR metadata (`v` cycles
  minimal/compact/labelled), a pinned title/status strip, capped prose width, section rules, an
  adaptive light/dark palette and a status-coloured selection gutter.
- Review cards no longer repeat the check count, and the worklog links to the review instead of
  repeating its message, verification and follow-ups.
- Worklog sessions use a left rail, a generated one-line summary and a single folded-routine chip.
- Dates in ticket headers are relative (`3d ago`) instead of absolute timestamps.

[Unreleased]: https://github.com/RL-Charles/sherpa/commits/main
