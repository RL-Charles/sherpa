# Ticket clarity: a design proposal for Sherpa

> Status: proposal · Phase 0 (readability) and Phase 1 core (deliverables, code map,
> templates, lint) implemented 24 Oct 2026. `shp where plan` reconciliation is Phase 3.
> Shipped: `shp deliverable`, `shp where`, `shp check add --for`, `shp design`,
> `shp lint`, `shp brief`, `shp migrate brief`, and
> `--template feature|bug|spike|chore`.
> Still planned: plan drift, automatic evidence capture, per-file stats, MCP.
> Scope: ticket content model, TUI readability, work management, agent integration.
> Visual companion: `docs/ticket-clarity-mockup.html`

Sherpa already stores the right facts. The problem is that it presents them as one
flat markdown document and leaves the most important question — *where does this
land in the code?* — implicit until after the work is done.

This is a proposal for making a ticket answer five questions in one screen, and
for giving agents the same concise picture the human gets.

---

## 1. The five questions

A ticket exists to let someone decide and act. In practice, every reader asks:

1. **Why does this exist?** What problem, for whom, and what changes if we do it?
2. **What will exist when it is done?** The deliverables, in outcomes — not tasks.
3. **Where does it land?** Files, packages, symbols, contracts, migrations.
4. **How will we know?** Checks, evidence, measures — mapped to deliverables.
5. **What is the state, and what is next?** Status, now, blockers, worklog.

Today's answer quality:

| Question | Where it lives today | Problem |
| --- | --- | --- |
| Why | `## Description`, optional `## Why` | Often `_No description yet._`; no prompt for constraints, prior art, non-goals |
| What exists when done | `checks` | Checks are *verification*, not outcomes; there is no deliverable list |
| Where | `## Work done` (PR, commits, diffstat) | Only after the fact. No planned file/symbol map, no "what changes here" |
| How we know | `checks` + optional evidence | Evidence is a free string, not attached to a check; often absent |
| State / next | `Now`, status, worklog | The strongest part, but repeated in 3–4 places and buried below the prose |

**Readability is not mainly a font/colour problem. It is an information-architecture
problem.** The current renderer concatenates: header → body → generated sections →
worklog. Everything has the same visual weight and no navigation.

---

## 2. What "hard to read" looks like today

Take a real ticket in this repo, `T-0046`. The same facts are rendered four times:

```
checks 0/4                                       ← badge line
> ☑ checks 0/4                                   ← review card
## Checks · 0/4                                  ← checks section
12:44  ◑ submitted for review                    ← worklog …
       Agents can close in-review items …           … repeating the submit message
       ✓ verified: go test ./...                    … repeating verification
       → follow-up: …                               … repeating follow-ups
```

Four representations, three wordings, zero links to the evidence, and the
worklog is the longest thing on the page. A reviewer has to reconstruct the
story instead of reading it.

Other concrete rough edges found in the code:

- **The ID is louder than the title.** `idCol` is cyan and the title is default;
  the title should be the primary scan target. (`internal/tui/tree.go`, `render/render.go`)
- **Status is glyph-only in the tree.** Seven similar circles (`○ ◎ ◐ ◒ ◑ ✖ ●`)
  must be memorised. The header uses `in_progress` with an underscore while the
  picker uses "in progress".
- **Colour collisions.** `agent_review` is cyan, and so is every ID. `review` is
  magenta, and so is a merged PR. Backlog is grey, and so is "faint".
- **Hard-coded dark-terminal colours.** `lipgloss.Color("237")` for a selected
  row, fixed ANSI 1–8 for status. On a light terminal several of these fail
  contrast; `NO_COLOR` is not honoured.
- **Selection destroys status colour.** Rows are rendered plain and then
  `Reverse(true)`, so the one row you are looking at loses its semantic colour.
- **No pinned context.** Scrolling to Checks loses the title, status, and parent.
- **No section navigation.** Long tickets are a single scroll with no outline.
- **Truncation beats information.** Tree titles truncate at the pane edge with
  `…` while the ID always gets full width. There is no right-aligned metadata
  column (checks, age, PR state), so the tree cannot be scanned vertically.
- **Worklog folds the wrong way.** It hides routine progress but repeats
  `… 1 progress entry · w shows all` after every folded run and shows detail
  lines at the same weight as headlines.
- **Review and worklog duplicate the submit record**, so the reviewer reads it
  twice, worded differently.

---

## 3. Ten readability fixes (mostly cheap)

| # | Change | Why |
| --- | --- | --- |
| 1 | **Title primary, ID secondary.** ID faint mono, title normal/bold. | The title is what humans search for; IDs are for agents. |
| 2 | **Words, then glyph.** Render `◐ in progress`, never a bare glyph. Add a density toggle `v`: `minimal / compact / labelled`. | Removes memorisation; keeps the compact column for small screens. |
| 3 | **Humanise time.** `updated 3d ago`, with the absolute date on the status line or on hover. | `2026-10-12 10:40` is slower to parse than "3d ago". |
| 4 | **One fact, one place.** Status strip says `2/4 checks`; review block is the only place that summarises a submission; the checks section holds detail; the worklog links to the review instead of repeating it. | Removes the four-fold duplication. |
| 5 | **Pin the ticket strip.** Keep ID, title, status, age, assignee, parent and checks above the scroll region. | You never lose "what am I reading?" |
| 6 | **Cap reading measure.** Body at 80–88 columns, centred, even in a wide pane. Code/tables may go wider. | Long lines are the biggest readability tax in a terminal. |
| 7 | **Section rhythm.** A rule + label before every section, a two-line gap between sections, and a bottom section chip bar `1 Brief · 2 Where · 3 Checks · 4 Work`. | Creates scannable landmarks. |
| 8 | **Semantic, adaptive palette.** Reserve red for blocked/error, amber for in progress, violet for agent review, magenta for human review, green for done. Use `AdaptiveColor`, preserve colour on selection, honour `NO_COLOR`. | Colour becomes information instead of decoration. |
| 9 | **Selection is a gutter**, not a full reverse-video bar: `▌ T-0066 Epic goals…` with a subtle background. | Keeps status colour and improves contrast. |
| 10 | **Worklog as a rail.** Session rail on the left, headline first, details hanging-indented and dim, one folded chip per run, one-line session summary. | The log reads as a story, not a dump. |

### Worklog: before → after

```text
BEFORE
  ▾ Session 1 · Oct 7 12:43 – Oct 12 10:40 · claude · @c81911f  +186 −16
    12:43  ▶ started  backlog → in progress
            … 1 progress entry · w shows all
    12:43  ⚑ Approval is a flag on done, recorded in the journal
            Agent stays the actor; spoofing … would falsify the audit log
            Restricted to review status so approval means sign-off…
            … 1 progress entry · w shows all
    12:44  ✓ go test ./... passes; e2e in scratch repo
            no approver → refused; in_progress → refused; …
            shp status <id> done still refused for agents
    12:44  ◑ submitted for review
            Agents can close in-review items …  (same text as the review card)

AFTER
  Worklog  1 session · 8 entries · claude            [w all]
  │
  ├─ ● Session 1  Oct 7 → Oct 12 · 4d · claude · c81911f  +186 −16
  │  Plan → 1 decision → verified → submitted
  │
  │   12:43  ▶  started · backlog → in progress
  │   12:43  ⚑  Approval is a flag on `done`, recorded in the journal
  │             Agent stays the actor; spoofing SHERPA_ACTOR would falsify the log.
  │             Restricted to `review` so approval signs off submitted work.
  │   12:44  ✓  go test ./... · e2e in a scratch repo
  │             no approver → refused · in_progress → refused · review + approver → done
  │   12:44  ◑  submitted → see Review
  │   ▸ 3 routine entries
  │
  └─ ○ Planning  Oct 7 · 2 entries
```

The session summary line is generated from the entries (`plan → decisions →
verified → submitted`), so even a folded session tells you its shape.

---

## 4. Ticket anatomy v2

Keep the project's core principle: **human prose in the body, structured state in
frontmatter, updated through commands.** Add two structured things that are
missing — *deliverables* and a *code map* — and change nothing else.

```text
T-0030 · JWT refresh                                  ◐ in progress · 2h · @claude
E-002 › S-004 › T-0030                                         checks 2/4 · PR #35
──────────────────────────────────────────────────────────────────────────────────
Now   Rotating refresh tokens on use; client retry is left.
Why   Sessions die mid-task when the access token expires. Users lose drafts and
      re-login; support sees ~40 tickets/week. Make refresh invisible.
Done when
  ✓ 1. Refresh accepts an expired access token              ← deliverable
       ✓ unit  go test ./internal/auth -run Rotate      abc1234
       ✓ e2e   expired token → 200 + rotated cookie
  ✓ 2. Each refresh revokes the previous token
       ✓ unit  TestRotateRevokesPrevious                 abc1234
  ○ 3. Web client retries once on 401 without a login redirect
       ○ e2e   two expiries, no redirect                  —
       ○ load  1k refreshes/s, p95 < 50ms                target 1000/s
Where  internal/auth/token.go        modify  Rotate, Clock            +64 −12
       api/auth/handlers.go          modify  handleRefresh            +38  −4
       internal/auth/token_test.go   add     TestRotateOnUse         +96
       web/src/session.ts            modify  retryOnce                +18  −9
Context
  Constraints  no schema change · token < 1KB · no new dependencies
  Decisions    rotate on use, not on a timer (12 Oct · ⚑)
  Related      T-0019 done · PR #35 open · design note: docs/auth-refresh.md
Risk / out of scope
  Revocation lags up to 30s. No "sign out everywhere" admin action (T-0061).
Work done
  ⇡ PR #35 open · +216 −25 in 4 files · abc1234..def5678
Checks  evidence last, each with where it came from
  ✓ unit  go test ./internal/auth -run Rotate               abc1234
  ✓ e2e   cmd/e2e/refresh.sh                                 CI #412
  ○ load  1k rps p95 < 50ms                                  not run
```

What changed relative to today:

- **`Done when` is deliverables, not checks.** A deliverable is an outcome you
  can show someone ("client retries once"); a check is how you prove it
  ("two expiries, no redirect"). Checks nest under the deliverable they prove.
- **`Where` is a first-class code map**, planned before the work and reconciled
  with `git diff` afterwards. This is the missing "what additions are being made
  where".
- **`Context` has prompts** (constraints, decisions, related, prior art) so it is
  not an empty optional section. Decisions link to journal entries.
- **`Risk / out of scope`** makes scope a first-class, reviewable thing.
- **`Checks` is visual evidence, last**, and collapses after the first few.

### Frontmatter additions

```yaml
deliverables:
  - text: Refresh accepts an expired access token
    done: true
    evidence: internal/auth/token_test.go:TestRotateOnUse
  - text: Each refresh revokes the previous token
    done: true
  - text: Web client retries once on a 401 without a login redirect
    done: false

code:
  - path: internal/auth/token.go
    change: modify            # add | modify | remove | move
    symbols: [Rotate, Clock]  # optional, best-effort
    note: rotation and revocation
    lines: 40-88              # optional
    deliverable: 1            # links the change to the outcome it serves
    status: planned           # planned | touched | done
  - path: web/src/session.ts
    change: modify
    symbols: [retryOnce]
    deliverable: 3
    status: planned
```

These stay in frontmatter so agents never rewrite human prose, exactly like
`checks` and `measures` today.

### New commands

```bash
shp deliverable add "Refresh accepts an expired access token"     # returns index
shp deliverable done 1 -e internal/auth/token_test.go:TestRotateOnUse
shp deliverable list

shp where add internal/auth/token.go --symbols Rotate,Clock --deliverable 1
shp where plan --stdin            # agent states its intended change map
shp where                         # planned vs touched vs diffstat, with drift
shp where check                   # exit non-zero if touched files are off-plan

shp lint [id]                     # brief completeness and vague-check detection
shp brief [id]                    # one-screen brief (also the agent context payload)
```

`shp where add` can also be inferred: the post-edit hook already records touched
files, and `git diff --numstat` already runs for log entries. Reconcile in
`shp sync` and at submit. The plan is what makes the map *intentional* rather
than a list of accidents.

### Templates by ticket type

One shape does not fit bugs and spikes. Add `--template`:

```bash
shp new ticket "Refresh drops the cookie" -p S-004 --template bug
```

- **feature** — Why / Done when / Where / Context / Risk
- **bug** — Expected / Actual / Repro / Suspected cause / Regression test / Fix
- **spike** — Question / Timebox / What I tried / Findings / Recommendation
- **chore** — Why / Change / Verification / Blast radius

Templates are markdown preambles with `##` sections, so they stay fully editable.
`shp lint` knows the expected sections for each type.

---

## 5. TUI: from one long scroll to a readable page

### Layout

```text
⛰ sherpa · SHP   ◐ 2 in progress  ◒ 1 agent review  ◑ 7 review ✋3  ● 41 done   [n needs you]
┌ Tree ───────────────────────┬ T-0030 · JWT refresh ──────────────────────────────────────┐
│ ▾ E-002 TUI            12/19 │ ◐ in progress · 2h · @claude · S-004 · checks 2/4 · ⇡ #35 │
│   ▾ S-004 Board         3/6  │ ─────────────────────────────────────────────────────────── │
│     ● T-0009 Board      3/3  │ Now   Rotating refresh tokens on use; client retry is left. │
│     ◐ T-0059 Search     2/4  │                                                            │
│   ▸ S-011 Editor        7/9  │ WHY                                                        │
│ ▸ E-003 Agent           8/14 │ Sessions die mid-task when the access token expires…       │
│                              │                                                            │
│                              │ DONE WHEN                                     2/3          │
│                              │ ✓ Refresh accepts an expired access token                   │
│                              │   ✓ unit · go test ./internal/auth -run Rotate   abc1234   │
│                              │ ✓ Each refresh revokes the previous token                   │
│                              │ ○ Web client retries once on a 401               —          │
│                              │                                                            │
│                              │ WHERE                                             4 files    │
│                              │ ▾ internal/auth                                            │
│                              │   ~ token.go        Rotate, Clock          +64 −12   ✓      │
│                              │   + token_test.go   TestRotateOnUse        +96       ✓      │
│                              │ ▾ api/auth                                                 │
│                              │   ~ handlers.go     handleRefresh          +38  −4   ✓      │
│                              │   ~ web/src/session.ts  retryOnce          +18  −9   ○      │
│                              │                                                            │
│                              │ △ 1 file touched outside the plan: web/src/app.ts           │
│                              │                                                            │
│                              │ ── 1 Brief · 2 Where · 3 Checks · 4 Work · 5 Log ─────────   │
└──────────────────────────────┴────────────────────────────────────────────────────────────┘
 j/k move  enter open  tab pane  [ ] section  d diff  o open code  y copy  w worklog  ? help
```

Key decisions:

- **Pinned strip** at the top of the document: title, status, age, assignee,
  parent, checks, PR. The body scrolls under it.
- **Reading width is capped** (~88 cols) and left-aligned inside the pane, so the
  eye does not travel 140 columns. Tables and diagrams may use full width.
- **Section navigation** with `[` / `]` and `1`–`5`; a section chip bar at the
  bottom shows progress and acts as a jump target.
- **`d` opens a diff peek** for the selected item's touched files
  (`git diff --stat`, then per-file hunks), so "what changed" is answerable
  without leaving the TUI.
- **`o` opens the file under the cursor** in `$EDITOR` (or `$SHERPA_OPEN`,
  e.g. `opencode`), at the recorded line if present.
- **`y` copies a ready-to-paste agent prompt** for the ticket: the same compact
  brief described in §7. Useful when you want to hand work to an agent.
- **`v` cycles tree density**: `minimal` (glyph + title), `compact` (+ checks,
  age), `labelled` (+ status word, assignee, PR).
- **`i` toggles an outline rail** on terminals ≥120 columns: sections with
  counts, open deliverables, code-map summary, live agent activity. On narrower
  terminals the rail is hidden and the strip carries the essentials.

### Tree row anatomy

```text
▌ ◐ T-0059  Fuzzy search for tickets…              2/4  ·  2h  ·  ⇡   ← selected
  ● T-0008  Glamour preview pane                    2/2  ·  9d
  ✖ T-0044  Hook adapters for Cursor, Codex…        0/4  ·  3d  ·  ⛔ creds
  ● E-001   Core data model & CLI                  41/52  ████████░░  79%
```

- Title gets the flexible width; metadata is right-aligned and fixed.
- Selection is a gutter + subtle background, preserving status colour.
- Epic/story rows show a rollup bar; tickets show check progress.
- Blocked rows show the *reason* inline when short, otherwise `⛔`.

### Preview micro-typography

- Headings lose the `##` (already done) and gain a rule + spacing.
- Metadata is faint; evidence is code-styled but not full-brightness.
- Empty states are actionable: `_No description yet._` becomes
  `No brief yet — press e to write Why, or y to copy an agent prompt.`
- Human prose and generated sections are labelled: a faint `generated` tag on
  Code map, Work done and Worklog prevents mistaking machine text for intent.

---

## 6. Work management

Readability is also about *what to do next*. Sherpa already has a board, an
inbox and a good review gate. Add the missing triage layer:

- **`shp next`** — the best next ticket: ready, unblocked, highest priority,
  oldest, smallest open check count. Prints one line and why. Optional `--claim`.
- **Aging** — `in progress 6d`, `in review 3d`; amber after 3d in progress,
  red after 7d in review. Stale work becomes visible instead of quietly ageing.
- **WIP awareness** — warn when an actor has >2 tickets in progress; the board
  shows `2/3` per actor. Do not hard-block: warn in `context`, `start` and the
  header.
- **Needs-you inbox with reasons** — sort by risk, not just status: failing
  checks, open manual checks, missing PR, stale review, plan drift. Each row says
  *why* it needs you in one phrase.
- **`shp standup`** — since the last workday: moved, finished, blocked, needs you,
  in flight with age. Generated from the journal; no new data.
- **Agent lanes on the board** — group in-progress tickets by worktree/actor so
  parallel agents are legible:

```text
In progress  2/3
  claude     ◐ T-0059 Fuzzy search          2h   6 files  ✓2/4
  opencode   ◐ T-0066 Epic goals            4h   2 files  ✓1/2
Agent review  1
  codex      ◒ T-0044 Hook adapters         1d   ⇡ #41 open
Needs you  3   ✋ manual checks (2) · failing check (1)
```

- **Cycle-time metrics from the journal** (no new tracking):
  created→start (wait), start→submit (build), submit→done (review latency),
  rework rate (rejects), plan drift. Show per epic and per actor in `shp stats`.

---

## 7. Agent integration: give the agent the same brief

OpenCode's advantage is not just hooks; it is that the model gets *compact,
relevant context* at the right moment. Sherpa can do the same with its own
journal and git.

### 7.1 A compact brief instead of a ticket dump

`shp brief` (and `shp context --brief`) emits ≤400 tokens, stable ordering,
facts only:

```text
TICKET T-0030 · JWT refresh · ◐ in progress · S-004
Outcome: users stay signed in past access-token expiry without a re-login.
Deliverables 2/3
  ✓ Refresh accepts an expired access token
  ✓ Each refresh revokes the previous token
  ○ Web client retries once on a 401 without a redirect
Next open check: #3 e2e — two expiries, no redirect
Where (planned, 1 touched off-plan)
  internal/auth/token.go      modify  Rotate, Clock     +64 −12
  api/auth/handlers.go        modify  handleRefresh     +38  −4
  web/src/session.ts          modify  retryOnce         +18  −9
  web/src/app.ts              touched off-plan
Constraints: no schema change · token < 1KB · no new deps
Decisions: rotate on use, not a timer (12 Oct)
Do not: add dependencies · touch billing/ · change token format
```
This is the payload injected at session start, after compaction, and by
`shp brief <id>`. It answers "what does success look like and where do I work?"
in a fraction of the ticket length.

### 7.2 Plan first, then drift-check

The single highest-value agent feature: **make the agent state its change map
before it edits.**

```bash
shp plan --stdin <<'EOF'
deliverables:
  - Refresh accepts an expired access token
  - Web client retries once on a 401
where:
  - internal/auth/token.go: modify Rotate, Clock
  - api/auth/handlers.go: modify handleRefresh
  - web/src/session.ts: modify retryOnce
checks:
  - unit: go test ./internal/auth -run Rotate
  - e2e: two expiries, no redirect
EOF
```

- The plan renders as `Where` (status `planned`).
- Post-edit hooks and `git diff` mark entries `touched`.
- Submit warns: *"3 files touched outside the plan"*, *"deliverable 2 has no
  check"*, *"planned api/auth/handlers.go not touched"*.
- On submit, the plan is reconciled, and `code` entries get final `+/-` stats.
- The TUI shows a `△ drift` badge; the review brief leads with it.

This turns "what additions are being made where" from a post-hoc diffstat into
an intention the human can review *before* code is written, and that the agent
is held to.

### 7.3 Capture evidence automatically

Hooks already see tool calls and shell commands. Use that:

- When an agent runs a test command, record `command · exit code · summary` as
  candidate evidence (e.g. `go test ./auth → ok 0.42s`).
- `shp check <n>` then links to a real command + commit instead of a self-typed
  string.
- Manual checks stay manual, with `--how` steps and a screenshot/attachment path.
- Submit refuses a check with no evidence unless it is explicitly waived — or
  warns loudly, depending on policy.

### 7.4 OpenCode specifics

Sherpa already installs an OpenCode plugin (`.opencode/plugins/sherpa.ts`). Extend
it rather than inventing a parallel protocol:

1. **Register the MCP server** in `.opencode/config` on `shp init`, and ship a
   bundled `sherpa` MCP server (`shp mcp`, already planned as T-0017).
   Tools: `brief`, `next`, `claim`, `log`, `plan`, `check`, `where`, `submit`,
   `review`, `diff_map`. Resources: `sherpa://ticket/T-0030`,
   `sherpa://board`, `sherpa://brief`.
2. **Inject the brief** on session start and after compaction via the plugin's
   system-prompt hook. No giant ticket body in context.
3. **Typed capture** on `tool.execute.after`: edited file → `touch`; test command
   → candidate evidence; `gh pr create` → PR link (already handled).
4. **Plan enforcement** on `tool.execute.before`: first edit without a plan emits
   the brief and asks for a `shp plan`; never hard-block (fail open, as today).
5. **Session identity**: record the OpenCode session id with the claim, so the
   TUI can link to or tail the transcript and the audit trail ties decisions to
   a conversation.
6. **Live activity, not just a spinner**: the plugin writes
   `.sherpa/.local/live/<session>.json` with `{actor, ticket, action, file,
   since}` on each tool call. The TUI reads it and shows
   `⠹ claude · editing internal/auth/token.go · 12s` and `plan drift`. This is a
   much better answer to "what is the agent doing?" than a generic spinner.
7. **Ship commands**: `.opencode/command/ticket.md`, `review.md`, `standup.md`,
   `brief.md`, so `/ticket` starts work with the protocol and `/review` runs the
   review brief. Equivalent Claude Code skills/commands from `shp init`.

### 7.5 A review brief, not a ticket

`shp review <id> --brief` produces a narrow, reviewer-shaped document:

```text
T-0030 · JWT refresh — review brief
Plan vs actual: 4 planned, 4 touched, 1 off-plan (web/src/app.ts)
Deliverables: 2/3 done · 1 untested
Checks: 2/4 — unit ✓, e2e ✓, load ✗ (not run)
Diff: +216 −25 in 4 files · PR #35 open
Risk: rotate-on-use can break parallel requests; no migration
Ask: is 30s revocation lag acceptable for v1?
Decisions since submit: 1 (⚑ rotate on use)
```

The reviewer agent gets this plus the diff, nothing else. This reduces token use
and focuses review on risk instead of prose.

---

## 8. Quality guardrails

Make clarity cheap to maintain:

- **`shp lint`** (advisory, `--strict` for CI):
  - missing `Why`, fewer than 2 deliverables, deliverable with no check
  - check text that is vague (`works`, `tests pass`, `looks good`) with no
    command/evidence
  - code map empty at submit, or >25% of touched files off-plan
  - stale `Now` (older than the last journal entry), missing non-goals
  - title too long to scan (>70 chars), too many labels
- **Brief completeness chip** in the TUI: `▰▰▰▱▱ 3/5`. Never blocks; makes the
  gap visible.
- **Definition of Ready**: `shp start` warns (does not refuse) when a ticket has
  no deliverables or code map. A one-line nudge beats a wall of prose later.
- **Generated-content labels**: faint `generated` tags on Where/Work done/Worklog
  so nobody mistakes machine output for a human decision.

---

## 9. Roadmap

Ordered by impact per unit of effort. Each phase is independently shippable.

| Phase | Work | Effort | Impact |
| --- | --- | --- | --- |
| **0. Readability pass** | Deduplicate state, title/ID emphasis, humanise dates, status words, adaptive colours, selection gutter, pinned strip, reading width, section rules, worklog rail/chips | 1–2 days | Very high |
| **1. Brief v2** | `deliverables` + `checks.for`, `code` map, `shp deliverable/where/plan`, templates, render | 3–5 days | Very high |
| **2. Navigate & inspect** | Outline, `[ ]`/`1–5`, `d` diff peek, `o` open file, `y` copy prompt, tree metadata + density | 2–3 days | High |
| **3. Agent brief** | `shp brief`, compact context, plan drift warnings, OpenCode/Claude injection, evidence capture, live activity | 4–6 days | Very high |
| **4. Work management** | `shp next`, `shp standup`, aging, WIP warnings, agent lanes, cycle-time stats | 3–5 days | Medium–high |
| **5. Guardrails** | `shp lint`, completeness chip, review brief, CI mode | 2–3 days | Medium |

**If only three things ship:** deduplicate + pin the ticket strip (phase 0), add
deliverables and the code map (phase 1), and ship `shp brief` + plan drift
(phase 3). Those three change the reading experience and the agent handoff more
than everything else combined.

---

## 10. Success measures

A simple usability test on the five questions, run against five real tickets:

- Can I state the status and whether it needs me in **5 seconds**?
- Can I state what "done" means in **15 seconds**?
- Can I name the files that will change in **20 seconds**?
- Can I find the evidence for each check in **30 seconds**?
- Can I explain the last decision and who made it in **30 seconds**?

Track over time: median time-to-review, review latency, reject rate, percentage
of tickets with a code map at submit, percentage of checks with evidence, plan
drift, and the number of "what is this ticket?" questions in review.

---

## 11. Trade-offs and non-goals

- **Do not turn Sherpa into Jira.** Every new field must earn its place by
  improving a decision. Keep tickets to roughly one screen; link out for depth.
- **Do not require the new fields.** Existing repos and tickets must keep
  working; `shp lint` warns, never breaks. Migrate additively.
- **Do not let agents write prose.** Deliverables and code maps are structured
  and command-updated; `Why`, `Context` and `Risk` stay human-authored. Agents
  can *propose* plan entries, but the brief's voice stays human.
- **Do not duplicate the journal.** Review blocks, checks and worklog must read
  from the same events and render each fact once.
- **Performance:** cache `git diff` and PR state by HEAD sha; never shell out on
  every render tick. The 2-second reload is fine for files, not for `gh`.

---

## 12. Where the changes land

| Change | Primary files |
| --- | --- |
| Ticket strip, section rules, reading width | `internal/tui/tui.go`, `internal/tui/editor.go` |
| Tree metadata, density, selection gutter | `internal/tui/tree.go` |
| Status strip, adaptive palette | `internal/render/render.go`, `internal/tui/tui.go` |
| Deduplicate review / checks | `internal/render/sections.go`, `internal/worklog/worklog.go` |
| Worklog rail and folding | `internal/worklog/worklog.go` |
| Deliverables, code map model | `internal/model/model.go`, `internal/model/sections.go` |
| Commands (`deliverable`, `where`, `plan`, `brief`, `lint`, `next`) | `internal/cli/sections.go`, `internal/cli/commands.go` |
| Plan drift, hooks, live activity | `internal/hooks/*`, `internal/cli/hook.go` |
| MCP server | new `internal/mcp` |
| OpenCode commands / injection | `internal/hooks/install.go`, `cmd/shp` docs |

---

## Appendix A — Ticket template (feature)

```markdown
## Why

_What hurts today, for whom, and what changes if we ship this?_

## Context

- Constraints:
- Prior art / links:
- Related tickets:

## Approach

_One or two paragraphs, not a spec. Link to a design note for depth._

## Out of scope

- …

## Risks

- …
```

Generated around it: **Now**, **Deliverables** (+ checks), **Code map**,
**Measurables**, **Work done**, **Worklog**. The human never has to keep those
in sync by hand, and no two sections tell the same story.
