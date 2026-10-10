# Changelog

All notable changes to Sherpa are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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

[Unreleased]: https://github.com/RL-Charles/sherpa/commits/main
