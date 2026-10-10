package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/RL-Charles/sherpa/internal/journal"
)

func click(m *Model, x, y int) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// find returns the screen cell where text first appears at or right of minX.
func find(t *testing.T, m *Model, text string, minX int) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(l)
		if i := strings.Index(plain, text); i >= 0 && ansi.StringWidth(plain[:i]) >= minX {
			return ansi.StringWidth(plain[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, ansi.Strip(m.View()))
	return 0, 0
}

func TestClickTreeSelectsAndToggles(t *testing.T) {
	m := fixture(t)
	click(m, 10, contentTop+2)
	if id := m.currentID(); id != "T-0001" {
		t.Fatalf("click on third row: want T-0001, got %s", id)
	}
	click(m, 1+padX, contentTop) // E-001's arrow
	if m.expanded["E-001"] || len(m.rows) != 2 {
		t.Fatalf("arrow click should collapse E-001, rows=%d", len(m.rows))
	}
	click(m, 10, contentTop) // the selected row again
	if !m.expanded["E-001"] || len(m.rows) != 5 {
		t.Fatalf("clicking the selected row should expand E-001, rows=%d", len(m.rows))
	}
}

func TestClickPreviewPlacesCursor(t *testing.T) {
	m := fixture(t)
	click(m, 10, contentTop+2) // T-0001
	x, y := find(t, m, "about Ticket one", m.treeWidth())
	click(m, x+8, y) // on the "c" of "Ticket"
	if !m.ed.editing {
		t.Fatal("click on body should start editing")
	}
	line := m.ed.lines[m.ed.row]
	if want := strings.Index(line, "Ticket") + 2; line != "about Ticket one" || m.ed.col != want {
		t.Fatalf("cursor at %q col %d, want %q col %d", line, m.ed.col, "about Ticket one", want)
	}
	x, y = find(t, m, "about", m.treeWidth()) // now raw, behind the gutter
	click(m, x, y)
	if m.ed.lines[m.ed.row] != "about Ticket one" || m.ed.col != 0 {
		t.Fatalf("raw click: cursor at %q col %d, want col 0", m.ed.lines[m.ed.row], m.ed.col)
	}
}

func TestClickTreeKeepsUnsavedEdit(t *testing.T) {
	m := fixture(t)
	click(m, 10, contentTop+2)
	x, y := find(t, m, "about Ticket one", m.treeWidth())
	click(m, x, y)
	press(m, "x") // vim: delete a character
	click(m, 10, contentTop+3)
	if !m.ed.editing || m.currentID() != "T-0001" || !strings.HasPrefix(m.flash, "⚠") {
		t.Fatalf("dirty edit should block tree clicks: editing=%v id=%s flash=%q", m.ed.editing, m.currentID(), m.flash)
	}
	press(m, "u") // undo, still dirty-flagged; save instead
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	click(m, 10, contentTop+3)
	if m.ed.editing || m.currentID() != "T-0002" {
		t.Fatalf("after save a tree click should leave editing: editing=%v id=%s", m.ed.editing, m.currentID())
	}
}

func TestClickWorklogHeaderToggles(t *testing.T) {
	m := fixture(t)
	for _, msg := range []string{"first", "second"} {
		if err := journal.Append(m.store.Dir, journal.Event{Actor: "agent:claude", Session: "s", Item: "T-0001", Type: "claim"}); err != nil {
			t.Fatal(err)
		}
		_ = journal.Append(m.store.Dir, journal.Event{Actor: "agent:claude", Session: "s", Item: "T-0001", Type: "log", Message: msg})
	}
	m.reloadKeep()
	click(m, 10, contentTop+2)
	x, y := find(t, m, "▸ Session 1", m.treeWidth())
	click(m, x, y)
	if !m.expandLog || m.ed.editing {
		t.Fatalf("session header click should expand the worklog without editing: expandLog=%v editing=%v", m.expandLog, m.ed.editing)
	}
}
