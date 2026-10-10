package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

// Bar draws a 10-cell progress bar.
func Bar(done, total int) string {
	if total == 0 {
		return strings.Repeat("░", 10)
	}
	n := done * 10 / total
	return strings.Repeat("█", n) + strings.Repeat("░", 10-n)
}

func children(all []*model.Item, id string) []*model.Item {
	var out []*model.Item
	for _, it := range all {
		if it.Parent == id {
			out = append(out, it)
		}
	}
	return out
}

func short(s string, n int) string { return ansi.Truncate(s, n, "…") }

// Rollup counts done tickets and resolved checks under an item.
func Rollup(it *model.Item, all []*model.Item) (doneTickets, tickets, resolved, checks int) {
	var walk func(id string)
	walk = func(id string) {
		for _, c := range children(all, id) {
			if c.Kind == model.Ticket {
				tickets++
				if c.Status == model.Done {
					doneTickets++
				}
				r, t := c.CheckCounts()
				resolved += r
				checks += t
			}
			walk(c.ID)
		}
	}
	walk(it.ID)
	return
}

// AutoVisual is the generated diagram for a story or epic: an epic's stories
// as a pipeline with progress, a story's tickets with their dependencies.
func AutoVisual(it *model.Item, all []*model.Item) string {
	kids := children(all, it.ID)
	if len(kids) == 0 {
		return "(no children yet)"
	}
	var b strings.Builder
	switch it.Kind {
	case model.Epic:
		for i, s := range kids {
			done, total, _, _ := Rollup(s, all)
			fmt.Fprintf(&b, "%s %s %-40s %s %d/%d\n", s.Status.Glyph(), s.ID, short(s.Title, 40), Bar(done, total), done, total)
			if i < len(kids)-1 {
				b.WriteString("│\n")
			}
		}
	default:
		for i, t := range kids {
			branch := "├─"
			if i == len(kids)-1 {
				branch = "└─"
			}
			line := fmt.Sprintf("%s %s %s %s", branch, t.Status.Glyph(), t.ID, short(t.Title, 44))
			if len(t.Depends) > 0 {
				line += "  ◀ " + strings.Join(t.Depends, ", ")
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Badges is the one-line summary under a title: PR, checks, size, rollups.
func Badges(it *model.Item, all []*model.Item) string {
	var parts []string
	if l := it.Links; !l.Empty() {
		if l.PRNumber > 0 {
			pr := fmt.Sprintf("⇡ PR #%d", l.PRNumber)
			if l.PRState != "" {
				pr += " " + l.PRState
			}
			parts = append(parts, pr)
		}
		if l.Added+l.Removed > 0 {
			parts = append(parts, fmt.Sprintf("+%d −%d", l.Added, l.Removed))
		}
	}
	if r, t := it.CheckCounts(); t > 0 {
		mark := ""
		if r == t {
			mark = " ✓"
		}
		parts = append(parts, fmt.Sprintf("checks %d/%d%s", r, t, mark))
	}
	if len(it.Depends) > 0 {
		var deps []string
		for _, d := range it.Depends {
			glyph := "?"
			for _, x := range all {
				if x.ID == d {
					glyph = x.Status.Glyph()
				}
			}
			deps = append(deps, d+" "+glyph)
		}
		parts = append(parts, "needs "+strings.Join(deps, ", "))
	}
	if it.Kind != model.Ticket {
		done, total, r, t := Rollup(it, all)
		if total > 0 {
			parts = append(parts, fmt.Sprintf("tickets %d/%d %s", done, total, Bar(done, total)))
		}
		if t > 0 {
			parts = append(parts, fmt.Sprintf("checks %d/%d", r, t))
		}
	}
	return strings.Join(parts, " · ")
}

// cell escapes text for a markdown table cell.
func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }

// Sections renders the generated sections that follow an item's body:
// Visual (generated, stories and epics without their own), Measurables,
// Work done and Checks. Children go between Work done and Checks.
func Sections(it *model.Item, all []*model.Item, kids []*model.Item) string {
	var b strings.Builder
	autoVisual := false
	if it.Kind != model.Ticket {
		if _, ok := model.Section(it.Body, "Visual"); !ok {
			fmt.Fprintf(&b, "\n\n## Visual\n\n```text\n%s\n```\n", AutoVisual(it, all))
			autoVisual = true // it lists every child, so the Children list would repeat it
		}
	}
	if len(it.Measures) > 0 {
		b.WriteString("\n\n## Measurables\n\n| Measure | Target | Current | Evidence |\n|---|---|---|---|\n")
		for _, m := range it.Measures {
			cur := m.Current
			if met, known := m.Met(); known {
				cur += map[bool]string{true: " ✓", false: " ✗"}[met]
			}
			ev := "–"
			if m.Evidence != "" {
				ev = "`" + m.Evidence + "`"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(m.Name), cell(cmpStr(m.Target, "–")), cell(cmpStr(cur, "–")), cell(ev))
		}
	}
	if l := it.Links; !l.Empty() {
		b.WriteString("\n\n## Work done\n\n")
		var head []string
		if l.PRNumber > 0 {
			pr := fmt.Sprintf("⇡ **PR #%d**", l.PRNumber)
			if repo, _ := splitPR(l.PR); repo != "" {
				pr = fmt.Sprintf("⇡ **%s#%d**", repo, l.PRNumber)
			}
			if l.PRState != "" {
				pr += " · " + l.PRState
			}
			head = append(head, pr)
		}
		if l.Branch != "" {
			head = append(head, "`"+l.Branch+"`")
		}
		if l.Added+l.Removed > 0 {
			size := fmt.Sprintf("+%d −%d", l.Added, l.Removed)
			if l.Files > 0 {
				size += fmt.Sprintf(" in %d files", l.Files)
			}
			head = append(head, size)
		}
		if len(head) > 0 {
			b.WriteString(strings.Join(head, " · ") + "\n")
		}
		if l.PR != "" {
			b.WriteString("\n" + l.PR + "\n") // own line: terminals make it clickable
		}
		if len(l.Related) > 0 {
			var rel []string
			for _, u := range l.Related {
				if _, n := splitPR(u); n != "" {
					rel = append(rel, fmt.Sprintf("[%s](%s)", n, u))
				} else {
					rel = append(rel, u)
				}
			}
			b.WriteString("\nAlso: " + strings.Join(rel, " · ") + "\n")
		}
		if len(l.Commits) > 0 {
			b.WriteString("\n")
			for i, c := range l.Commits {
				if i == 8 {
					fmt.Fprintf(&b, "- … and %d more commits\n", len(l.Commits)-8)
					break
				}
				fmt.Fprintf(&b, "- `%s` %s\n", c.SHA[:min(7, len(c.SHA))], c.Subject)
			}
		}
	}
	if !autoVisual {
		b.WriteString(DocFooter(kids))
	}
	if len(it.Checks) > 0 {
		r, t := it.CheckCounts()
		fmt.Fprintf(&b, "\n\n## Checks · %d/%d\n\n", r, t)
		for _, c := range it.Checks {
			switch {
			case c.Waived != "":
				fmt.Fprintf(&b, "- [x] ~~%s~~ · _waived: %s_\n", c.Text, c.Waived)
			case c.Done:
				line := "- [x] " + c.Text
				if c.Evidence != "" {
					line += " · `" + c.Evidence + "`"
				}
				b.WriteString(line + "\n")
			default:
				b.WriteString("- [ ] " + c.Text + "\n")
			}
		}
	}
	return b.String()
}

func cmpStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// splitPR turns ".../owner/repo/pull/12" into ("owner/repo", "repo#12").
func splitPR(url string) (repo, label string) {
	parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
	if len(parts) >= 4 && parts[len(parts)-2] == "pull" {
		return parts[len(parts)-4] + "/" + parts[len(parts)-3], parts[len(parts)-3] + "#" + parts[len(parts)-1]
	}
	return "", ""
}

// ReviewCard summarises a submission for the reviewer: what changed, how it
// was verified, what's left and what's missing. Shown on items in review.
func ReviewCard(it *model.Item, e journal.Event) string {
	if it.Status != model.Review && it.Status != model.AgentReview {
		return ""
	}
	who := e.Actor
	if _, name, ok := strings.Cut(who, ":"); ok {
		who = name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "> **Review** · submitted %s by %s\n", e.TS.Local().Format("Jan 2 15:04"), who)
	lines := []string{}
	if e.Message != "" {
		lines = append(lines, e.Message)
	}
	if e.Verified != "" {
		lines = append(lines, "✓ verified: "+e.Verified)
	}
	for _, f := range e.Followups {
		lines = append(lines, "→ follow-up: "+f)
	}
	if l := it.Links; l != nil && l.PRNumber > 0 {
		lines = append(lines, fmt.Sprintf("⇡ PR #%d %s", l.PRNumber, l.PRState))
	}
	if r, t := it.CheckCounts(); t > 0 {
		lines = append(lines, fmt.Sprintf("☑ checks %d/%d", r, t))
	}
	for _, d := range e.Detail {
		if strings.HasPrefix(d, "⚠") {
			lines = append(lines, d)
		}
	}
	for _, l := range lines {
		b.WriteString(">\n> " + l + "\n")
	}
	return b.String() + "\n"
}
