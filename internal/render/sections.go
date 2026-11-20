package render

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/worklog"
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
	if d, t := it.DeliverableCounts(); t > 0 {
		parts = append(parts, fmt.Sprintf("deliverables %d/%d", d, t))
	}
	if len(it.Code) > 0 {
		parts = append(parts, fmt.Sprintf("%d files", len(it.Code)))
	}
	if hand := it.OpenManual(); len(hand) > 0 {
		parts = append(parts, fmt.Sprintf("✋ %d to verify by hand", len(hand)))
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
	return TailSections(it, all, kids, false)
}

// TailSections is Sections with an option to omit Deliverables and Where when
// Snapshot already rendered them above the prose body.
func TailSections(it *model.Item, all []*model.Item, kids []*model.Item, skipStructured bool) string {
	var b strings.Builder
	autoVisual := false
	if it.Kind != model.Ticket {
		if _, ok := model.Section(it.Body, "Visual"); !ok {
			fmt.Fprintf(&b, "\n\n## Visual\n\n```text\n%s\n```\n", AutoVisual(it, all))
			autoVisual = true // it lists every child, so the Children list would repeat it
		}
	}
	if !skipStructured && len(it.Deliverables) > 0 {
		done, total := it.DeliverableCounts()
		fmt.Fprintf(&b, "\n\n## Deliverables · %d/%d\n", done, total)
		for i, d := range it.Deliverables {
			mark := "○"
			if d.Done {
				mark = "✓"
			}
			r, t := it.CheckCountsFor(i + 1)
			fmt.Fprintf(&b, "\n### %d. %s %s", i+1, d.Text, mark)
			if t > 0 {
				fmt.Fprintf(&b, " · %d/%d checks", r, t)
			}
			b.WriteString("\n\n")
			if d.Evidence != "" {
				fmt.Fprintf(&b, "_evidence: `%s`_\n\n", d.Evidence)
			}
			if d.Note != "" {
				fmt.Fprintf(&b, "_%s_\n\n", d.Note)
			}
			checks := it.ChecksFor(i + 1)
			if len(checks) == 0 {
				fmt.Fprintf(&b, "_No checks yet — add one with `shp check add --for %d`._\n", i+1)
				continue
			}
			for _, c := range checks {
				writeCheck(&b, c, false)
			}
		}
	}
	if !skipStructured && len(it.Code) > 0 {
		fmt.Fprintf(&b, "\n\n## Where\n\n```text\n%s\n```\n", CodeMap(it))
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
	if unlinked := it.UnlinkedChecks(); len(unlinked) > 0 {
		resolved, total := 0, 0
		for _, c := range unlinked {
			if c.Resolved() {
				resolved++
			}
			total++
		}
		label := "Checks"
		if len(it.Deliverables) > 0 {
			label = "Other checks"
		}
		fmt.Fprintf(&b, "\n\n## %s · %d/%d\n\n", label, resolved, total)
		for _, c := range unlinked {
			writeCheck(&b, c, true)
		}
	}
	return b.String()
}

// writeCheck renders one check. showFor adds the deliverable link for checks
// that are not already grouped under it.
func writeCheck(b *strings.Builder, c model.Check, showFor bool) {
	text := c.Text
	if c.Manual {
		text = "✋ " + text
	}
	suffix := ""
	if showFor && c.For > 0 {
		suffix = fmt.Sprintf(" · _for deliverable %d_", c.For)
	}
	switch {
	case c.Waived != "":
		fmt.Fprintf(b, "- [x] ~~%s~~ · _waived: %s_%s\n", text, c.Waived, suffix)
	case c.Done:
		line := "- [x] " + text
		if c.Evidence != "" {
			line += " · `" + c.Evidence + "`"
		}
		b.WriteString(line + suffix + "\n")
	default:
		line := "- [ ] " + text
		if c.How != "" {
			line += " · _how: " + c.How + "_"
		}
		b.WriteString(line + suffix + "\n")
	}
}

// CodeMap renders the planned code map as a directory-grouped text tree. It
// is deliberately preformatted: paths, symbols and notes stay aligned.
func CodeMap(it *model.Item) string {
	return CodeMapReconciled(it.Code, nil)
}

// CodeMapReconciled renders code refs reconciled against journal touched files,
// appending any off-plan touched files as a drift summary.
func CodeMapReconciled(code []model.CodeRef, touched []string) string {
	reconciled, offPlan := model.ReconcileCode(code, touched)
	refs := append([]model.CodeRef(nil), reconciled...)
	sort.SliceStable(refs, func(i, j int) bool {
		return filepath.ToSlash(refs[i].Path) < filepath.ToSlash(refs[j].Path)
	})

	changeW, pathW := 0, 0
	label := func(c string) string {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "add", "added", "new":
			return "add"
		case "remove", "removed", "delete", "deleted":
			return "remove"
		case "move", "moved":
			return "move"
		default:
			return "modify"
		}
	}
	for _, r := range refs {
		if n := len(label(r.Change)); n > changeW {
			changeW = n
		}
		name := path.Base(filepath.ToSlash(r.Path))
		if r.Lines != "" {
			name += ":" + r.Lines
		}
		if n := len(name); n > pathW {
			pathW = n
		}
	}

	var b strings.Builder
	dir := "\x00"
	for _, r := range refs {
		p := filepath.ToSlash(r.Path)
		d := path.Dir(p)
		if d == "." {
			d = "./"
		}
		if d != dir {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(d + "\n")
			dir = d
		}
		name := path.Base(p)
		if r.Lines != "" {
			name += ":" + r.Lines
		}
		fmt.Fprintf(&b, "  %-*s  %-*s", changeW, label(r.Change), pathW, name)
		var tail []string
		if r.Deliverable > 0 {
			tail = append(tail, fmt.Sprintf("deliver %d", r.Deliverable))
		}
		state := strings.TrimSpace(r.Status)
		if state == "" {
			state = "planned"
		}
		tail = append(tail, state)
		b.WriteString("  " + strings.Join(tail, " · ") + "\n")
		if len(r.Symbols) > 0 {
			b.WriteString("    symbols: " + strings.Join(r.Symbols, ", ") + "\n")
		}
		if r.Note != "" {
			b.WriteString("    note: " + r.Note + "\n")
		}
	}
	if len(offPlan) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		shown := offPlan
		extra := ""
		if len(shown) > 5 {
			extra = fmt.Sprintf(" (+%d more)", len(shown)-5)
			shown = shown[:5]
		}
		fmt.Fprintf(&b, "△ %d off-plan touched: %s%s\n", len(offPlan), strings.Join(shown, ", "), extra)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Snapshot builds the scannable at-a-glance overview shown above long prose in
// the TUI preview: Recent signals & session context, Deliverables (done
// collapsed, open expanded), and reconciled Where code map. When compact is
// true (Brief mode), completed deliverables fold into one summary line so the
// entire ticket brief fits on one screen.
func Snapshot(it *model.Item, events []journal.Event, compact bool) string {
	if it.Kind != model.Ticket {
		return ""
	}
	touched := worklog.TouchedFiles(events)
	signals := worklog.RecentSignals(events, 3)
	hasCode := len(it.Code) > 0 || len(touched) > 0
	if len(it.Deliverables) == 0 && !hasCode && len(signals) == 0 && it.Handoff.Empty() {
		return ""
	}
	var b strings.Builder

	// 1. Recent signals & session context (latest decisions/blockers/rejects/handoff).
	if len(signals) > 0 || !it.Handoff.Empty() {
		sessions := worklog.Build(events)
		workSessions := 0
		for _, s := range sessions {
			if s.N > 0 {
				workSessions++
			}
		}
		heading := "Recent signals"
		if workSessions > 0 {
			heading = fmt.Sprintf("Recent signals · %d sessions · %d events", workSessions, len(events))
		}
		fmt.Fprintf(&b, "## %s\n\n", heading)
		actor, sid, br, wt := worklog.LastAgentContext(events)
		if h := it.Handoff; !h.Empty() {
			if h.Agent != "" {
				actor = h.Agent
			}
			if h.SessionID != "" {
				sid = h.SessionID
			}
			if h.Branch != "" {
				br = h.Branch
			}
			if h.Worktree != "" {
				wt = h.Worktree
			}
		}
		if br == "" && it.Links != nil {
			br = it.Links.Branch
		}
		var ctx []string
		if actor != "" {
			ctx = append(ctx, "@"+actor)
		}
		if sid != "" {
			ctx = append(ctx, "`"+short(sid, 18)+"`")
		}
		if br != "" {
			ctx = append(ctx, "`"+br+"`")
		}
		if wt != "" && wt != "main" {
			ctx = append(ctx, "wt:`"+wt+"`")
		}
		if len(ctx) > 0 {
			fmt.Fprintf(&b, "_Last context: %s_\n\n", strings.Join(ctx, " · "))
		}
		for _, e := range signals {
			icon, _, head, _ := worklog.Describe(e)
			age := Age(e.TS)
			if age != "" {
				age = " · _" + age + "_"
			}
			fmt.Fprintf(&b, "- %s %s%s\n", icon, short(head, 80), age)
		}
		b.WriteString("\n")
	}

	// 2. Deliverables: done collapsed (summary in compact mode, 1-liners in full),
	// open expanded with their checks.
	if len(it.Deliverables) > 0 {
		done, total := it.DeliverableCounts()
		fmt.Fprintf(&b, "## Deliverables · %d/%d\n\n", done, total)
		foldDone := compact && done > 0 && total > 3
		if foldDone {
			fmt.Fprintf(&b, "- [x] **%d deliverables done** _(press `B` for full list)_\n", done)
		}
		for i, d := range it.Deliverables {
			r, t := it.CheckCountsFor(i + 1)
			chk := ""
			if t > 0 {
				chk = fmt.Sprintf(" · %d/%d", r, t)
				if r == t {
					chk += " ✓"
				}
			}
			if d.Done {
				if !foldDone {
					fmt.Fprintf(&b, "- [x] **%d.** %s%s\n", i+1, short(d.Text, 86), chk)
				}
				continue
			}
			fmt.Fprintf(&b, "- [ ] **%d.** %s%s\n", i+1, d.Text, chk)
			for _, c := range it.ChecksFor(i + 1) {
				text := c.Text
				if c.Manual {
					text = "✋ " + text
				}
				switch {
				case c.Waived != "":
					fmt.Fprintf(&b, "  - [x] ~~%s~~ · _waived_\n", short(text, 78))
				case c.Done:
					ev := ""
					if c.Evidence != "" {
						ev = " · `" + short(c.Evidence, 44) + "`"
					}
					fmt.Fprintf(&b, "  - [x] %s%s\n", short(text, 74), ev)
				default:
					fmt.Fprintf(&b, "  - [ ] %s\n", short(text, 84))
				}
			}
		}
		b.WriteString("\n")
	}

	// 3. Where: reconciled planned code map + off-plan touched files.
	if hasCode {
		reconciled, offPlan := model.ReconcileCode(it.Code, touched)
		label := fmt.Sprintf("Where · %d files", len(reconciled))
		if len(reconciled) == 0 {
			label = fmt.Sprintf("Where · %d touched", len(offPlan))
		} else if len(offPlan) > 0 {
			label += fmt.Sprintf(" · △ %d off-plan", len(offPlan))
		}
		fmt.Fprintf(&b, "## %s\n\n```text\n%s\n```\n\n", label, CodeMapReconciled(it.Code, touched))
	}

	return strings.TrimSpace(b.String())
}

// BriefBody is the concise prose substitute shown in TUI brief mode (`B`):
// just the Outcome paragraph (and Out of scope when present) so long-horizon
// tickets fit cleanly in one screen.
func BriefBody(it *model.Item) string {
	var b strings.Builder
	if out := model.Outcome(it, 320); out != "" && out != it.Title {
		fmt.Fprintf(&b, "## Outcome\n\n%s\n", out)
	}
	if oos, ok := model.Section(it.Body, "Out of scope"); ok && strings.TrimSpace(oos) != "" {
		fmt.Fprintf(&b, "\n## Out of scope\n\n%s\n", short(strings.Join(strings.Fields(oos), " "), 240))
	}
	b.WriteString("\n_Brief view · press `B` for full prose & worklog_\n")
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
// It deliberately does not repeat the check count: the status strip already
// owns that fact.
func ReviewCard(it *model.Item, e journal.Event) string {
	if it.Status != model.Review && it.Status != model.AgentReview {
		return ""
	}
	who := e.Actor
	if _, name, ok := strings.Cut(who, ":"); ok {
		who = name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "> **Review** · submitted %s by %s\n>\n", e.TS.Local().Format("Jan 2 15:04"), who)
	lines := []string{}
	if e.Message != "" {
		lines = append(lines, "**Changed** "+e.Message)
	}
	if e.Verified != "" {
		lines = append(lines, "✓ **Verified** "+e.Verified)
	}
	for _, f := range e.Followups {
		lines = append(lines, "→ **Follow-up** "+f)
	}
	if l := it.Links; l != nil && l.PRNumber > 0 {
		lines = append(lines, fmt.Sprintf("⇡ **PR** #%d %s", l.PRNumber, l.PRState))
	}
	for _, d := range e.Detail {
		if strings.HasPrefix(d, "⚠") {
			lines = append(lines, d)
		}
	}
	for _, l := range lines {
		b.WriteString("> - " + l + "\n")
	}
	return b.String() + "\n"
}
