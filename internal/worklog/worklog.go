// Package worklog turns an item's journal events into a readable timeline,
// grouped into work sessions: one session per claim (`shp start`), ending at
// submit or done. Events before the first claim form a "Planning" group.
package worklog

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
)

type Session struct {
	N              int // 1-based; 0 is the planning group before the first claim
	Actor          string
	Start, End     time.Time
	Base, Head     string // commit the session started from / last seen
	Added, Removed int    // totals recorded at submit
	Entries        []journal.Event
	Touched        []string // files recorded by agent edit hooks, in first-touch order
	Open           bool     // claimed and not yet submitted or done
}

// Build groups one item's events (oldest first) into sessions.
func Build(events []journal.Event) []Session {
	var out []Session
	cur := Session{}
	n := 0
	flush := func() {
		if len(cur.Entries) > 0 {
			out = append(out, cur)
		}
	}
	for _, e := range events {
		// Older versions of `shp start` wrote a status event next to the claim.
		if e.Type == "status" && e.To == "in_progress" && e.Message == "claimed" {
			continue
		}
		if e.Type == "migrate" { // data-format upgrades are not work
			continue
		}
		if e.Type == "touch" { // edit hooks: summarised per session, not listed
			for _, f := range e.Files {
				if !slices.Contains(cur.Touched, f) {
					cur.Touched = append(cur.Touched, f)
				}
			}
			continue
		}
		if e.Type == "claim" {
			flush()
			n++
			cur = Session{N: n, Actor: e.Actor, Start: e.TS, Base: e.Commit, Head: e.Commit, Open: true}
		}
		if len(cur.Entries) == 0 && cur.Start.IsZero() {
			cur.Start = e.TS
			cur.Actor = e.Actor
		}
		cur.Entries = append(cur.Entries, e)
		cur.End = e.TS
		if e.Commit != "" {
			cur.Head = e.Commit
		}
		if e.Type == "submit" || e.Type == "status" && (e.To == "review" || e.To == "agent_review" || e.To == "done") {
			cur.Open = false
			if e.Type == "submit" {
				cur.Added, cur.Removed = e.Added, e.Removed
			}
		}
	}
	flush()
	return out
}

// Rendering ---------------------------------------------------------------------

var (
	faint    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	title    = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"})
	sessHead = lipgloss.NewStyle().Bold(true)
	plus     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"})
	minus    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"})
	openTag  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "214"})
	colors   = map[string]lipgloss.Style{
		"cyan":    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "45"}),
		"green":   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"}),
		"magenta": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "162", Dark: "205"}),
		"red":     lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"}),
		"yellow":  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "214"}),
		"blue":    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"}),
		"faint":   faint,
		"":        lipgloss.NewStyle(),
	}
)

// Describe returns an event's icon, colour name and headline, plus any detail
// lines that belong under it.
func Describe(e journal.Event) (icon, color, headline string, detail []string) {
	return describe(e, false)
}

// describe is Describe with the option to omit a submission's message,
// verification and follow-ups, because a review card already shows them.
func describe(e journal.Event, hideSubmit bool) (icon, color, headline string, detail []string) {
	detail = append(detail, e.Detail...)
	switch e.Type {
	case "create":
		return "✚", "faint", "created", nil
	case "claim":
		h := "started"
		if e.From != "" && e.From != "in_progress" {
			h += faint.Render("  " + strings.ReplaceAll(e.From, "_", " ") + " → in progress")
		}
		return "▶", "cyan", h, detail
	case "edit":
		return "✐", "faint", "edited description", nil
	case "design":
		return "✎", "cyan", "updated the Design", detail
	case "deliverable":
		return "◇", "green", e.Message, detail
	case "where":
		return "⇢", "blue", e.Message, detail
	case "check":
		return "☑", "green", e.Message, detail
	case "measure":
		return "▤", "cyan", e.Message, detail
	case "link":
		return "⇡", "blue", e.Message, detail
	case "touch":
		return "◦", "faint", "edited " + strings.Join(e.Files, ", "), nil
	case "submit":
		headline = "submitted for review"
		if e.To == "agent_review" {
			headline = "submitted for agent review"
		}
		if !hideSubmit {
			if e.Message != "" {
				detail = append([]string{e.Message}, detail...)
			}
			if e.Verified != "" {
				detail = append(detail, "✓ verified: "+e.Verified)
			}
			for _, f := range e.Followups {
				detail = append(detail, "→ follow-up: "+f)
			}
		}
		if e.To == "agent_review" {
			return "◒", "cyan", headline, detail
		}
		return "◑", "magenta", headline, detail
	case "review": // a verdict on work in agent review
		if e.Message != "" {
			detail = append([]string{e.Message}, detail...)
		}
		who := e.Actor
		if _, name, ok := strings.Cut(who, ":"); ok {
			who = name
		}
		if e.Kind == "reject" {
			return "↩", "red", "changes requested by " + who + " · back in progress", detail
		}
		if e.To == "done" {
			return "●", "green", "agent review passed by " + who + " · closed by " + e.ApprovedBy, detail
		}
		if e.Kind == "spawn" {
			return "⟳", "cyan", "independent reviewer launched by " + who, detail
		}
		return "✔", "green", "agent review passed by " + who + " · ready for human review", detail
	case "status":
		if e.Message != "" && !(hideSubmit && (e.To == "review" || e.To == "agent_review")) {
			detail = append([]string{e.Message}, detail...)
		}
		switch e.To {
		case "agent_review":
			return "◒", "cyan", "moved to agent review", detail
		case "review":
			return "◑", "magenta", "submitted for review", detail
		case "done":
			if e.ApprovedBy != "" {
				return "●", "green", "approved by " + e.ApprovedBy + " · done", detail
			}
			return "●", "green", "closed · done", detail
		case "blocked":
			return "⛔", "red", "blocked", detail
		case "ready":
			return "◎", "blue", "marked ready", detail
		case "backlog":
			return "○", "faint", "moved to backlog", detail
		case "in_progress":
			return "◐", "yellow", "back in progress", detail
		}
		return "→", "", e.From + " → " + e.To, detail
	case "log":
		switch e.Kind {
		case journal.KindDecision:
			return "⚑", "magenta", e.Message, detail
		case journal.KindVerify:
			return "✓", "green", e.Message, detail
		case journal.KindBlocker:
			return "⛔", "red", e.Message, detail
		}
		return "✎", "", e.Message, detail
	}
	return "·", "faint", e.Type + " " + e.Message, detail
}

func actorName(a string) string {
	if _, name, ok := strings.Cut(a, ":"); ok {
		return name
	}
	return a
}

func stats(added, removed int) string {
	if added == 0 && removed == 0 {
		return ""
	}
	return plus.Render(fmt.Sprintf("+%d", added)) + " " + minus.Render(fmt.Sprintf("−%d", removed))
}

// fileList shortens paths to base names unless two files share one.
func fileList(files []string) string {
	seen := map[string]int{}
	for _, f := range files {
		seen[filepath.Base(f)]++
	}
	var names []string
	for _, f := range files {
		if seen[filepath.Base(f)] > 1 {
			names = append(names, f)
		} else {
			names = append(names, filepath.Base(f))
		}
	}
	if len(names) > 4 {
		names = append(names[:3], fmt.Sprintf("+%d more", len(names)-3))
	}
	return strings.Join(names, " · ")
}

// Options controls the worklog layout. HideSubmitDetail is set when a review
// card already shows the submission, so the log can link to it instead of
// repeating the same message, verification and follow-ups.
type Options struct {
	ExpandAll        bool
	HideSubmitDetail bool
}

// Render draws the timeline with every session, expanding only the newest.
// It is the old, simple entry point; use RenderOptions for the richer view.
func Render(sessions []Session, width int, expandAll bool) []string {
	return RenderOptions(sessions, width, Options{ExpandAll: expandAll})
}

// RenderOptions draws the timeline: newest session first and expanded, older
// ones collapsed to one line unless ExpandAll. Sessions use a left rail, a
// one-line summary, and one folded chip per run of routine entries.
func RenderOptions(sessions []Session, width int, opts Options) []string {
	entries := 0
	for _, s := range sessions {
		entries += len(s.Entries)
	}
	if entries == 0 {
		return nil
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, one)
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	work := 0
	for _, s := range sessions {
		if s.N > 0 {
			work++
		}
	}
	summary := plural(work, "session", "sessions")
	if work == 0 {
		summary = "not started"
	}
	out := []string{
		"  " + title.Render("Worklog") + faint.Render("  "+summary+" · "+plural(entries, "entry", "entries")),
		"",
	}
	const entryIndent = 17 // rail + time + icon columns
	textW := max(20, width-entryIndent-4)
	for i := len(sessions) - 1; i >= 0; i-- {
		s := sessions[i]
		expanded := opts.ExpandAll || i == len(sessions)-1
		last := i == 0
		branch, cont := "  ├─ ", "  │  "
		if last {
			branch, cont = "  └─ ", "     "
		}
		out = append(out, branch+sessionHeader(s, expanded))
		if !expanded {
			continue
		}
		if sum := sessionSummary(s); sum != "" {
			out = append(out, cont+"   "+faint.Render(sum))
		}
		out = append(out, cont)
		folded := 0
		flush := func() {
			if folded > 0 {
				out = append(out, cont+"   "+faint.Render(fmt.Sprintf("… %d routine %s", folded, map[bool]string{true: "entry", false: "entries"}[folded == 1])))
				folded = 0
			}
		}
		detailPad := cont + strings.Repeat(" ", max(0, entryIndent-ansi.StringWidth(cont)))
		highlights := !opts.ExpandAll && len(s.Entries) > 6
		for _, e := range s.Entries {
			if highlights && routine(e) {
				folded++
				continue
			}
			icon, color, headline, detail := describe(e, opts.HideSubmitDetail)
			who := ""
			if actorName(e.Actor) != actorName(s.Actor) {
				who = faint.Render(" · " + actorName(e.Actor))
			}
			pad := cont + "   " + faint.Render(e.TS.Local().Format("15:04")) + "  " + colors[color].Render(icon) + " "
			for k, l := range wrap(headline+who, textW) {
				if k == 0 {
					out = append(out, pad+l)
				} else {
					out = append(out, detailPad+l)
				}
			}
			for _, d := range detail {
				for _, l := range wrap(d, textW) {
					out = append(out, detailPad+faint.Render(l))
				}
			}
			if len(e.Files) > 0 && e.Type != "submit" { // submit totals are on the session line
				line := faint.Render(fileList(e.Files))
				if st := stats(e.Added, e.Removed); st != "" {
					line += "  " + st
				}
				out = append(out, detailPad+line)
			}
		}
		flush()
		if len(s.Touched) > 0 {
			line := cont + "   " + faint.Render("files touched: ")
			for k, l := range wrap(strings.Join(s.Touched, " · "), textW) {
				if k == 0 {
					out = append(out, line+faint.Render(l))
				} else {
					out = append(out, detailPad+faint.Render(l))
				}
			}
		}
		out = append(out, "")
	}
	return out
}

func sessionHeader(s Session, expanded bool) string {
	arrow := "▸"
	if expanded {
		arrow = "▾"
	}
	name := fmt.Sprintf("Session %d", s.N)
	if s.N == 0 {
		name = "Planning"
	}
	parts := []string{sessHead.Render(name), when(s.Start, s.End), actorName(s.Actor)}
	switch {
	case s.Base != "" && s.Head != "" && s.Base != s.Head:
		parts = append(parts, s.Base+".."+s.Head)
	case s.Head != "":
		parts = append(parts, "@"+s.Head)
	}
	line := arrow + " " + strings.Join(parts, faint.Render(" · "))
	if st := stats(s.Added, s.Removed); st != "" {
		line += "  " + st
	}
	if s.Open {
		line += "  " + openTag.Render("in progress")
	}
	if !expanded {
		n := fmt.Sprintf("  %d entries", len(s.Entries))
		if len(s.Entries) == 1 {
			n = "  1 entry"
		}
		line += faint.Render(n)
	}
	return line
}

// sessionSummary is the one-line shape of a session: plan → decisions →
// verification → submit → close. It keeps collapsed logs informative.
func sessionSummary(s Session) string {
	var parts []string
	if s.N == 0 {
		parts = append(parts, "planning")
	}
	dec, ver, blk := 0, 0, 0
	submitted, closed := false, false
	for _, e := range s.Entries {
		switch e.Type {
		case "log":
			switch e.Kind {
			case journal.KindDecision:
				dec++
			case journal.KindVerify:
				ver++
			case journal.KindBlocker:
				blk++
			}
		case "submit":
			submitted = true
		case "status":
			if e.To == "done" {
				closed = true
			}
		}
	}
	if dec > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", dec, map[bool]string{true: "decision", false: "decisions"}[dec == 1]))
	}
	if ver > 0 {
		parts = append(parts, "verified")
	}
	if blk > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", blk, map[bool]string{true: "blocker", false: "blockers"}[blk == 1]))
	}
	if submitted {
		parts = append(parts, "submitted")
	}
	if closed {
		parts = append(parts, "closed")
	}
	return strings.Join(parts, " → ")
}

func when(start, end time.Time) string {
	start, end = start.Local(), end.Local()
	switch {
	case start.IsZero():
		return ""
	case end.Sub(start) < time.Minute:
		return start.Format("Jan 2 15:04")
	case start.YearDay() == end.YearDay() && start.Year() == end.Year():
		return start.Format("Jan 2 15:04") + "–" + end.Format("15:04")
	}
	return start.Format("Jan 2 15:04") + " – " + end.Format("Jan 2 15:04")
}

func wrap(s string, w int) []string {
	return strings.Split(ansi.Wordwrap(s, w, ""), "\n")
}

// routine entries fold away in highlight view: plain progress notes, edits,
// and structural code-map updates (the map itself is rendered above the log).
// Decisions, verifications, blockers, checks, links, deliverables, status
// changes and submits always show.
func routine(e journal.Event) bool {
	switch e.Type {
	case "log":
		return e.Kind == "" || e.Kind == journal.KindProgress
	case "edit", "create", "where":
		return true
	}
	return false
}

// LastReview returns the most recent submit (or move to review) of an item.
func LastReview(events []journal.Event) (journal.Event, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type == "submit" || (e.Type == "status" && e.To == "review") {
			return e, true
		}
	}
	return journal.Event{}, false
}
