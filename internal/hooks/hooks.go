// Package hooks is Sherpa's agent-neutral hook logic. Each coding agent
// (Claude Code, Cursor, Codex, OpenCode) calls `shp hook <event>` from its
// own hook system; an adapter translates that agent's payload into an Input
// and the Output back into what the agent understands.
//
//	session-start ──▶ Context: what's active, in flight and ready
//	pre-edit      ──▶ Block unless a ticket is claimed in this checkout
//	post-edit     ──▶ record the touched file on the active ticket
//	stop          ──▶ Block once if files changed since the last log entry
//	working       ──▶ heartbeat only (prompt submitted, any tool call)
//
// Every event but session-start also refreshes the checkout's live
// heartbeat, which the TUI animates; a stop that lets the agent end clears it.
//
// Hooks fail open: outside a Sherpa project, or on any internal error, the
// agent is never blocked.
package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/links"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

type Event string

const (
	SessionStart Event = "session-start"
	PreEdit      Event = "pre-edit"
	PostEdit     Event = "post-edit"
	Stop         Event = "stop"
	PostShell    Event = "post-shell" // links PRs the agent opens with gh
	Working      Event = "working"    // heartbeat for the TUI's live indicator
)

var Events = []Event{SessionStart, PreEdit, PostEdit, Stop, PostShell, Working}

type Input struct {
	Agent      string // claude, cursor, codex, opencode
	Event      Event
	Dir        string   // working directory of the agent
	Files      []string // files being edited (absolute or relative to Dir)
	StopActive bool     // the agent is already continuing because of a stop hook
	Command    string   // post-shell: the command the agent ran
	Output     string   // post-shell: its output (or the raw payload)
}

type Output struct {
	Block   bool   // deny the edit / keep the agent going
	Reason  string // shown to the agent when blocking
	Context string // injected at session start
}

// Handle decides what to do. It never returns an error: problems fail open.
func Handle(in Input) Output {
	s, err := store.Find(cmpDir(in.Dir))
	if err != nil {
		return Output{}
	}
	var out Output
	switch in.Event {
	case SessionStart:
		return Output{Context: sessionContext(s)}
	case PreEdit:
		out = preEdit(s, in)
	case PostEdit:
		postEdit(s, in)
	case Stop:
		out = stop(s, in)
	case PostShell:
		postShell(s, in)
	}
	if in.Event == Stop && !out.Block {
		_ = live.Clear(s)
	} else {
		id := ""
		if it := activeTicket(s); it != nil {
			id = it.ID
		}
		_ = live.Touch(s, cmpOr(in.Agent, "agent"), id)
	}
	return out
}

func cmpDir(d string) string {
	if d == "" {
		d, _ = os.Getwd()
	}
	return d
}

// activeTicket returns the ticket claimed in this checkout if it is still in progress.
func activeTicket(s *store.Store) *model.Item {
	id, err := s.Active()
	if err != nil || id == "" {
		return nil
	}
	it, err := s.Get(id)
	if err != nil || it.Status != model.InProgress {
		return nil
	}
	return it
}

func sessionContext(s *store.Store) string {
	items, err := s.All()
	if err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sherpa tracks work in this repo (%s, key %s).\n", s.Config.Name, s.Config.Key)
	if it := activeTicket(s); it != nil {
		fmt.Fprintf(&b, "Active ticket in this checkout: %s %s. Keep working on it, logging as you go.\n", it.ID, it.Title)
		if d, t := it.DeliverableCounts(); t > 0 {
			fmt.Fprintf(&b, "  deliverables %d/%d:\n", d, t)
			for i, x := range it.Deliverables {
				mark := "○"
				if x.Done {
					mark = "✓"
				}
				fmt.Fprintf(&b, "    %s %d. %s\n", mark, i+1, x.Text)
			}
		}
		for i, c := range it.Checks {
			if !c.Resolved() {
				link := ""
				if c.For > 0 {
					link = fmt.Sprintf(" (deliverable %d)", c.For)
				}
				fmt.Fprintf(&b, "  open check %d: %s%s\n", i+1, c.Text, link)
			}
		}
		if len(it.Code) > 0 {
			fmt.Fprintf(&b, "  where:\n")
			for i, c := range it.Code {
				if i == 6 {
					fmt.Fprintf(&b, "    … %d more\n", len(it.Code)-6)
					break
				}
				line := "    " + c.Path
				if len(c.Symbols) > 0 {
					line += " (" + strings.Join(c.Symbols, ", ") + ")"
				}
				fmt.Fprintln(&b, line)
			}
		}
		fmt.Fprintf(&b, "  brief: `shp brief %s` (outcome, deliverables, next check, code map)\n", it.ID)
	} else {
		b.WriteString("No ticket is claimed in this checkout. Claim one with `shp start <T-id>` before editing files (edits are blocked until you do).\n")
	}
	list := func(title string, st model.Status, limit int) {
		var lines []string
		for _, it := range items {
			if it.Kind == model.Ticket && it.Status == st {
				lines = append(lines, fmt.Sprintf("  %s %s", it.ID, it.Title))
			}
		}
		if len(lines) == 0 {
			return
		}
		more := ""
		if len(lines) > limit {
			more = fmt.Sprintf("  … and %d more (`shp ls -s %s`)\n", len(lines)-limit, st)
			lines = lines[:limit]
		}
		fmt.Fprintf(&b, "%s:\n%s\n%s", title, strings.Join(lines, "\n"), more)
	}
	list("In progress", model.InProgress, 8)
	list("Ready to start", model.Ready, 8)
	list("Awaiting agent review (if asked to review: shp review <id> --pass|--reject -m \"notes\")", model.AgentReview, 8)
	b.WriteString(`Protocol: shp brief <T-id> → shp start <T-id> → shp log "headline" -f <file> (-k decision|verify|blocker, -d details) → ` +
		`shp deliverable add "outcome", shp where add <file> --symbols <Type,Func> --deliverable N, shp check add --for N "…" → ` +
		`shp now "one-line state", shp check <n> -e <evidence>, shp measure "<name>" <value> --target <goal> → ` +
		`shp lint <T-id> → open the PR with gh pr create (Sherpa links it) → shp submit -m "what changed" --verified "how it was checked". ` +
		`Close a ticket with shp done <T-id> when every deliverable and check is resolved; only a human closes a story or epic. ` +
		`Never edit .sherpa/ by hand.`)
	return b.String()
}

// relative reports f relative to the checkout, and whether it lies inside it.
func relative(s *store.Store, dir, f string) (string, bool) {
	if !filepath.IsAbs(f) {
		f = filepath.Join(dir, f)
	}
	for _, root := range []string{s.CodeRoot, s.Root} {
		if rel, err := filepath.Rel(root, f); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), true
		}
	}
	return f, false
}

func preEdit(s *store.Store, in Input) Output {
	var inside []string
	for _, f := range in.Files {
		rel, ok := relative(s, cmpDir(in.Dir), f)
		if !ok {
			continue // scratch files, home config, other repos: not ours to police
		}
		if rel == store.DirName || strings.HasPrefix(rel, store.DirName+"/") {
			return Output{Block: true, Reason: "Sherpa: don't edit .sherpa/ by hand. Use shp (start, log, submit, `shp edit <id> --stdin` for descriptions)."}
		}
		inside = append(inside, rel)
	}
	if len(inside) == 0 && len(in.Files) > 0 {
		return Output{}
	}
	if activeTicket(s) != nil {
		return Output{}
	}
	what := "files in this repo"
	if len(inside) > 0 {
		what = strings.Join(inside, ", ")
	}
	return Output{Block: true, Reason: fmt.Sprintf(
		"Sherpa: no ticket is claimed in this checkout, so editing %s is blocked. "+
			"Run `shp context` to see ready work, then `shp start <T-id>`. "+
			"If nothing fits, create one: `shp new ticket \"…\" -p <S-id>`, then start it.", what)}
}

func postEdit(s *store.Store, in Input) {
	it := activeTicket(s)
	if it == nil {
		return
	}
	var files []string
	for _, f := range in.Files {
		if rel, ok := relative(s, cmpDir(in.Dir), f); ok && !strings.HasPrefix(rel, store.DirName) {
			files = append(files, rel)
		}
	}
	if len(files) == 0 {
		return
	}
	actor := "agent:" + cmpOr(in.Agent, "unknown")
	session := journal.Session(actor)
	if s.CodeRoot != s.Root && os.Getenv("SHERPA_SESSION") == "" {
		session += "-" + filepath.Base(s.CodeRoot)
	}
	_ = journal.Append(s.Dir, journal.Event{Actor: actor, Session: session, Item: it.ID, Type: "touch", Files: files})
}

func stop(s *store.Store, in Input) Output {
	if in.StopActive {
		return Output{} // already nudged once this turn; never loop
	}
	it := activeTicket(s)
	if it == nil {
		return Output{}
	}
	events, err := journal.ReadAll(s.Dir)
	if err != nil {
		return Output{}
	}
	var lastTouch, lastNote time.Time
	for _, e := range journal.ForItem(events, it.ID) {
		switch e.Type {
		case "touch":
			lastTouch = e.TS
		case "log", "submit", "claim", "status":
			lastNote = e.TS
		}
	}
	if lastTouch.IsZero() || !lastTouch.After(lastNote) {
		return Output{}
	}
	// Nudge at most once per batch of edits, whatever the agent reports
	// about its own stop loop: if it stops again without logging, let it.
	nudge := filepath.Join(s.Dir, ".local", "stop-nudge", s.CheckoutKey())
	stamp := lastTouch.UTC().Format(time.RFC3339)
	if prev, err := os.ReadFile(nudge); err == nil && strings.TrimSpace(string(prev)) == stamp {
		return Output{}
	}
	if err := os.MkdirAll(filepath.Dir(nudge), 0o755); err == nil {
		_ = os.WriteFile(nudge, []byte(stamp+"\n"), 0o644)
	}
	return Output{Block: true, Reason: fmt.Sprintf(
		"Sherpa: you changed files on %s since its last worklog entry. Before stopping, record where things stand: "+
			"if the work is finished, `shp submit -m \"what changed\" --verified \"how it was checked\"`; "+
			"otherwise `shp log \"what you did and what's next\"` (add `-k blocker` if you're stuck).", it.ID)}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// postShell links the pull request an agent opens (or views) with gh to the
// active ticket. Agents create PRs; Sherpa only records them.
func postShell(s *store.Store, in Input) {
	if !strings.Contains(in.Command, "gh pr") && !strings.Contains(in.Command, "gh api") {
		return
	}
	url, n := links.FromText(in.Output)
	if url == "" {
		return
	}
	it := activeTicket(s)
	if it == nil {
		return
	}
	if it.Links != nil && it.Links.PR == url {
		return
	}
	if it.Links == nil {
		it.Links = &model.Links{}
	}
	it.Links.PR, it.Links.PRNumber = url, n
	if it.Links.PRState == "" {
		it.Links.PRState = "open"
	}
	if br := gitinfo.Branch(s.CodeRoot); br != "" {
		it.Links.Branch = br
	}
	if err := s.Save(it); err != nil {
		return
	}
	actor := "agent:" + cmpOr(in.Agent, "unknown")
	_ = journal.Append(s.Dir, journal.Event{Actor: actor, Session: journal.Session(actor), Item: it.ID, Type: "link",
		Message: fmt.Sprintf("linked PR #%d", n), Detail: []string{url}})
}
