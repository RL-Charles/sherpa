package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"

	"github.com/RL-Charles/sherpa/internal/palette"
)

type row struct {
	item    *model.Item
	depth   int
	hasKids bool
	rollup  string // epic/story ticket progress, e.g. "12/19"

	// Board section rows have no item.
	group model.Status
	count int

	spin string // spinner frame while an agent works on the item
}

// flatten turns the item hierarchy into visible rows, descending only into
// expanded items. Items whose parent is missing are shown at the root.
func flatten(items []*model.Item, expanded map[string]bool) []row {
	return flattenFiltered(items, expanded, false)
}

// flattenFiltered is flatten with an option to hide completed tickets (and
// completed stories whose tickets are all done) so large repos stay scannable.
func flattenFiltered(items []*model.Item, expanded map[string]bool, hideDone bool) []row {
	known := map[string]bool{}
	for _, it := range items {
		known[it.ID] = true
	}
	visibleKids := func(id string) []*model.Item {
		kids := store.Children(items, id)
		if !hideDone {
			return kids
		}
		var out []*model.Item
		for _, k := range kids {
			if k.Kind == model.Ticket && k.Status == model.Done {
				continue
			}
			if k.Kind == model.Story && k.Status == model.Done {
				// Hide finished stories when every ticket under them is done.
				done, total, _, _ := render.Rollup(k, items)
				if total == 0 || done == total {
					continue
				}
			}
			out = append(out, k)
		}
		return out
	}
	var rows []row
	var walk func(it *model.Item, depth int)
	walk = func(it *model.Item, depth int) {
		kids := visibleKids(it.ID)
		rollup := ""
		if it.Kind != model.Ticket {
			if done, total, _, _ := render.Rollup(it, items); total > 0 {
				rollup = fmt.Sprintf("%d/%d", done, total)
			}
		}
		rows = append(rows, row{item: it, depth: depth, hasKids: len(kids) > 0, rollup: rollup})
		if expanded[it.ID] {
			for _, k := range kids {
				walk(k, depth+1)
			}
		}
	}
	for _, it := range items {
		if it.Parent == "" || !known[it.Parent] {
			if hideDone && it.Kind == model.Ticket && it.Status == model.Done {
				continue
			}
			walk(it, 0)
		}
	}
	return rows
}

var (
	selectedFocused = lipgloss.NewStyle().Bold(true).
			Foreground(palette.SelectedFg).
			Background(palette.SelectedBg)
	selectedBlurred = lipgloss.NewStyle().
			Foreground(palette.SelectedDimFg).
			Background(palette.SelectedDimBg)
	faint     = lipgloss.NewStyle().Foreground(palette.Muted)
	idStyle   = lipgloss.NewStyle().Foreground(palette.ID)
	epicStyle = lipgloss.NewStyle().Bold(true)
)

// density controls how much metadata a tree row shows. It is deliberately
// small so the title always gets the flexible width.
type density int

const (
	densityMinimal  density = iota // glyph + ID + title
	densityCompact                 // + checks, age, PR
	densityLabelled                // + status word and assignee
)

func (d density) String() string {
	switch d {
	case densityMinimal:
		return "minimal"
	case densityLabelled:
		return "labelled"
	}
	return "compact"
}

func (d density) next() density { return (d + 1) % 3 }

// renderRow draws one tree line exactly width cells wide.
func renderRow(r row, expanded, selected, focused bool, width int, d density) string {
	arrow := "  "
	if r.hasKids {
		arrow = "▸ "
		if expanded {
			arrow = "▾ "
		}
	}
	indent := strings.Repeat("  ", r.depth)
	it := r.item
	styled := render.StatusStyle(it.Status).Render(it.Status.Glyph())
	if r.spin != "" {
		styled = workStyle.Render(r.spin)
	}

	title := it.Title
	if it.Kind == model.Epic {
		title = epicStyle.Render(title)
	}
	left := faint.Render(indent+arrow) + styled + " " +
		idStyle.Render(it.ID) + " " + handMark(it) + flagMark(it) + title
	right := rowMeta(it, d, max(10, width/3))
	if r.rollup != "" && d != densityMinimal {
		if right == "" {
			right = faint.Render(r.rollup)
		} else {
			right = faint.Render(r.rollup) + faint.Render(" · ") + right
		}
	}
	w := width
	if selected {
		w-- // the status gutter
	}
	left, right = fitRow(left, right, w)

	if selected {
		plain := ansi.Strip(left)
		if right != "" {
			plain += strings.Repeat(" ", max(1, w-ansi.StringWidth(plain)-ansi.StringWidth(ansi.Strip(right)))) + ansi.Strip(right)
		}
		plain += strings.Repeat(" ", max(0, w-ansi.StringWidth(plain)))
		bar := render.StatusStyle(it.Status).Render("▌")
		if focused {
			return bar + selectedFocused.Render(plain)
		}
		return bar + selectedBlurred.Render(plain)
	}

	line := left
	if right != "" {
		line += strings.Repeat(" ", max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right))) + right
	}
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	return line
}

// fitRow drops metadata before the title when the pane is narrow, then
// truncates the left side so the row never overflows.
func fitRow(left, right string, width int) (string, string) {
	rw := ansi.StringWidth(right)
	if rw > 0 && rw+16 > width {
		right, rw = "", 0
	}
	avail := width - rw
	if rw > 0 {
		avail-- // separator cell
	}
	if lw := ansi.StringWidth(left); lw > avail && avail > 0 {
		left = ansi.Truncate(left, avail, "…")
	}
	return left, right
}

// rowMeta is the right-aligned, low-priority half of a tree row. Parts are
// added in priority order and dropped when they no longer fit, so a narrow
// pane keeps the most important metadata instead of losing all of it.
func rowMeta(it *model.Item, d density, maxWidth int) string {
	if d == densityMinimal {
		return ""
	}
	var parts []string
	add := func(s string, st lipgloss.Style) { parts = append(parts, st.Render(s)) }
	if r, t := it.CheckCounts(); t > 0 {
		add(fmt.Sprintf("%d/%d", r, t), faint)
	}
	if age := render.Age(it.Updated); age != "" && age != "just now" {
		add(age, render.StaleAgeStyle(it.Status, it.Updated))
	}
	if d == densityLabelled {
		add(statusLabel(it.Status), render.StatusStyle(it.Status))
		if it.Assignee != "" {
			add("@"+it.Assignee, faint)
		}
	}
	if badge := prBadge(it); badge != "" {
		parts = append(parts, badge)
	}
	sep := faint.Render(" · ")
	var out []string
	width := 0
	for _, p := range parts {
		pw := ansi.StringWidth(p)
		step := pw
		if len(out) > 0 {
			step += 3
		}
		if len(out) > 0 && width+step > maxWidth {
			break
		}
		out = append(out, p)
		width += step
	}
	return strings.Join(out, sep)
}

var handStyle = lipgloss.NewStyle().Foreground(palette.Yellow)

// handMark is a ✋ before the title while manual checks wait for a human.
func handMark(it *model.Item) string {
	if len(it.OpenManual()) == 0 {
		return ""
	}
	return handStyle.Render("✋") + " "
}

// flagMark shows 🔒 when an item carries a security or human-gate label.
func flagMark(it *model.Item) string {
	for _, l := range it.Labels {
		switch strings.ToLower(l) {
		case "security", "security-flagged", "needs-human":
			return handStyle.Render("🔒") + " "
		}
	}
	return ""
}

var prStyles = map[string]lipgloss.Style{
	"merged": lipgloss.NewStyle().Foreground(palette.Purple),
	"open":   lipgloss.NewStyle().Foreground(palette.Green),
	"draft":  lipgloss.NewStyle().Foreground(palette.Subtle),
	"closed": lipgloss.NewStyle().Foreground(palette.Red),
}

// prBadge is a small ⇡ in the metadata column when a PR is linked.
func prBadge(it *model.Item) string {
	if it.Links == nil || it.Links.PRNumber == 0 {
		return ""
	}
	st, ok := prStyles[it.Links.PRState]
	if !ok {
		st = faint
	}
	return st.Render("⇡")
}
