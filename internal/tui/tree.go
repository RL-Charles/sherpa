package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"
)

type row struct {
	item    *model.Item
	depth   int
	hasKids bool

	// Board section rows have no item.
	group model.Status
	count int

	spin string // spinner frame while an agent works on the item
}

// flatten turns the item hierarchy into visible rows, descending only into
// expanded items. Items whose parent is missing are shown at the root.
func flatten(items []*model.Item, expanded map[string]bool) []row {
	known := map[string]bool{}
	for _, it := range items {
		known[it.ID] = true
	}
	var rows []row
	var walk func(it *model.Item, depth int)
	walk = func(it *model.Item, depth int) {
		kids := store.Children(items, it.ID)
		rows = append(rows, row{item: it, depth: depth, hasKids: len(kids) > 0})
		if expanded[it.ID] {
			for _, k := range kids {
				walk(k, depth+1)
			}
		}
	}
	for _, it := range items {
		if it.Parent == "" || !known[it.Parent] {
			walk(it, 0)
		}
	}
	return rows
}

var (
	selectedFocused = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "232", Dark: "252"}).
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "237"})
	selectedBlurred = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "250"}).
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
	faint     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	idStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "243", Dark: "244"})
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
		idStyle.Render(it.ID) + " " + handMark(it) + title
	right := rowMeta(it, d, max(10, width/3))
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
		add(age, faint)
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

var handStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "214"})

// handMark is a ✋ before the title while manual checks wait for a human.
func handMark(it *model.Item) string {
	if len(it.OpenManual()) == 0 {
		return ""
	}
	return handStyle.Render("✋") + " "
}

var prStyles = map[string]lipgloss.Style{
	"merged": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "55", Dark: "99"}),
	"open":   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"}),
	"draft":  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"}),
	"closed": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "124", Dark: "203"}),
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
