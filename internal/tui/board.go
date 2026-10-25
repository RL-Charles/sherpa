package tui

import (
	"fmt"
	"slices"
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

// inboxItems includes stories and epics: their closure is the human review
// step, so they belong in the needs-you queue too.
func inboxItems(items []*model.Item, st model.Status) []*model.Item {
	var out []*model.Item
	for _, it := range items {
		if it.Status == st && (it.Kind == model.Ticket || it.Kind == model.Story || it.Kind == model.Epic) {
			out = append(out, it)
		}
	}
	return out
}

// flattenBoard lists every status section in order, with its tickets when
// expanded. flattenInbox does the same for every item kind.
func flattenBoard(items []*model.Item, expanded map[string]bool, order []model.Status) []row {
	return flattenGroups(items, expanded, order, false)
}

func flattenInbox(items []*model.Item, expanded map[string]bool, order []model.Status) []row {
	return flattenGroups(items, expanded, order, true)
}

func flattenGroups(items []*model.Item, expanded map[string]bool, order []model.Status, inbox bool) []row {
	var rows []row
	for _, st := range order {
		group := boardTickets(items, st)
		if inbox {
			group = inboxItems(items, st)
		}
		if st == model.Review { // hand checks first: they are what only a human can do
			slices.SortStableFunc(group, func(a, b *model.Item) int {
				return len(b.OpenManual()) - len(a.OpenManual())
			})
		}
		rows = append(rows, row{group: st, count: len(group), hasKids: len(group) > 0})
		if expanded[groupKey(st)] {
			for _, it := range group {
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

// groupDoc is the preview for a selected section: its items as a list.
func (m *Model) groupDoc(st model.Status) string {
	items := boardTickets(m.items, st)
	if m.inbox {
		items = inboxItems(m.items, st)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", st.Glyph(), strings.ToUpper(statusLabel(st)[:1])+statusLabel(st)[1:])
	if len(items) == 0 {
		b.WriteString("_No items._\n")
	}
	for _, it := range items {
		fmt.Fprintf(&b, "- **%s** %s", it.ID, it.Title)
		if it.Kind != model.Ticket {
			fmt.Fprintf(&b, " · _%s_", it.Kind)
		}
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
	m.board, m.inbox = !m.board, false
	m.offset = 0
	m.rebuild(m.currentID())
}
