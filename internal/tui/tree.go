package tui

import (
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
	selectedFocused = lipgloss.NewStyle().Reverse(true).Bold(true)
	selectedBlurred = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("237"))
	faint           = lipgloss.NewStyle().Faint(true)
	idStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	epicStyle       = lipgloss.NewStyle().Bold(true)
)

// renderRow draws one tree line exactly width cells wide.
func renderRow(r row, expanded, selected, focused bool, width int) string {
	arrow := "  "
	if r.hasKids {
		arrow = "▸ "
		if expanded {
			arrow = "▾ "
		}
	}
	indent := strings.Repeat("  ", r.depth)
	it := r.item
	if selected {
		plain := indent + arrow + it.Status.Glyph() + " " + it.ID + " " + it.Title
		plain = ansi.Truncate(plain, width, "…")
		plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
		if focused {
			return selectedFocused.Render(plain)
		}
		return selectedBlurred.Render(plain)
	}
	title := it.Title
	if it.Kind == model.Epic {
		title = epicStyle.Render(title)
	}
	line := faint.Render(indent+arrow) +
		render.StatusStyle(it.Status).Render(it.Status.Glyph()) + " " +
		idStyle.Render(it.ID) + " " + title
	line = ansi.Truncate(line, width, "…")
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}
