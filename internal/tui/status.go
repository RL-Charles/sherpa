package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// Status picker ---------------------------------------------------------------
//
// `s` replaces the help bar with a one-line picker for the selected item:
//
//	Move T-0001 to  1 ○ backlog  2 ◎ ready  [3 ◐ in progress]  4 ◑ review …
//
// ←/→ (or h/l, tab) move, enter applies, number keys apply directly, esc cancels.
// A click on an option applies it. The same workflow rules as `shp status`
// apply, and the change is journaled like one.

type picker struct {
	id  string
	idx int
}

var (
	pickCurrent = lipgloss.NewStyle().Underline(true)
	pickActive  = lipgloss.NewStyle().Reverse(true).Bold(true)
)

func statusLabel(s model.Status) string { return strings.ReplaceAll(string(s), "_", " ") }

func (m *Model) openPicker() {
	it := m.selected()
	if it == nil {
		return
	}
	m.pick = &picker{id: it.ID, idx: max(0, slices.Index(model.Statuses, it.Status))}
	m.layout()
}

func (m *Model) closePicker() {
	m.pick = nil
	m.layout()
}

func (m *Model) pickKey(msg tea.KeyMsg) {
	n := len(model.Statuses)
	switch s := msg.String(); {
	case s == "esc" || s == "q" || s == "s":
		m.closePicker()
	case s == "left" || s == "h" || s == "shift+tab":
		m.pick.idx = (m.pick.idx + n - 1) % n
	case s == "right" || s == "l" || s == "tab":
		m.pick.idx = (m.pick.idx + 1) % n
	case s == "enter" || s == " ":
		m.applyStatus(model.Statuses[m.pick.idx])
	case len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < n:
		m.applyStatus(model.Statuses[s[0]-'1'])
	}
}

// applyStatus moves the picked item to status to and closes the picker.
func (m *Model) applyStatus(to model.Status) {
	id := m.pick.id
	m.closePicker()
	it, err := m.store.Get(id)
	if err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	if it.Status == to {
		m.flash = "· " + id + " is already " + statusLabel(to)
		return
	}
	if err := workflow.CheckTransition(m.actor, it, to); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	from := it.Status
	it.Status = to
	if err := m.store.Save(it); err != nil {
		m.flash = "⚠ " + err.Error()
		return
	}
	_ = journal.Append(m.store.Dir, journal.Event{
		Actor: m.actor.String(), Session: m.session, Item: id, Type: "status",
		From: string(from), To: string(to), Commit: gitinfo.Head(m.store.CodeRoot),
	})
	m.flash = fmt.Sprintf("✓ %s %s → %s", id, statusLabel(from), statusLabel(to))
	m.reloadKeep()
}

// pickerView renders the picker line and the cell span [from, to) of each option.
func (m *Model) pickerView() (string, [][2]int) {
	var cur model.Status
	if it := m.findItem(m.pick.id); it != nil {
		cur = it.Status
	}
	line := "Move " + idStyle.Render(m.pick.id) + " to "
	spans := make([][2]int, len(model.Statuses))
	for i, st := range model.Statuses {
		line += " "
		opt := fmt.Sprintf("%d %s %s", i+1, st.Glyph(), statusLabel(st))
		var shown string
		switch {
		case i == m.pick.idx:
			shown = pickActive.Render(" " + opt + " ")
		case st == cur:
			shown = " " + pickCurrent.Render(render.StatusStyle(st).Render(opt)) + " "
		default:
			shown = " " + render.StatusStyle(st).Render(opt) + " "
		}
		from := ansi.StringWidth(line)
		line += shown
		spans[i] = [2]int{from, ansi.StringWidth(line)}
	}
	line += faint.Render(fmt.Sprintf("  ←→ enter · 1-%d · esc", len(model.Statuses)))
	return ansi.Truncate(line, max(1, m.width), "…"), spans
}

// footerView is the help bar, or the status picker while it is open.
func (m *Model) footerView() string {
	if m.pick != nil {
		line, _ := m.pickerView()
		return line
	}
	if m.reject != nil {
		return m.rejectView()
	}
	return m.help.View(m.helpKeys())
}

// clickPicker applies the option under x on the footer line; any click
// elsewhere closes the picker. It reports whether the click was consumed.
func (m *Model) clickPicker(x, y int) bool {
	if y != headerHeight+m.bodyHeight() {
		m.closePicker()
		return false
	}
	_, spans := m.pickerView()
	for i, sp := range spans {
		if x >= sp[0] && x < sp[1] {
			m.applyStatus(model.Statuses[i])
			return true
		}
	}
	return true
}
