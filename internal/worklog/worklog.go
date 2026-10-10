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
	faint    = lipgloss.NewStyle().Faint(true)
	title    = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("39"))
	sessHead = lipgloss.NewStyle().Bold(true)
	plus     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	minus    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	openTag  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	colors   = map[string]lipgloss.Style{
		"cyan":    lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		"green":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"magenta": lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		"red":     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		"yellow":  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"blue":    lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"faint":   faint,
		"":        lipgloss.NewStyle(),
	}
)

// Describe returns an event's icon, colour name and headline, plus any detail
// lines that belong under it.
func Describe(e journal.Event) (icon, color, headline string, detail []string) {
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
		if e.Message != "" {
			detail = append([]string{e.Message}, detail...)
		}
		if e.Verified != "" {
			detail = append(detail, "✓ verified: "+e.Verified)
		}
		for _, f := range e.Followups {
			detail = append(detail, "→ follow-up: "+f)
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
		return "✔", "green", "agent review passed by " + who + " · ready for human review", detail
	case "status":
		if e.Message != "" {
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
			return "●", "green", "approved · done", detail
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

const indent = 10 // "  15:04  " + icon column

// Render draws the timeline: newest session first and expanded, older ones
// collapsed to one line unless expandAll.
func Render(sessions []Session, width int, expandAll bool) []string {
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
	textW := max(20, width-indent-4)
	for i := len(sessions) - 1; i >= 0; i-- {
		s := sessions[i]
		expanded := expandAll || i == len(sessions)-1
		out = append(out, sessionHeader(s, expanded))
		if !expanded {
			continue
		}
		folded := 0
		flush := func() {
			if folded > 0 {
				out = append(out, strings.Repeat(" ", indent+2)+faint.Render(fmt.Sprintf("… %d progress %s · w shows all", folded, map[bool]string{true: "entry", false: "entries"}[folded == 1])))
				folded = 0
			}
		}
		highlights := !expandAll && len(s.Entries) > 6
		for _, e := range s.Entries {
			if highlights && routine(e) {
				folded++
				continue
			}
			flush()
			icon, color, headline, detail := Describe(e)
			who := ""
			if actorName(e.Actor) != actorName(s.Actor) {
				who = faint.Render(" · " + actorName(e.Actor))
			}
			pad := "    " + faint.Render(e.TS.Local().Format("15:04")) + "  " + colors[color].Render(icon) + " "
			for k, l := range wrap(headline+who, textW) {
				if k == 0 {
					out = append(out, pad+l)
				} else {
					out = append(out, strings.Repeat(" ", indent+2)+l)
				}
			}
			for _, d := range detail {
				for _, l := range wrap(d, textW) {
					out = append(out, strings.Repeat(" ", indent+2)+faint.Render(l))
				}
			}
			if len(e.Files) > 0 && e.Type != "submit" { // submit totals are on the session line
				line := faint.Render(fileList(e.Files))
				if st := stats(e.Added, e.Removed); st != "" {
					line += "  " + st
				}
				out = append(out, strings.Repeat(" ", indent+2)+line)
			}
		}
		flush()
		if len(s.Touched) > 0 {
			line := "    " + faint.Render("files touched: ")
			for k, l := range wrap(strings.Join(s.Touched, " · "), textW) {
				if k == 0 {
					out = append(out, line+faint.Render(l))
				} else {
					out = append(out, strings.Repeat(" ", 19)+faint.Render(l))
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
	line := "  " + arrow + " " + strings.Join(parts, faint.Render(" · "))
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

// routine entries fold away in highlight view: plain progress notes and edits.
// Decisions, verifications, blockers, checks, links, status changes and
// submits always show.
func routine(e journal.Event) bool {
	switch e.Type {
	case "log":
		return e.Kind == "" || e.Kind == journal.KindProgress
	case "edit", "create":
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
