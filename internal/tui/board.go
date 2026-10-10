package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
)

// Board -----------------------------------------------------------------------
//
// `b` swaps the tree for the board: tickets grouped into one collapsible
// section per status, active work first. Section rows have no item; their
// expand state lives in Model.expanded under groupKey.

var boardOrder = []model.Status{model.InProgress, model.AgentReview, model.Review, model.Blocked, model.Ready, model.Backlog, model.Done}

func groupKey(s model.Status) string { return "status:" + string(s) }

// key identifies a row for expand state and cursor restoration.
func (r row) key() string {
	if r.item == nil {
		return groupKey(r.group)
	}
	return r.item.ID
}

func boardTickets(items []*model.Item, st model.Status) []*model.Item {
	var out []*model.Item
	for _, it := range items {
		if it.Kind == model.Ticket && it.Status == st {
			out = append(out, it)
		}
	}
	return out
}

// flattenBoard lists every status section, with its tickets when expanded.
func flattenBoard(items []*model.Item, expanded map[string]bool) []row {
	var rows []row
	for _, st := range boardOrder {
		tickets := boardTickets(items, st)
		rows = append(rows, row{group: st, count: len(tickets), hasKids: len(tickets) > 0})
		if expanded[groupKey(st)] {
			for _, it := range tickets {
				rows = append(rows, row{item: it, depth: 1})
			}
		}
	}
	return rows
}

// renderGroup draws a section header exactly width cells wide.
func renderGroup(r row, expanded, selected, focused bool, width int) string {
	arrow := "  "
	if r.hasKids {
		arrow = "▸ "
		if expanded {
			arrow = "▾ "
		}
	}
	name := strings.ToUpper(statusLabel(r.group)[:1]) + statusLabel(r.group)[1:]
	count := fmt.Sprintf(" %d", r.count)
	if selected {
		plain := ansi.Truncate(arrow+r.group.Glyph()+" "+name+count, width, "…")
		plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
		if focused {
			return selectedFocused.Render(plain)
		}
		return selectedBlurred.Render(plain)
	}
	st := render.StatusStyle(r.group)
	line := faint.Render(arrow) + st.Render(r.group.Glyph()+" ") + st.Bold(true).Render(name) + faint.Render(count)
	line = ansi.Truncate(line, width, "…")
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}

// groupDoc is the preview for a selected section: its tickets as a list.
func (m *Model) groupDoc(st model.Status) string {
	tickets := boardTickets(m.items, st)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", st.Glyph(), strings.ToUpper(statusLabel(st)[:1])+statusLabel(st)[1:])
	if len(tickets) == 0 {
		b.WriteString("_No tickets._\n")
	}
	for _, it := range tickets {
		fmt.Fprintf(&b, "- **%s** %s", it.ID, it.Title)
		if it.Assignee != "" {
			fmt.Fprintf(&b, " · @%s", it.Assignee)
		}
		if it.Parent != "" {
			fmt.Fprintf(&b, " · _%s_", it.Parent)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) toggleBoard() {
	m.board = !m.board
	m.offset = 0
	m.rebuild(m.currentID())
}
