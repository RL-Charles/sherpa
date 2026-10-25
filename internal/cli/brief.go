package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/model"
)

// Brief is the compact context a reader or an agent needs before touching a
// ticket: outcome, deliverables, next open check, code map and the authored
// context. Full prose stays in `shp show`.
type Brief struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	Status       string             `json:"status"`
	Template     string             `json:"template,omitempty"`
	Parent       string             `json:"parent,omitempty"`
	Assignee     string             `json:"assignee,omitempty"`
	Now          string             `json:"now,omitempty"`
	Outcome      string             `json:"outcome"`
	ChecksDone   int                `json:"checks_done"`
	ChecksTotal  int                `json:"checks_total"`
	Unlinked     int                `json:"unlinked_checks,omitempty"`
	Deliverables []BriefDeliverable `json:"deliverables,omitempty"`
	Where        []model.CodeRef    `json:"where,omitempty"`
	NextCheck    string             `json:"next_check,omitempty"`
	Context      []string           `json:"context,omitempty"`
	Design       []string           `json:"design,omitempty"`
	OutOfScope   []string           `json:"out_of_scope,omitempty"`
	Gaps         []string           `json:"gaps,omitempty"`
}

type BriefDeliverable struct {
	Index    int          `json:"index"`
	Text     string       `json:"text"`
	Done     bool         `json:"done"`
	Evidence string       `json:"evidence,omitempty"`
	Checks   []BriefCheck `json:"checks,omitempty"`
}

type BriefCheck struct {
	Index    int    `json:"index"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
	Manual   bool   `json:"manual,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

func (a *app) briefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "brief [id]",
		Short: "Compact intent, deliverables, next check and code map for an agent or reviewer",
		Long: `Print the short version of a ticket: outcome, deliverables, next open check,
code map, and the authored context and design. Use ` + "`shp show`" + ` for the
full document and worklog.

This is the context to hand an agent before it starts, and the summary a
reviewer should read before opening the diff.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			b := BuildBrief(it)
			if a.json {
				return a.emit(b)
			}
			fmt.Print(RenderBrief(b))
			return nil
		},
	}
}

// BuildBrief assembles the compact view. It deliberately reuses the same
// lint rules so the gaps an agent sees are the gaps a reviewer sees.
func BuildBrief(it *model.Item) Brief {
	b := Brief{
		ID:       it.ID,
		Title:    it.Title,
		Status:   strings.ReplaceAll(string(it.Status), "_", " "),
		Template: it.Template,
		Parent:   it.Parent,
		Assignee: it.Assignee,
		Now:      it.Now,
		Where:    it.Code,
	}
	b.ChecksDone, b.ChecksTotal = it.CheckCounts()
	for _, c := range it.UnlinkedChecks() {
		if !c.Resolved() {
			b.Unlinked++
		}
	}
	b.Outcome = briefOutcome(it)
	for i, d := range it.Deliverables {
		bd := BriefDeliverable{Index: i + 1, Text: d.Text, Done: d.Done, Evidence: d.Evidence}
		for j, c := range it.Checks {
			if c.For != i+1 {
				continue
			}
			bd.Checks = append(bd.Checks, BriefCheck{Index: j + 1, Text: c.Text, Done: c.Resolved(), Manual: c.Manual, Evidence: c.Evidence})
		}
		b.Deliverables = append(b.Deliverables, bd)
	}
	for i, c := range it.Checks {
		if c.Resolved() {
			continue
		}
		next := fmt.Sprintf("#%d %s", i+1, c.Text)
		if c.Manual {
			next += " (manual — a human verifies it)"
		}
		if c.For > 0 {
			next += fmt.Sprintf(" (deliverable %d)", c.For)
		}
		b.NextCheck = next
		break
	}
	b.Context = sectionLines(it.Body, "Context", 6, false)
	b.Design = sectionLines(it.Body, "Design", 7, true)
	b.OutOfScope = sectionLines(it.Body, "Out of scope", 3, false)
	for _, f := range lintItem(it) {
		b.Gaps = append(b.Gaps, f.Message)
	}
	return b
}

// RenderBrief is the plain-text form. No colour: it is meant to be pasted
// into an agent prompt or read in a narrow terminal.
func RenderBrief(b Brief) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s · %s\n", b.ID, b.Title)
	meta := []string{b.Status}
	if b.Template != "" {
		meta = append(meta, b.Template)
	}
	if b.Parent != "" {
		meta = append(meta, b.Parent)
	}
	if b.Assignee != "" {
		meta = append(meta, "@"+b.Assignee)
	}
	d, t := 0, len(b.Deliverables)
	for _, x := range b.Deliverables {
		if x.Done {
			d++
		}
	}
	if t > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d deliverables", d, t))
	}
	if b.ChecksTotal > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d checks", b.ChecksDone, b.ChecksTotal))
	}
	if b.Unlinked > 0 {
		meta = append(meta, fmt.Sprintf("%d unlinked", b.Unlinked))
	}
	if len(b.Where) > 0 {
		meta = append(meta, fmt.Sprintf("%d files", len(b.Where)))
	}
	out.WriteString(strings.Join(meta, " · ") + "\n\n")
	fmt.Fprintf(&out, "Outcome  %s\n", b.Outcome)
	if b.Now != "" && b.Now != b.Outcome {
		fmt.Fprintf(&out, "Now      %s\n", truncateText(b.Now, 240))
	}

	if len(b.Deliverables) == 0 {
		out.WriteString("Deliverables  none yet\n")
	} else {
		fmt.Fprintf(&out, "Deliverables  %d/%d\n", d, t)
		for _, x := range b.Deliverables {
			mark := "○"
			if x.Done {
				mark = "✓"
			}
			fmt.Fprintf(&out, "  %s %d. %s\n", mark, x.Index, x.Text)
			if x.Evidence != "" {
				fmt.Fprintf(&out, "      evidence: %s\n", x.Evidence)
			}
			if len(x.Checks) == 0 {
				out.WriteString("      (no checks yet)\n")
			}
			for k, c := range x.Checks {
				if k == 4 {
					fmt.Fprintf(&out, "      … %d more\n", len(x.Checks)-4)
					break
				}
				cm := "○"
				if c.Done {
					cm = "✓"
				}
				line := fmt.Sprintf("      %s %s", cm, c.Text)
				if c.Evidence != "" {
					line += " (" + c.Evidence + ")"
				}
				out.WriteString(line + "\n")
			}
		}
	}
	if b.NextCheck != "" {
		fmt.Fprintf(&out, "Next check   %s\n", b.NextCheck)
	}
	if len(b.Where) > 0 {
		fmt.Fprintf(&out, "Where  %d files\n", len(b.Where))
		for i, r := range b.Where {
			if i == 8 {
				fmt.Fprintf(&out, "  … %d more\n", len(b.Where)-8)
				break
			}
			line := fmt.Sprintf("  %-6s %s", codeChange(r.Change), r.Path)
			if len(r.Symbols) > 0 {
				line += "  " + strings.Join(r.Symbols, ", ")
			}
			if r.Deliverable > 0 {
				line += fmt.Sprintf("  deliver %d", r.Deliverable)
			}
			state := r.Status
			if state == "" {
				state = "planned"
			}
			line += " · " + state
			out.WriteString(line + "\n")
		}
	}
	if len(b.Context) > 0 {
		out.WriteString("\nContext\n")
		for _, l := range b.Context {
			out.WriteString("  " + l + "\n")
		}
	}
	if len(b.Design) > 0 {
		out.WriteString("\nDesign\n")
		for _, l := range b.Design {
			out.WriteString("  " + l + "\n")
		}
	}
	if len(b.OutOfScope) > 0 {
		out.WriteString("\nOut of scope\n")
		for _, l := range b.OutOfScope {
			out.WriteString("  " + l + "\n")
		}
	}
	if len(b.Gaps) > 0 {
		out.WriteString("\nGaps\n")
		for i, g := range b.Gaps {
			if i == 5 {
				fmt.Fprintf(&out, "  … %d more (shp lint %s)\n", len(b.Gaps)-5, b.ID)
				break
			}
			out.WriteString("  ⚠ " + g + "\n")
		}
	}
	return out.String()
}

func briefOutcome(it *model.Item) string {
	if why, ok := model.Section(it.Body, "Why"); ok {
		if p := firstParagraph(why); p != "" {
			return truncateText(p, 240)
		}
	}
	if it.Now != "" {
		return truncateText(it.Now, 240)
	}
	return truncateText(it.Title, 240)
}

func firstParagraph(s string) string {
	for _, p := range strings.Split(s, "\n\n") {
		p = strings.Join(strings.Fields(p), " ")
		if p != "" && !isPlaceholderLine(p) {
			return p
		}
	}
	return ""
}

func sectionLines(body, name string, max int, keepHeadings bool) []string {
	text, ok := model.Section(body, name)
	if !ok {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || isPlaceholderLine(t) {
			continue
		}
		if strings.HasPrefix(t, "###") {
			if keepHeadings {
				out = append(out, t)
			}
			continue
		}
		if strings.HasPrefix(t, "~~~") || strings.HasPrefix(t, "```") {
			continue
		}
		if strings.HasSuffix(t, ":") && !strings.HasPrefix(t, "###") {
			continue // an empty label is not context
		}
		t = strings.TrimPrefix(t, "- ")
		t = strings.TrimPrefix(t, "* ")
		out = append(out, truncateText(t, 300))
		if len(out) >= max {
			break
		}
	}
	return out
}

func isPlaceholderLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "- …" || t == "- ..." || t == "…" {
		return true
	}
	if strings.HasPrefix(t, "_") && strings.HasSuffix(t, "_") && len(t) > 2 {
		return true
	}
	return false
}

func truncateText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimRight(string(r[:n]), " ") + "…"
}

func codeChange(c string) string {
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
