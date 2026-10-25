package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFinderJumpsIntoCollapsedTree(t *testing.T) {
	m := fixture(t)
	press(m, "C") // collapse everything: tickets are hidden
	if m.currentID() != "E-001" {
		t.Fatalf("setup: cursor on %s", m.currentID())
	}
	press(m, "/")
	if m.find == nil {
		t.Fatal("/ should open the finder")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tkt two")}) // coalesced input, a q-free query
	if got := m.found(); got == nil || got.ID != "T-0002" {
		t.Fatalf("best match should be T-0002, got %v", got)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⌕ tkt two") || !strings.Contains(view, "about Ticket two") {
		t.Fatalf("finder prompt and live preview expected:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.find != nil || m.currentID() != "T-0002" {
		t.Fatalf("enter should close the finder on T-0002, got %s", m.currentID())
	}
	if !m.expanded["E-001"] || !m.expanded["S-001"] {
		t.Fatal("parents of the found ticket should be expanded")
	}
}

func TestFinderEscRestores(t *testing.T) {
	m := fixture(t)
	press(m, "j") // S-001
	press(m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // q types, it doesn't quit
	if m.find == nil || m.find.query != "q" {
		t.Fatal("letters must go to the query while searching")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.find != nil || m.currentID() != "S-001" {
		t.Fatalf("esc should close and keep the tree position, got %s", m.currentID())
	}
}

func TestFinderFiltersAndNumbers(t *testing.T) {
	m := fixture(t)
	press(m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if got := m.found(); got == nil || got.ID != "T-0001" {
		t.Fatalf("a bare number should find that ticket first, got %v", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("is:epic")})
	if n := len(m.find.results); n != 2 {
		t.Fatalf("is:epic should list the 2 epics, got %d", n)
	}
}
